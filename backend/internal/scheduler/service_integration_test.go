//go:build integration

package scheduler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/eventbus"
)

// newRiverBus builds the real Postgres-backed event bus used by the worker.
func newRiverBus(t *testing.T, pool *pgxpool.Pool) *eventbus.RiverBus {
	t.Helper()
	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{})
	if err != nil {
		t.Fatalf("new river bus: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = bus.Stop(ctx)
	})
	return bus
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

// entryIDFor returns the cron entry currently registered with the given spec.
func (f *fakeScheduler) entryIDFor(t *testing.T, spec string) cron.EntryID {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, e := range f.entries {
		if e.spec == spec {
			return id
		}
	}
	t.Fatalf("no cron entry registered for spec %q (have %v)", spec, f.specs())
	return 0
}

func granularityOf(t *testing.T, ev eventbus.Event) string {
	t.Helper()
	var payload struct {
		Granularity string `json:"granularity"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("decode tick payload: %v", err)
	}
	return payload.Granularity
}

func countWorldTicks(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM world.events WHERE world_id = $1 AND event_type = 'WORLD_TICK'`, worldID).Scan(&n); err != nil {
		t.Fatalf("count WORLD_TICK events: %v", err)
	}
	return n
}

func currentTick(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(),
		`SELECT current_tick FROM world.worlds WHERE id = $1`, worldID).Scan(&n); err != nil {
		t.Fatalf("load current_tick: %v", err)
	}
	return n
}

// TestConfiguredDailyTickArrivesAtWorker proves AC5 end to end: a configured
// daily cadence is read from world_config, registered by the scheduler, fired,
// and delivered through the real river round-trip to a worker handler for the
// intended world. It also proves AC3 (a cadence change takes effect within the
// next sync, no redeploy) and that pausing a world unregisters its clock.
func TestConfiguredDailyTickArrivesAtWorker(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	bus := newRiverBus(t, pool)
	received := make(chan eventbus.Event, 4)
	if err := bus.Subscribe(ctx, "WORLD_TICK", func(ev eventbus.Event) error {
		received <- ev
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	go func() {
		if err := bus.Start(ctx); err != nil {
			t.Errorf("start bus: %v", err)
		}
	}()

	worldSvc := internalworld.NewService(pool, bus)
	w, err := worldSvc.CreateWorld(ctx, "clock-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}
	// A dev-friendly daily cadence the test can wait for (the seeded default is
	// a real daily cron; any valid spec works once the clock re-reads config).
	if err := worldSvc.SetConfig(ctx, w.ID, "tick.daily_cadence", "0 0 * * *"); err != nil {
		t.Fatalf("set daily cadence: %v", err)
	}

	fake := newFakeScheduler()
	clock := newServiceWith(pool, bus, fake)
	clockCtx, stopClock := context.WithCancel(ctx)
	defer stopClock()
	done := make(chan error, 1)
	go func() { done <- clock.Run(clockCtx, 100*time.Millisecond) }()

	// The clock must pick the configured spec up from world_config on sync.
	waitFor(t, 5*time.Second, func() bool {
		for _, spec := range fake.specs() {
			if spec == "0 0 * * *" {
				return true
			}
		}
		return false
	}, "daily cadence registration")

	fake.run(fake.entryIDFor(t, "0 0 * * *"))

	waitFor(t, 10*time.Second, func() bool { return len(received) > 0 }, "WORLD_TICK delivery")
	ev := <-received
	if ev.WorldID != w.ID {
		t.Fatalf("tick arrived for world %s, want %s", ev.WorldID, w.ID)
	}
	if g := granularityOf(t, ev); g != "daily" {
		t.Fatalf("tick granularity = %q, want daily", g)
	}
	if ev.WorldTick != 1 {
		t.Fatalf("first tick world_tick = %d, want 1", ev.WorldTick)
	}

	// The event log and the monotonic counter agree with the delivered event.
	if n := countWorldTicks(t, pool, w.ID); n != 1 {
		t.Fatalf("expected 1 WORLD_TICK in event log, got %d", n)
	}
	if tick := currentTick(t, pool, w.ID); tick != 1 {
		t.Fatalf("world current_tick = %d, want 1", tick)
	}

	// AC3: changing the cadence while the clock runs replaces the schedule on
	// the next sync — no redeploy involved.
	if err := worldSvc.SetConfig(ctx, w.ID, "tick.daily_cadence", "30 0 * * *"); err != nil {
		t.Fatalf("change daily cadence: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool {
		for _, spec := range fake.specs() {
			if spec == "30 0 * * *" {
				return true
			}
		}
		return false
	}, "cadence change picked up on next sync")

	// Pausing unregisters the world's clock; a stale fire is a safe no-op.
	if _, err := worldSvc.SetStatus(ctx, w.ID, "paused"); err != nil {
		t.Fatalf("pause world: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return len(fake.specs()) == 0 }, "clock unregister on pause")
	if err := clock.FireTick(ctx, w.ID, "daily"); err != nil {
		t.Fatalf("fire tick on paused world: %v", err)
	}
	if n := countWorldTicks(t, pool, w.ID); n != 1 {
		t.Fatalf("paused world must not tick: got %d WORLD_TICK events", n)
	}
	if tick := currentTick(t, pool, w.ID); tick != 1 {
		t.Fatalf("paused world must not advance counter: got %d", tick)
	}

	stopClock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("world clock run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("world clock did not stop")
	}
}

func TestFireTickAdvancesCounterAndRecordsPayload(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	worldSvc := internalworld.NewService(pool, nil)
	clock := NewService(pool, nil)

	w, err := worldSvc.CreateWorld(ctx, "clock-counter")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	if err := clock.FireTick(ctx, w.ID, "daily"); err != nil {
		t.Fatalf("fire daily: %v", err)
	}
	if err := clock.FireTick(ctx, w.ID, "weekly"); err != nil {
		t.Fatalf("fire weekly: %v", err)
	}
	if tick := currentTick(t, pool, w.ID); tick != 2 {
		t.Fatalf("current_tick = %d, want 2 after two ticks", tick)
	}

	rows, err := pool.Query(ctx, `
		SELECT world_tick, event_type, actor_type, payload::text
		FROM world.events WHERE world_id = $1 AND event_type = 'WORLD_TICK' ORDER BY world_tick`, w.ID)
	if err != nil {
		t.Fatalf("query ticks: %v", err)
	}
	defer rows.Close()

	type row struct {
		tick    int64
		event   string
		actor   string
		payload string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.tick, &r.event, &r.actor, &r.payload); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 event rows, got %d", len(got))
	}
	if got[0].tick != 1 || got[0].event != "WORLD_TICK" || got[0].actor != "system" || got[0].payload != `{"granularity": "daily"}` {
		t.Fatalf("row 1 mismatch: %+v", got[0])
	}
	if got[1].tick != 2 || got[1].event != "WORLD_TICK" || got[1].actor != "system" || got[1].payload != `{"granularity": "weekly"}` {
		t.Fatalf("row 2 mismatch: %+v", got[1])
	}
}

func TestFireTickSkipsNonPlayableWorlds(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)

	worldSvc := internalworld.NewService(pool, nil)
	clock := NewService(pool, nil)

	for _, status := range []string{"paused", "archived"} {
		w, err := worldSvc.CreateWorld(ctx, "clock-"+status)
		if err != nil {
			t.Fatalf("create world: %v", err)
		}
		if status == "paused" {
			// paused requires a previous active state.
			if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
				t.Fatalf("launch world: %v", err)
			}
		}
		if _, err := worldSvc.SetStatus(ctx, w.ID, status); err != nil {
			t.Fatalf("set status %s: %v", status, err)
		}
		if err := clock.FireTick(ctx, w.ID, "daily"); err != nil {
			t.Fatalf("fire tick on %s world: %v", status, err)
		}
		if n := countWorldTicks(t, pool, w.ID); n != 0 {
			t.Fatalf("%s world must not tick: got %d", status, n)
		}
		if tick := currentTick(t, pool, w.ID); tick != 0 {
			t.Fatalf("%s world counter advanced to %d", status, tick)
		}
	}

	// A world that never existed is also a no-op rather than an error.
	if err := clock.FireTick(ctx, uuid.New(), "daily"); err != nil {
		t.Fatalf("fire tick on missing world: %v", err)
	}
}
