package playerpool

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// ageBands defines the pooled free-agent age distribution: ~15% teenagers,
// ~50% prime (21-28), ~25% senior (29-31), ~10% late (32-33). The
// proportions give pool consumers a realistic age mix: a few raw prospects,
// a broad prime core, and a thin veteran tail. Each band is uniform within
// its range; the weighted selection keeps the shape stable over many draws.
var ageBands = []struct {
	lo, hi int
	weight int
}{
	{17, 20, 15},
	{21, 28, 50},
	{29, 31, 25},
	{32, 33, 10},
}

// poolAge samples an age from the free-agent distribution using the given rng.
// It is deterministic for a fixed seed.
func poolAge(rng *rand.Rand) int {
	total := 0
	for _, b := range ageBands {
		total += b.weight
	}
	pick := rng.Intn(total)
	for _, b := range ageBands {
		if pick < b.weight {
			return b.lo + rng.Intn(b.hi-b.lo+1)
		}
		pick -= b.weight
	}
	return ageBands[len(ageBands)-1].lo
}

// SeedPool generates `size` free agents and persists them into player.players
// with club_id=NULL, status='free_agent'. countryID may be nil for the
// world-level bootstrap pool. The factory's rng is used for age distribution
// AND player generation so one deterministic seed drives the whole pool.
// Returns the new player ids.
func SeedPool(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	worldID uuid.UUID, countryID *uuid.UUID, size int,
	factory *playergen.PlayerFactory, ref time.Time,
) ([]uuid.UUID, error) {
	rng := factory.Rng()
	ids := make([]uuid.UUID, 0, size)
	for i := 0; i < size; i++ {
		age := poolAge(rng)
		gp, err := factory.CreatePlayerWithOptions(playergen.CreatePlayerOptions{
			MinAge: age,
			MaxAge: age,
			Origin: "generated",
		})
		if err != nil {
			return nil, fmt.Errorf("generate pool player %d: %w", i, err)
		}
		playerID, _, err := persistGeneratedPlayer(ctx, tx, worldID, nil, countryID, 0, gp, ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, playerID)
	}

	if pub != nil && size > 0 {
		actor := "system"
		_ = eventbus.WriteTx(ctx, pub, tx, &eventbus.Event{
			WorldID:   worldID,
			EventType: "POOL_SEEDED",
			ActorType: &actor,
			Payload: mustJSON(map[string]any{
				"world_id":   worldID,
				"count":      size,
				"country_id": countryID,
			}),
		})
	}

	return ids, nil
}

// PoolCount returns the number of free agents available in the pool for the
// given world and optional country.
func PoolCount(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, countryID *uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM player.players
		WHERE world_id = $1 AND club_id IS NULL AND status = 'free_agent'
		  AND ($2::uuid IS NULL OR country_id IS NOT DISTINCT FROM $2)`,
		worldID, countryID,
	).Scan(&n)
	return n, err
}

// ReplenishPool brings the free-agent pool back up to `target` by generating
// additional players as needed. It is idempotent: if the pool already holds
// at least `target` players it does nothing.
func ReplenishPool(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	worldID uuid.UUID, countryID *uuid.UUID, target int,
	factory *playergen.PlayerFactory, ref time.Time,
) error {
	n, err := PoolCount(ctx, tx, worldID, countryID)
	if err != nil {
		return fmt.Errorf("count pool: %w", err)
	}
	if n >= target {
		return nil
	}
	if _, err := SeedPool(ctx, tx, pub, worldID, countryID, target-n, factory, ref); err != nil {
		return fmt.Errorf("replenish pool: %w", err)
	}
	return nil
}

// ListFreeAgents returns a paginated slice of the pool's free agents together
// with a total count (for pagination). Overall rating is the canonical
// per-position OVR (squad.PositionalOverall, 99-capped) and results are
// ordered by it descending then by name for deterministic paging. The pool is
// bounded (PoolTargetSize per country), so the full slice is aggregated once
// and paginated in Go — SQL AVG-overall ordering is gone by design. An empty
// filter skips all optional predicates; position/age/nationality are applied
// after loading so paging always reflects the filtered set.
func ListFreeAgents(ctx context.Context, db Queryable,
	worldID uuid.UUID, filter FreeAgentFilter, page, pageSize int,
) ([]FreeAgent, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}

	var total int
	if err := db.QueryRow(ctx, `
		SELECT COUNT(*) FROM player.players p
		WHERE p.world_id = $1 AND p.club_id IS NULL AND p.status = 'free_agent'
		  AND ($2::uuid IS NULL OR p.country_id IS NOT DISTINCT FROM $2)`,
		worldID, filter.CountryID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count free agents: %w", err)
	}

	if total == 0 {
		return []FreeAgent{}, 0, nil
	}

	rows, err := db.Query(ctx, `
		SELECT p.id, pe.first_name, pe.last_name, pe.display_name,
		       pe.nationality_code, pe.date_of_birth, p.primary_position, p.origin,
		       COALESCE(p.market_value, 0)::bigint,
		       COALESCE(AVG(a.value) FILTER (WHERE a.attribute_category = 'technical'), 0)::int,
		       COALESCE(AVG(a.value) FILTER (WHERE a.attribute_category = 'physical'), 0)::int,
		       COALESCE(AVG(a.value) FILTER (WHERE a.attribute_category = 'mental'), 0)::int,
		       COALESCE(AVG(a.value) FILTER (WHERE a.attribute_category = 'tactical'), 0)::int,
		       COALESCE(AVG(a.value) FILTER (WHERE a.attribute_category = 'positional'), 0)::int,
		       COALESCE(AVG(a.value) FILTER (WHERE a.attribute_category = 'goalkeeping'), 0)::int
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_attributes a ON a.player_id = p.id
		WHERE p.world_id = $1 AND p.club_id IS NULL AND p.status = 'free_agent'
		  AND ($2::uuid IS NULL OR p.country_id IS NOT DISTINCT FROM $2)
		GROUP BY p.id, pe.id`,
		worldID, filter.CountryID,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query free agents: %w", err)
	}
	defer rows.Close()

	var cands []candidate
	for rows.Next() {
		var c candidate
		var dob interface{}
		var cats squad.AttributeSnapshot
		if err := rows.Scan(&c.fa.ID, &c.fa.FirstName, &c.fa.LastName, &c.fa.DisplayName,
			&c.fa.NationalityCode, &dob, &c.fa.PrimaryPosition, &c.fa.Origin,
			&c.fa.MarketValue, &cats.Technical, &cats.Physical, &cats.Mental,
			&cats.Tactical, &cats.Positional, &cats.Goalkeeping); err != nil {
			return nil, 0, fmt.Errorf("scan free agent: %w", err)
		}
		birth, _ := dob.(time.Time)
		if !birth.IsZero() {
			c.fa.DateOfBirth = birth.Format("2006-01-02")
		}
		c.fa.Age = yearsSince(birth, time.Now().UTC())
		c.ovr = squad.PositionalOverall(c.fa.PrimaryPosition, cats)
		c.fa.OverallRating = c.ovr
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate free agents: %w", err)
	}

	if len(cands) > 0 {
		cands = applyFreeAgentFilter(cands, filter)
		total = len(cands)
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].ovr != cands[j].ovr {
			return cands[i].ovr > cands[j].ovr
		}
		if cands[i].fa.LastName != cands[j].fa.LastName {
			return cands[i].fa.LastName < cands[j].fa.LastName
		}
		return cands[i].fa.FirstName < cands[j].fa.FirstName
	})

	lo := (page - 1) * pageSize
	if lo > len(cands) {
		lo = len(cands)
	}
	hi := lo + pageSize
	if hi > len(cands) {
		hi = len(cands)
	}
	agents := make([]FreeAgent, 0, hi-lo)
	for i := lo; i < hi; i++ {
		agents = append(agents, cands[i].fa)
	}

	// Second pass: person_id is needed for the API response but the first
	// query grouped by person, so we fetch it in one more shot.
	if len(agents) > 0 {
		ids := make([]uuid.UUID, len(agents))
		for i, fa := range agents {
			ids[i] = fa.ID
		}
		pRows, err := db.Query(ctx, `
			SELECT id, person_id FROM player.players WHERE id = ANY($1)`, ids)
		if err != nil {
			return nil, 0, fmt.Errorf("load person ids: %w", err)
		}
		defer pRows.Close()
		pMap := map[uuid.UUID]uuid.UUID{}
		for pRows.Next() {
			var pid, personID uuid.UUID
			if err := pRows.Scan(&pid, &personID); err != nil {
				return nil, 0, err
			}
			pMap[pid] = personID
		}
		if err := pRows.Err(); err != nil {
			return nil, 0, err
		}
		for i := range agents {
			agents[i].PersonID = pMap[agents[i].ID]
		}
	}

	return agents, total, nil
}

// Queryable is satisfied by both *pgxpool.Pool and pgx.Tx so callers can
// use the same query helpers in tests or inside a broader transaction.
type Queryable interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// candidate is a free-agent row plus its aggregated positional overall, used
// for in-memory sort/filter/pagination after the attribute aggregation pass.
type candidate struct {
	fa  FreeAgent
	ovr int
}

// yearsSince computes whole calendar years elapsed since birth.
func yearsSince(birth, ref time.Time) int {
	if birth.IsZero() {
		return 0
	}
	y := ref.Year() - birth.Year()
	if ref.Month() < birth.Month() || (ref.Month() == birth.Month() && ref.Day() < birth.Day()) {
		y--
	}
	if y < 0 {
		y = 0
	}
	return y
}

// applyFreeAgentFilter narrows a fully-loaded pool slice by the optional
// position/age/nationality predicates. Country scoping is already applied in
// SQL; these filters exist because overall requires attribute aggregation.
func applyFreeAgentFilter(cands []candidate, f FreeAgentFilter) []candidate {
	filtered := cands[:0]
	for _, c := range cands {
		if f.Position != nil && *f.Position != "" && c.fa.PrimaryPosition != *f.Position {
			continue
		}
		if f.AgeMin != nil && c.fa.Age < *f.AgeMin {
			continue
		}
		if f.AgeMax != nil && c.fa.Age > *f.AgeMax {
			continue
		}
		if f.Nationality != nil && *f.Nationality != "" && c.fa.NationalityCode != *f.Nationality {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}
