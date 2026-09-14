package playerpool

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
				"world_id":  worldID,
				"count":     size,
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
// with a total count (for pagination). Results are ordered by overall rating
// descending then by name for deterministic paging.
func ListFreeAgents(ctx context.Context, db Queryable,
	worldID uuid.UUID, countryID *uuid.UUID, page, pageSize int,
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
		worldID, countryID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count free agents: %w", err)
	}

	if total == 0 {
		return []FreeAgent{}, 0, nil
	}

	offset := (page - 1) * pageSize
	rows, err := db.Query(ctx, `
		SELECT p.id, pe.first_name, pe.last_name, pe.display_name,
		       pe.nationality_code, pe.date_of_birth, p.primary_position, p.origin,
		       COALESCE(ROUND(AVG(a.value))::int, 0) AS overall,
		       COALESCE(p.market_value, 0)::bigint
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_attributes a ON a.player_id = p.id
		WHERE p.world_id = $1 AND p.club_id IS NULL AND p.status = 'free_agent'
		  AND ($2::uuid IS NULL OR p.country_id IS NOT DISTINCT FROM $2)
		GROUP BY p.id, pe.id
		ORDER BY overall DESC, pe.last_name, pe.first_name
		LIMIT $3 OFFSET $4`,
		worldID, countryID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("query free agents: %w", err)
	}
	defer rows.Close()

	var agents []FreeAgent
	for rows.Next() {
		var fa FreeAgent
		var dob interface{}
		if err := rows.Scan(&fa.ID, &fa.FirstName, &fa.LastName, &fa.DisplayName,
			&fa.NationalityCode, &dob, &fa.PrimaryPosition, &fa.Origin,
			&fa.OverallRating, &fa.MarketValue); err != nil {
			return nil, 0, fmt.Errorf("scan free agent: %w", err)
		}
		if t, ok := dob.(interface{ Format(layout string) string }); ok {
			fa.DateOfBirth = t.Format("2006-01-02")
		}
		fa.PersonID = uuid.Nil // populated below
		agents = append(agents, fa)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate free agents: %w", err)
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