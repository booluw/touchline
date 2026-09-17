// Admin bulk player creation (A10): admins inject free agents directly into a
// country pool by declaring high-level metrics — count, age band, quality,
// optional positions and nationality, and the player origin. Generation runs
// through the same deterministic playergen pipeline as seeding; the factory's
// seeded rng drives ages, positions, names and attributes so a given world
// seed reproduces the same batch. Each batch emits one ADMIN_BULK_PLAYER_CREATED
// event.
package playerpool

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// EventAdminBulkPlayerCreated fires when an admin bulk-creates players into a
// country pool (A10).
const EventAdminBulkPlayerCreated = "ADMIN_BULK_PLAYER_CREATED"

// Quality bands for bulk creation. Each band's target overall range and the
// offset applied to the playergen baseline are listed so the admin-facing
// docs and the implementation stay in sync.
var qualityBands = map[string]struct {
	Label    string
	RangeMin int // intended overall range (inclusive) — informational
	RangeMax int
	Offset   int // qualityOffset passed to playergen
}{
	"low":   {"low", 35, 55, -10},
	"mid":   {"mid", 50, 70, 0},
	"high":  {"high", 65, 85, 15},
	"elite": {"elite", 80, 95, 25},
}

// QualityOffset resolves a quality label to the playergen offset. Returns 0
// and ok=false for unknown labels.
func QualityOffset(quality string) (int, bool) {
	b, ok := qualityBands[quality]
	if !ok {
		return 0, false
	}
	return b.Offset, true
}

// BulkOpts is the validated admin bulk-creation request.
type BulkOpts struct {
	Count       int      // 1..500
	MinAge      int      // inclusive
	MaxAge      int      // inclusive
	Quality     string   // low | mid | high | elite
	Positions   []string // empty = any valid position
	Nationality string   // empty = weighted nationality draw
	Origin      string   // default "generated"
}

// Validate checks the request invariants: a bounded count, a valid quality
// label, a sane age band, and positions restricted to the playergen catalogue.
func (o BulkOpts) Validate() error {
	if o.Count < 1 || o.Count > 500 {
		return fmt.Errorf("count must be within 1..500, got %d", o.Count)
	}
	if _, ok := qualityBands[o.Quality]; !ok {
		return fmt.Errorf("quality must be one of low|mid|high|elite, got %q", o.Quality)
	}
	if o.MinAge < 13 || o.MaxAge > 38 || o.MinAge > o.MaxAge {
		return fmt.Errorf("age band %d..%d must be within 13..38", o.MinAge, o.MaxAge)
	}
	for _, p := range o.Positions {
		known := false
		for _, v := range playergen.ValidPositions {
			if p == v {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown position %q", p)
		}
	}
	return nil
}

// BulkCreate generates `count` players with the given metrics and persists
// them into the country pool as free agents (club_id NULL, status
// 'free_agent', country_id set). Returns the new player ids.
func BulkCreate(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
	worldID, countryID uuid.UUID, opts BulkOpts,
	factory *playergen.PlayerFactory, ref time.Time,
) ([]uuid.UUID, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	offset, _ := QualityOffset(opts.Quality)
	if opts.Origin == "" {
		opts.Origin = "generated"
	}

	ids := make([]uuid.UUID, 0, opts.Count)
	for i := 0; i < opts.Count; i++ {
		gp, err := factory.CreatePlayerWithOptions(playergen.CreatePlayerOptions{
			MinAge:        opts.MinAge,
			MaxAge:        opts.MaxAge,
			QualityOffset: offset,
			Nationality:   opts.Nationality,
			Positions:     opts.Positions,
			Origin:        opts.Origin,
		})
		if err != nil {
			return nil, fmt.Errorf("generate bulk player %d: %w", i, err)
		}
		playerID, _, err := persistGeneratedPlayer(ctx, tx, worldID, nil, &countryID, 0, gp, ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, playerID)
	}

	actor := "admin"
	_ = eventbus.WriteTx(ctx, pub, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: EventAdminBulkPlayerCreated,
		ActorType: &actor,
		Payload: mustJSON(map[string]any{
			"country_id": countryID,
			"count":      len(ids),
			"quality":    opts.Quality,
			"origin":     opts.Origin,
			"player_ids": ids,
		}),
	})
	return ids, nil
}
