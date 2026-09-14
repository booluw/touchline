package playerpool

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/pkg/eventbus"
)

// squadTemplateSlot associates a slot index with the primary positions it may
// accept. GK is rigid; the flexible slots accept any outfield position.
// Copied from bootstrap (A03) so playerpool has no bootstrap dependency.
func squadTemplate(size int) [][]string {
	template := [][]string{
		{"GK"}, {"GK"},
		{"CB", "LB", "RB"}, {"CB", "LB", "RB"}, {"CB", "LB", "RB"}, {"CB", "LB", "RB"},
		{"CB", "LB", "RB"}, {"CB", "LB", "RB"}, {"CB", "LB", "RB"},
		{"DM", "CM", "AM", "LM", "RM"}, {"DM", "CM", "AM", "LM", "RM"},
		{"DM", "CM", "AM", "LM", "RM"}, {"DM", "CM", "AM", "LM", "RM"},
		{"DM", "CM", "AM", "LM", "RM"}, {"DM", "CM", "AM", "LM", "RM"}, {"DM", "CM", "AM", "LM", "RM"},
		{"LW", "RW", "ST"}, {"LW", "RW", "ST"}, {"LW", "RW", "ST"},
		{"LW", "RW", "ST"}, {"LW", "RW", "ST"}, {"LW", "RW", "ST"},
	}
	out := make([][]string, 0, size)
	for i := 0; len(out) < size; i++ {
		slot := template[i%len(template)]
		if i >= len(template) {
			slot = []string{"CB", "LB", "RB", "DM", "CM", "AM", "LM", "RM", "LW", "RW", "ST"}
		}
		out = append(out, slot)
	}
	return out
}

// poolCandidate is a free-agent loaded from the DB for draft selection.
type poolCandidate struct {
	ID              uuid.UUID
	PersonID        uuid.UUID
	FirstName       string
	LastName        string
	DisplayName     string
	NationalityCode string
	DateOfBirth     time.Time
	PrimaryPosition string
	OverallRating   int
}

// DraftSquad picks `size` free agents from the pool via the position template
// and assigns them to `clubID`. Every drafted player gets club_id set,
// status='active', and a squad_number 1..size. The finance.ContractSeed
// returned per player lets the caller sign each player to a starter contract
// via finance.BootstrapClub. poolCountryID may be nil for the world-level
// bootstrap pool (nationality-agnostic).
//
// DraftSquad is idempotent per call: re-drafting without ReplenishPool
// returns ErrPoolTooSmall since previously-drafted players are no longer
// free agents.
func DraftSquad(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	worldID uuid.UUID, clubID uuid.UUID, poolCountryID *uuid.UUID, ref time.Time, size int,
) (*DraftResult, error) {
	template := squadTemplate(size)

	// Load every free agent in the pool, ordered by quality (overall) so the
	// draft fills each slot with the best available match.
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.person_id, pe.first_name, pe.last_name, pe.display_name,
		       pe.nationality_code, pe.date_of_birth, p.primary_position,
		       COALESCE(ROUND(AVG(a.value))::int, 0) AS overall
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN player.player_attributes a ON a.player_id = p.id
		WHERE p.world_id = $1 AND p.club_id IS NULL AND p.status = 'free_agent'
		  AND ($2::uuid IS NULL OR p.country_id IS NOT DISTINCT FROM $2)
		GROUP BY p.id, pe.id
		ORDER BY overall DESC, p.id`,
		worldID, poolCountryID,
	)
	if err != nil {
		return nil, fmt.Errorf("load free agents: %w", err)
	}
	defer rows.Close()

	var pool []poolCandidate
	for rows.Next() {
		var c poolCandidate
		if err := rows.Scan(&c.ID, &c.PersonID, &c.FirstName, &c.LastName,
			&c.DisplayName, &c.NationalityCode, &c.DateOfBirth,
			&c.PrimaryPosition, &c.OverallRating); err != nil {
			return nil, fmt.Errorf("scan pool candidate: %w", err)
		}
		pool = append(pool, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pool candidates: %w", err)
	}

	if len(pool) < size {
		return nil, ErrPoolTooSmall
	}

	// Greedy position-matching draft: for each template slot, pick the
	// highest-rated unmatched candidate whose position fits. If no fit exists
	// (pool exhaustion of a rare position), fall back to the best overall
	// remaining player. The deterministic ORDER BY guarantees replayable output
	// for a given seed-driven pool.
	remaining := pool // points into the same slice; shrunk as players are claimed
	used := make(map[uuid.UUID]bool, size)
	draft := make([]poolCandidate, 0, size)
	for _, allowed := range template {
		bestIdx := -1
		for i, c := range remaining {
			if used[c.ID] {
				continue
			}
			if contains(allowed, c.PrimaryPosition) {
				bestIdx = i
				break // remaining is sorted by overall desc, first match is best
			}
		}
		if bestIdx == -1 {
			// Fallback: take the best overall remaining player (may be
			// off-template, e.g. a GK pulled into an outfield slot).
			for i, c := range remaining {
				if !used[c.ID] {
					bestIdx = i
					break
				}
			}
		}
		if bestIdx == -1 {
			return nil, ErrPoolTooSmall
		}
		c := remaining[bestIdx]
		used[c.ID] = true
		draft = append(draft, c)
	}

	// Persist assignments and build the return slices.
	result := &DraftResult{ClubID: clubID, SquadSize: len(draft)}
	drafted := make([]DraftedPlayer, 0, len(draft))
	seeds := make([]finance.ContractSeed, 0, len(draft))

	for i, c := range draft {
		num := i + 1
		if _, err := tx.Exec(ctx, `
			UPDATE player.players
			SET club_id = $1, status = 'active', squad_number = $2
			WHERE id = $3`,
			clubID, num, c.ID,
		); err != nil {
			return nil, fmt.Errorf("assign player %s: %w", c.ID, err)
		}

		age := ageFor(ref, c.DateOfBirth)

		drafted = append(drafted, DraftedPlayer{
			PlayerID:        c.ID,
			PersonID:        c.PersonID,
			FirstName:       c.FirstName,
			LastName:        c.LastName,
			DisplayName:     c.DisplayName,
			NationalityCode: c.NationalityCode,
			DateOfBirth:     c.DateOfBirth,
			Age:             age,
			PrimaryPosition: c.PrimaryPosition,
			SquadNumber:     num,
			OverallRating:   c.OverallRating,
		})

		seeds = append(seeds, finance.ContractSeed{
			PlayerID:   c.ID,
			Position:   c.PrimaryPosition,
			Age:        age,
			Attributes: loadAttributes(ctx, tx, c.ID),
		})

		if err := recordEvent(ctx, pub, tx, worldID, EventClaimedFromPool, map[string]any{
			"player_id":       c.ID,
			"club_id":         clubID,
			"pool_country_id": poolCountryID,
		}); err != nil {
			return nil, err
		}
	}

	result.Players = drafted
	result.Seeds = seeds

	poolRemaining, err := PoolCount(ctx, tx, worldID, poolCountryID)
	if err != nil {
		return nil, err
	}
	result.PoolRemaining = poolRemaining

	return result, nil
}

// loadAttributes fetches the full attribute EAV for a player, keyed by
// attribute_key. It returns nil if no rows are found (should never happen
// for a persisted player).
func loadAttributes(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) map[string]int {
	rows, err := tx.Query(ctx, `
		SELECT attribute_key, value FROM player.player_attributes
		WHERE player_id = $1`, playerID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var v int
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		out[k] = v
	}
	_ = rows.Err()
	return out
}

// meanAttribute returns the arithmetic mean of all attribute values, or 0 if
// the map is empty. Used internally for the ListFreeAgents / attribute
// roll-up — the squad-level equivalent lives in internal/squad.
func meanAttribute(attrs map[string]int) int {
	if len(attrs) == 0 {
		return 0
	}
	sum := 0
	for _, v := range attrs {
		sum += v
	}
	return int(math.Round(float64(sum) / float64(len(attrs))))
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}