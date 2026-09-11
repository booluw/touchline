//go:build integration

package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// fastRetry backs off ~100ms so the retry test stays quick.
type fastRetry struct{}

func (fastRetry) NextRetry(_ *rivertype.JobRow) time.Time { return time.Now().Add(100 * time.Millisecond) }

// integrationDB returns a ready pool and applies all migrations. When
// TEST_DATABASE_URL is set it is used verbatim (CI / local cluster); otherwise
// a throwaway Postgres container is started.
func integrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		req := testcontainers.ContainerRequest{
			Image:        "postgres:16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     "touchline",
				"POSTGRES_PASSWORD": "touchline",
				"POSTGRES_DB":       "touchline",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections"),
		}
		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("start postgres container: %v", err)
		}
		t.Cleanup(func() { _ = container.Terminate(context.Background()) })

		host, err := container.Host(ctx)
		if err != nil {
			t.Fatalf("container host: %v", err)
		}
		port, err := container.MappedPort(ctx, "5432/tcp")
		if err != nil {
			t.Fatalf("container port: %v", err)
		}
		dbURL = fmt.Sprintf("postgres://touchline:touchline@%s:%s/touchline?sslmode=disable", host, port.Port())
	}

	m, err := migrate.New("file://../../migrations", dbURL)
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("apply migrations: %v", err)
	}
	_, _ = m.Close()

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Every test starts from an empty queue + event log so leftover jobs from
	// prior runs (in a long-lived DB like this one) can't leak into assertions.
	if _, err := pool.Exec(context.Background(),
		`TRUNCATE TABLE river.river_job, world.events, world.worlds RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test tables: %v", err)
	}
	return pool
}

func newTestBus(t *testing.T, pool *pgxpool.Pool, retryPolicy river.ClientRetryPolicy) *RiverBus {
	t.Helper()
	bus, err := NewRiverBus(pool, RiverBusConfig{RetryPolicy: retryPolicy})
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
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

// newWorld creates a world row so event inserts satisfy the FK into world.worlds.
func newWorld(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO world.worlds (id, name, status) VALUES ($1, 'it-world', 'provisioning')`, id); err != nil {
		t.Fatalf("insert world: %v", err)
	}
	return id
}

func jobCountForEvent(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM river.river_job WHERE args->>'event_id' = $1`, eventID.String(),
	).Scan(&n)
	if err != nil {
		t.Fatalf("count river_job: %v", err)
	}
	return n
}

func jobStateForEvent(t *testing.T, pool *pgxpool.Pool, eventID uuid.UUID) string {
	t.Helper()
	var state string
	err := pool.QueryRow(context.Background(),
		`SELECT state::text FROM river.river_job WHERE args->>'event_id' = $1 ORDER BY id DESC LIMIT 1`,
		eventID.String(),
	).Scan(&state)
	if err != nil {
		t.Fatalf("load river_job state: %v", err)
	}
	return state
}

// newCauseEvent inserts a minimal cause event row directly (no publish, so it
// doesn't enqueue work of its own) and returns its ID.
func newCauseEvent(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO world.events (id, world_id, world_tick, event_type) VALUES ($1, $2, 0, 'test.cause')`, id, worldID); err != nil {
		t.Fatalf("insert cause event: %v", err)
	}
	return id
}

func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }

// jsonEquals compares two JSON documents semantically (jsonb re-serializes with
// sorted keys and uniform spacing, so byte equality is not meaningful).
func jsonEquals(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return reflect.DeepEqual(va, vb)
}

func publishEvent(bus *RiverBus, worldID uuid.UUID, causeID *uuid.UUID) *Event {
	actorType := "system"
	seed := int64(424242)
	e := &Event{
		ID:              uuid.New(),
		WorldID:         worldID,
		WorldTick:       7,
		EventType:       "test.match_finished",
		ActorType:       &actorType,
		ActorID:         &worldID,
		Payload:         []byte(`{"home":3,"away":1}`),
		Explanation:     []byte(`{"why":"determinism seed"}`),
		CausedByEventID: causeID,
		RandomSeed:      &seed,
	}
	return e
}

func TestPublishConsumeRoundTrip(t *testing.T) {
	pool := integrationDB(t)
	bus := newTestBus(t, pool, nil)

	received := make(chan Event, 4)
	if err := bus.Subscribe(context.Background(), "test.match_finished", func(ev Event) error {
		received <- ev
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := bus.Start(context.Background()); err != nil {
		t.Fatalf("start bus: %v", err)
	}

	worldID := newWorld(t, pool)
	want := publishEvent(bus, worldID, uuidPtr(newCauseEvent(t, pool, worldID)))
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatalf("publish: %v", err)
	}

	waitFor(t, 20*time.Second, func() bool { return len(received) > 0 }, "handler invocation")

	got := <-received
	if got.ID != want.ID || got.EventType != want.EventType || got.WorldID != want.WorldID ||
		got.WorldTick != want.WorldTick || got.ActorID == nil || *got.ActorID != *want.ActorID ||
		got.ActorType == nil || *got.ActorType != "system" ||
		got.CausedByEventID == nil || *got.CausedByEventID != *want.CausedByEventID ||
		got.RandomSeed == nil || *got.RandomSeed != 424242 {
		t.Fatalf("dispatched event mismatch:\n got %+v\nwant %+v", got, want)
	}
	if !jsonEquals(t, got.Payload, []byte(`{"home":3,"away":1}`)) ||
		!jsonEquals(t, got.Explanation, []byte(`{"why":"determinism seed"}`)) {
		t.Fatalf("payload/explanation mismatch: payload=%s explanation=%s", got.Payload, got.Explanation)
	}
	if got.OccurredAt.IsZero() {
		t.Fatalf("occurred_at must be set on dispatch")
	}

	// Event log row round-tripped all columns.
	var actorType, payload, explanation string
	var seed int64
	err := pool.QueryRow(context.Background(),
		`SELECT actor_type, payload::text, explanation::text, random_seed FROM world.events WHERE id = $1`, want.ID,
	).Scan(&actorType, &payload, &explanation, &seed)
	if err != nil {
		t.Fatalf("load world.events row: %v", err)
	}
	if actorType != "system" || !jsonEquals(t, []byte(payload), []byte(`{"home":3,"away":1}`)) ||
		!jsonEquals(t, []byte(explanation), []byte(`{"why":"determinism seed"}`)) || seed != 424242 {
		t.Fatalf("world.events row mismatch: actor_type=%s payload=%s explanation=%s seed=%d", actorType, payload, explanation, seed)
	}

	// River queued and completed the job exactly as consumed.
	waitFor(t, 20*time.Second, func() bool { return jobStateForEvent(t, pool, want.ID) == "completed" }, "river job completion")
	if n := jobCountForEvent(t, pool, want.ID); n != 1 {
		t.Fatalf("expected exactly one river job, got %d", n)
	}
}

func TestDuplicatePublishDoesNotDoubleEnqueue(t *testing.T) {
	pool := integrationDB(t)
	bus := newTestBus(t, pool, nil)

	var mu sync.Mutex
	var invocations int
	if err := bus.Subscribe(context.Background(), "test.match_finished", func(ev Event) error {
		mu.Lock()
		invocations++
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Start(context.Background()); err != nil {
		t.Fatalf("start bus: %v", err)
	}

	e := publishEvent(bus, newWorld(t, pool), nil)
	if err := bus.Publish(context.Background(), e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// Republish of the same event ID is a full idempotent no-op: the log insert
	// is ON CONFLICT (id) DO NOTHING and the unique-by-args enqueue is skipped.
	if err := bus.Publish(context.Background(), e); err != nil {
		t.Fatalf("republish: %v", err)
	}

	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return invocations > 0
	}, "handler invocation")
	// Give any duplicate job a chance to fire, then assert all three layers.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	gotInvocations := invocations
	mu.Unlock()
	if gotInvocations != 1 {
		t.Fatalf("expected exactly one handler invocation, got %d", gotInvocations)
	}
	if n := jobCountForEvent(t, pool, e.ID); n != 1 {
		t.Fatalf("expected exactly one river job after duplicate publish, got %d", n)
	}
	var logRows int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM world.events WHERE id = $1`, e.ID,
	).Scan(&logRows); err != nil {
		t.Fatalf("count world.events rows: %v", err)
	}
	if logRows != 1 {
		t.Fatalf("expected exactly one world.events row after duplicate publish, got %d", logRows)
	}
}

func TestRetryThenSuccessNoDuplicateStateChange(t *testing.T) {
	pool := integrationDB(t)
	bus := newTestBus(t, pool, fastRetry{})

	if _, err := pool.Exec(context.Background(), `DROP TABLE IF EXISTS world.eventbus_it_effects`); err != nil {
		t.Fatalf("drop scratch table: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`CREATE TABLE world.eventbus_it_effects (event_id uuid PRIMARY KEY, seen_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatalf("create scratch table: %v", err)
	}

	var mu sync.Mutex
	attempts := 0
	handler := func(ev Event) error {
		mu.Lock()
		attempts++
		fail := attempts < 2 // fail the first two attempts to force retries
		mu.Unlock()
		if fail {
			return fmt.Errorf("transient failure")
		}
		// Idempotent side effect: keyed on event.ID so replays cannot double-apply.
		_, err := pool.Exec(context.Background(),
			`INSERT INTO world.eventbus_it_effects (event_id) VALUES ($1) ON CONFLICT (event_id) DO NOTHING`, ev.ID)
		return err
	}
	if err := bus.Subscribe(context.Background(), "test.match_finished", handler); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Start(context.Background()); err != nil {
		t.Fatalf("start bus: %v", err)
	}

	e := publishEvent(bus, newWorld(t, pool), nil)
	if err := bus.Publish(context.Background(), e); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Job must eventually complete (not silently discarded) after retries.
	waitFor(t, 30*time.Second, func() bool { return jobStateForEvent(t, pool, e.ID) == "completed" }, "job completion after retries")

	// Side effect applied exactly once despite at-least-once retries.
	var effects int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM world.eventbus_it_effects WHERE event_id = $1`, e.ID,
	).Scan(&effects); err != nil {
		t.Fatalf("count effects: %v", err)
	}
	if effects != 1 {
		t.Fatalf("expected exactly one idempotent side effect, got %d", effects)
	}
	mu.Lock()
	gotAttempts := attempts
	mu.Unlock()
	if gotAttempts < 2 {
		t.Fatalf("expected retries to have occurred, handler ran %d times", gotAttempts)
	}
}