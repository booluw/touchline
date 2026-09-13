//go:build integration

package eventoutbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/eventoutbox"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/eventbus"
)

// insertUndispatchedEvent writes a world.events row directly, simulating a
// pre-OPD-23 row (or one whose river job was lost): committed state, no job.
func insertUndispatchedEvent(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, eventType string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO world.events (id, world_id, world_tick, event_type, occurred_at)
		VALUES ($1, $2, 0, $3, now())`, id, worldID, eventType); err != nil {
		t.Fatalf("insert undispatched event: %v", err)
	}
	return id
}

func jobCountForEvent(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river.river_job WHERE args->>'event_id' = $1`, eventID.String()).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

// TestSweepReenqueuesOriginalIDsOnce proves the repair sweep end to end: rows
// in world.events with no dispatch job are found, re-enqueued by their ORIGINAL
// id (the same id the audit trail carries), delivered to a subscriber, and a
// second sweep is an idempotent no-op.
func TestSweepReenqueuesOriginalIDsOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	worldID := testdb.CreateWorld(t, pool, "sweep-it")
	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{})
	if err != nil {
		t.Fatalf("new river bus: %v", err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = bus.Stop(sctx)
	})

	received := make(chan eventbus.Event, 8)
	if err := bus.Subscribe(ctx, "test.outbox.sweep", func(ev eventbus.Event) error {
		received <- ev
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Start(ctx); err != nil {
		t.Fatalf("start bus: %v", err)
	}

	ids := map[uuid.UUID]bool{}
	for i := 0; i < 3; i++ {
		ids[insertUndispatchedEvent(t, pool, worldID, "test.outbox.sweep")] = true
	}

	rep, err := eventoutbox.Sweep(ctx, pool, bus, eventoutbox.Options{})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if rep.Repaired != 3 {
		t.Fatalf("repaired = %d, want 3", rep.Repaired)
	}
	if rep.Scanned < 3 {
		t.Fatalf("scanned = %d, want >= 3", rep.Scanned)
	}
	if rep.OldestLagSeconds <= 0 {
		t.Fatalf("oldest_lag_s = %f, want > 0 for an undispatched backlog", rep.OldestLagSeconds)
	}
	for i := 0; i < 3; i++ {
		select {
		case ev := <-received:
			if !ids[ev.ID] {
				t.Fatalf("delivered id %s is not one of the persisted ids", ev.ID)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("timed out waiting for swept deliveries")
		}
	}
	for id := range ids {
		if n := jobCountForEvent(t, pool, id); n != 1 {
			t.Fatalf("job count for %s = %d, want exactly 1", id, n)
		}
	}

	// Idempotent second sweep: nothing left to repair, and no double delivery.
	again, err := eventoutbox.Sweep(ctx, pool, bus, eventoutbox.Options{})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again.Repaired != 0 {
		t.Fatalf("second sweep repaired = %d, want 0", again.Repaired)
	}
	if again.OldestLagSeconds != 0 {
		t.Fatalf("second sweep oldest_lag_s = %f, want 0", again.OldestLagSeconds)
	}
	for id := range ids {
		if n := jobCountForEvent(t, pool, id); n != 1 {
			t.Fatalf("job count after re-sweep for %s = %d, want 1", id, n)
		}
	}
	select {
	case extra := <-received:
		t.Fatalf("second sweep double-delivered %s", extra.ID)
	default:
	}
}

// TestSweepHonorsBatchLimit verifies the sweep is cooperative: a bounded pass
// re-enqueues at most Limit rows and subsequent passes drain the rest.
func TestSweepHonorsBatchLimit(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	worldID := testdb.CreateWorld(t, pool, "sweep-limit")
	bus, err := eventbus.NewRiverBus(pool, eventbus.RiverBusConfig{})
	if err != nil {
		t.Fatalf("new river bus: %v", err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = bus.Stop(sctx)
	})
	if err := bus.Start(ctx); err != nil {
		t.Fatalf("start bus: %v", err)
	}

	ids := []uuid.UUID{}
	for i := 0; i < 5; i++ {
		ids = append(ids, insertUndispatchedEvent(t, pool, worldID, "test.outbox.sweep"))
	}

	rep, err := eventoutbox.Sweep(ctx, pool, bus, eventoutbox.Options{Limit: 2})
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if rep.Repaired != 2 {
		t.Fatalf("first sweep repaired = %d, want 2", rep.Repaired)
	}

	rep, err = eventoutbox.Sweep(ctx, pool, bus, eventoutbox.Options{Limit: 2})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if rep.Repaired != 2 {
		t.Fatalf("second sweep repaired = %d, want 2", rep.Repaired)
	}

	rep, err = eventoutbox.Sweep(ctx, pool, bus, eventoutbox.Options{Limit: 2})
	if err != nil {
		t.Fatalf("third sweep: %v", err)
	}
	if rep.Repaired != 1 {
		t.Fatalf("third sweep repaired = %d, want 1", rep.Repaired)
	}
	for _, id := range ids {
		if n := jobCountForEvent(t, pool, id); n != 1 {
			t.Fatalf("job count for %s = %d, want 1", id, n)
		}
	}
}
