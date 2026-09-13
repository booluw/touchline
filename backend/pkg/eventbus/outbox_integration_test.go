//go:build integration

package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestPublishTxRollsBackEventAndJobTogether is the OPD-23 atomicity gate: the
// world.events row and its river dispatch job commit (or vanish) as one unit
// with the enclosing tx. A poisoned tx must leave neither behind, and a clean
// commit must leave both.
func TestPublishTxRollsBackEventAndJobTogether(t *testing.T) {
	pool := integrationDB(t)
	bus := newTestBus(t, pool, nil)
	ctx := context.Background()

	worldID := newWorld(t, pool)
	mk := func() *Event {
		actor := "system"
		return &Event{
			ID:        uuid.New(),
			WorldID:   worldID,
			WorldTick: 0,
			EventType: "test.outbox_atomic",
			ActorType: &actor,
			Payload:   []byte(`{"k":"v"}`),
		}
	}

	// 1. Publish inside a tx, then poison the tx. Rollback must cancel BOTH the
	// event row and the queued job.
	bad := mk()
	badTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := bus.PublishTx(ctx, badTx, bad); err != nil {
		_ = badTx.Rollback(ctx)
		t.Fatalf("publish tx: %v", err)
	}
	if _, err := badTx.Exec(ctx, `INSERT INTO definitely_not_a_table VALUES (1)`); err == nil {
		_ = badTx.Rollback(ctx)
		t.Fatal("poison statement unexpectedly succeeded")
	}
	_ = badTx.Rollback(ctx)

	if err := pool.QueryRow(ctx,
		`SELECT 1 FROM world.events WHERE id = $1`, bad.ID).Scan(new(int)); err != pgx.ErrNoRows {
		t.Fatalf("event row survived a rolled-back tx (err=%v)", err)
	}
	if n := jobCountForEvent(t, pool, bad.ID); n != 0 {
		t.Fatalf("job survived a rolled-back tx: %d", n)
	}

	// 2. The identical write in a clean committed tx leaves both behind.
	good := mk()
	goodTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := bus.PublishTx(ctx, goodTx, good); err != nil {
		_ = goodTx.Rollback(ctx)
		t.Fatalf("publish tx: %v", err)
	}
	if err := goodTx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	var present int
	if err := pool.QueryRow(ctx,
		`SELECT 1 FROM world.events WHERE id = $1`, good.ID).Scan(&present); err != nil {
		t.Fatalf("event row missing after commit: %v", err)
	}
	if n := jobCountForEvent(t, pool, good.ID); n != 1 {
		t.Fatalf("job count after commit = %d, want 1", n)
	}
}

// TestEnqueueRepairReplaysOriginalIDOnce is the repair-sweep idempotency gate:
// a committed-but-undispatched event is re-enqueued by its ORIGINAL id, is
// delivered exactly once per unique args, and a second repair enqueue is a
// no-op that cannot double-dispatch.
func TestEnqueueRepairReplaysOriginalIDOnce(t *testing.T) {
	pool := integrationDB(t)
	bus := newTestBus(t, pool, nil)
	ctx := context.Background()

	worldID := newWorld(t, pool)
	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO world.events (id, world_id, world_tick, event_type, occurred_at)
		 VALUES ($1, $2, 0, 'test.repair_gate', now())`, id, worldID); err != nil {
		t.Fatalf("insert undispatched event: %v", err)
	}

	received := make(chan Event, 4)
	if err := bus.Subscribe(ctx, "test.repair_gate", func(ev Event) error {
		received <- ev
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Start(ctx); err != nil {
		t.Fatalf("start bus: %v", err)
	}

	if err := bus.EnqueueRepair(ctx, id); err != nil {
		t.Fatalf("enqueue repair: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool { return len(received) > 0 }, "repair delivery")

	got := <-received
	if got.ID != id {
		t.Fatalf("delivered id %s, want the original %s", got.ID, id)
	}
	if n := jobCountForEvent(t, pool, id); n != 1 {
		t.Fatalf("job count = %d, want exactly 1", n)
	}

	// A second repair (and a third) must be deduped by river's unique-by-args.
	if err := bus.EnqueueRepair(ctx, id); err != nil {
		t.Fatalf("re-enqueue repair: %v", err)
	}
	if err := bus.EnqueueRepair(ctx, id); err != nil {
		t.Fatalf("third re-enqueue repair: %v", err)
	}
	if n := jobCountForEvent(t, pool, id); n != 1 {
		t.Fatalf("job count after re-repair = %d, want still 1", n)
	}
}

// TestEnqueueRepairMissingEventErrors ensures the repair path surfaces a broken
// log rather than silently enqueueing a job that would fail to load.
func TestEnqueueRepairMissingEventErrors(t *testing.T) {
	pool := integrationDB(t)
	bus := newTestBus(t, pool, nil)

	if err := bus.EnqueueRepair(context.Background(), uuid.New()); err == nil {
		t.Fatal("enqueue repair for a missing event must error")
	}
}
