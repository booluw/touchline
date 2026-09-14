//go:build integration

package competition

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// competitionEventTypes are the spine event types the replay/audit contract
// covers (OPD-23/competition-event-spine): seed, per-season completion,
// movement, and next-season creation.
var competitionEventTypes = []string{
	"COMPETITION_SEEDED",
	"SEASON_CREATED",
	"SEASON_COMPLETED",
	"CLUB_PROMOTED",
	"CLUB_RELEGATED",
}

// runningBus builds a live river bus that records every competition spine event
// it dispatches.
func runningBus(t *testing.T, pool *pgxpool.Pool) (chan eventbus.Event, *eventbus.RiverBus) {
	t.Helper()
	ctx := context.Background()
	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{})
	if err != nil {
		t.Fatalf("new river bus: %v", err)
	}
	delivered := make(chan eventbus.Event, 64)
	for _, typ := range competitionEventTypes {
		if err := bus.Subscribe(ctx, typ, func(ev eventbus.Event) error {
			delivered <- ev
			return nil
		}); err != nil {
			t.Fatalf("subscribe %s: %v", typ, err)
		}
	}
	if err := bus.Start(ctx); err != nil {
		t.Fatalf("start bus: %v", err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = bus.Stop(sctx)
	})
	return delivered, bus
}

// TestCompetitionEventsDispatchPersistedIDs is the replay/audit gate for the
// competition event spine: seed + rollover events are both persisted and
// dispatched, the delivered event id is EXACTLY the persisted id (so replays
// keyed on ID are causally consistent), and the ordered rollover sequence
// (SEASON_COMPLETED → CLUB_PROMOTED/CLUB_RELEGATED → SEASON_CREATED) is
// observable in one coherent pass.
func TestCompetitionEventsDispatchPersistedIDs(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()

	delivered, bus := runningBus(t, pool)
	svc := NewService(pool, bus)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier season: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ season: %v", err)
	}

	// Play the full season; the last result triggers the rollover cascade.
	rows, err := pool.Query(ctx, `
		SELECT id FROM match.fixtures WHERE world_id = $1 ORDER BY scheduled_at, id`, worldID)
	if err != nil {
		t.Fatalf("list fixtures: %v", err)
	}
	var fixtureIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan fixture: %v", err)
		}
		fixtureIDs = append(fixtureIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fixtures: %v", err)
	}
	for _, id := range fixtureIDs {
		if err := svc.ApplyResult(ctx, id, 2, 1); err != nil {
			t.Fatalf("apply result: %v", err)
		}
	}

	wantCounts := map[string]int{
		"COMPETITION_SEEDED": 1,
		"SEASON_CREATED":     4, // 2 started + 2 rollover (one per league)
		"SEASON_COMPLETED":   2,
		"CLUB_PROMOTED":      1,
		"CLUB_RELEGATED":     1,
	}
	total := 0
	for _, n := range wantCounts {
		total += n
	}

	got := map[string][]uuid.UUID{}
	for i := 0; i < total; i++ {
		select {
		case ev := <-delivered:
			got[ev.EventType] = append(got[ev.EventType], ev.ID)
		case <-time.After(30 * time.Second):
			t.Fatalf("timed out; dispatched so far: %v", got)
		}
	}

	persisted := func(eventType string) []uuid.UUID {
		rows, err := pool.Query(ctx, `
			SELECT id FROM world.events
			WHERE world_id = $1 AND event_type = $2 ORDER BY occurred_at, id`, worldID, eventType)
		if err != nil {
			t.Fatalf("query persisted %s: %v", eventType, err)
		}
		defer rows.Close()
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan persisted %s: %v", eventType, err)
			}
			ids = append(ids, id)
		}
		return ids
	}

	for typ, want := range wantCounts {
		if len(got[typ]) != want {
			t.Fatalf("dispatched %s = %d, want %d", typ, len(got[typ]), want)
		}
		if len(persisted(typ)) != want {
			t.Fatalf("persisted %s = %d, want %d", typ, len(persisted(typ)), want)
		}
		for _, id := range got[typ] {
			if !containsID(persisted(typ), id) {
				t.Fatalf("delivered %s id %s diverges from the persisted id set", typ, id)
			}
		}
	}
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

// failingBus is a Publisher whose PublishTx always fails, standing in for a
// broken dispatch path (OPD-23): a competition event that cannot be enqueued
// must abort the enclosing competition state change.
type failingBus struct{}

func (failingBus) PublishTx(context.Context, pgx.Tx, *eventbus.Event) error {
	return errors.New("bus: injectable publish failure")
}

// TestSeedWorldEventFailureAbortsState proves that a seed whose event
// enqueue fails persists NOTHING: no clubs, no league memberships, no world
// seed, and no events — not a half-materialized pyramid.
func TestSeedWorldEventFailureAbortsState(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()

	svc := NewService(pool, failingBus{})
	twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err == nil {
		t.Fatal("seed must fail when the event enqueue fails")
	}

	var clubs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM club.clubs WHERE world_id = $1`, worldID).Scan(&clubs); err != nil {
		t.Fatalf("count clubs: %v", err)
	}
	if clubs != 0 {
		t.Fatalf("clubs = %d, want 0 after rolled-back seed", clubs)
	}
	var memberships int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM competition.club_competitions WHERE world_id = $1`, worldID).Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if memberships != 0 {
		t.Fatalf("memberships = %d, want 0 after rollback", memberships)
	}
	var seedNull bool
	if err := pool.QueryRow(ctx,
		`SELECT world_seed IS NULL FROM world.worlds WHERE id = $1`, worldID).Scan(&seedNull); err != nil {
		t.Fatalf("load world_seed nullability: %v", err)
	}
	if !seedNull {
		t.Fatal("world_seed is set despite the rolled-back seed")
	}
	var events int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM world.events
		WHERE world_id = $1 AND event_type IN ('WORLD_SEEDED','COMPETITION_SEEDED')`,
		worldID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 0 {
		t.Fatalf("seed events = %d, want 0 after rollback", events)
	}
	var fixtures int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM match.fixtures WHERE world_id = $1`, worldID).Scan(&fixtures); err != nil {
		t.Fatalf("count fixtures: %v", err)
	}
	if fixtures != 0 {
		t.Fatalf("fixtures = %d, want 0 after rolled-back seed", fixtures)
	}
}

// TestRolloverEventFailureAbortsSeasonCompletion plays a full season with a
// healthy bus, then trips the failing bus on the very last result. The atomic
// rollover must abort: the final fixture stays unapplied, season 1 stays open,
// season 2 does not exist, and no SEASON_COMPLETED event survives.
func TestRolloverEventFailureAbortsSeasonCompletion(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()

	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier season: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ season: %v", err)
	}

	var fixtures []struct {
		ID uuid.UUID
	}
	rows, err := pool.Query(ctx, `
		SELECT id FROM match.fixtures WHERE world_id = $1 ORDER BY scheduled_at, id`, worldID)
	if err != nil {
		t.Fatalf("list fixtures: %v", err)
	}
	for rows.Next() {
		var f struct{ ID uuid.UUID }
		if err := rows.Scan(&f.ID); err != nil {
			rows.Close()
			t.Fatalf("scan fixture: %v", err)
		}
		fixtures = append(fixtures, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fixtures: %v", err)
	}
	if len(fixtures) != 24 {
		t.Fatalf("fixtures = %d, want 24", len(fixtures))
	}

	svcFail := NewService(pool, failingBus{})
	for i, f := range fixtures {
		// The last result trips the rollover, whose event enqueue fails — the
		// failing enqueue must surface as an error and abort the whole tx.
		if i == len(fixtures)-1 {
			if err := svcFail.ApplyResult(ctx, f.ID, 2, 1); err == nil {
				t.Fatal("last apply must fail when the rollover event enqueue fails")
			}
			continue
		}
		if err := svc.ApplyResult(ctx, f.ID, 2, 1); err != nil {
			t.Fatalf("apply fixture %d/%d: %v", i+1, len(fixtures), err)
		}
	}

	// The failing last write must have surfaced as an error.
	last := fixtures[len(fixtures)-1].ID
	var appliedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT standings_applied_at FROM match.fixtures WHERE id = $1`, last).Scan(&appliedAt); err != nil {
		t.Fatalf("load last fixture: %v", err)
	}
	if appliedAt != nil {
		t.Fatal("last fixture was applied despite the rollover aborting")
	}

	var openSeasons, season2 int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM competition.seasons
		WHERE competition_id IN ($1,$2) AND world_id = $3 AND status <> 'completed'`,
		premier.ID, champ.ID, worldID).Scan(&openSeasons); err != nil {
		t.Fatalf("count open seasons: %v", err)
	}
	if openSeasons != 2 {
		t.Fatalf("open seasons = %d, want 2 (rollover must have aborted)", openSeasons)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM competition.seasons
		WHERE competition_id IN ($1,$2) AND world_id = $3 AND season_number = 2`,
		premier.ID, champ.ID, worldID).Scan(&season2); err != nil {
		t.Fatalf("count season 2: %v", err)
	}
	if season2 != 0 {
		t.Fatalf("season 2 rows = %d, want 0 after aborted rollover", season2)
	}
	var completedEvents int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM world.events WHERE world_id = $1 AND event_type = 'SEASON_COMPLETED'`,
		worldID).Scan(&completedEvents); err != nil {
		t.Fatalf("count season completed events: %v", err)
	}
	if completedEvents != 0 {
		t.Fatalf("SEASON_COMPLETED events = %d, want 0 after aborted rollover", completedEvents)
	}
}
