package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// DefaultRiverSchema is the Postgres schema that owns river objects. It is
// created by migrations/0016_river_migration.up.sql.
const DefaultRiverSchema = "river"

// RiverBusConfig configures a RiverBus.
type RiverBusConfig struct {
	// Schema is the Postgres schema that owns river objects.
	Schema string
	// Queue is the river queue events are enqueued to.
	Queue string
	// MaxWorkers bounds concurrent event handler invocations.
	MaxWorkers int
	// RetryPolicy customizes retry scheduling for failed jobs. When nil the
	// river DefaultClientRetryPolicy applies.
	RetryPolicy river.ClientRetryPolicy
}

// RiverBus is the Phase-0 EventBus implemented with river on top of Postgres.
// Publishing is transactional: the world.events row and the enqueued job are
// committed together, so the queue can never reference a missing event.
// One RiverBus serves both roles — publish (InsertTx, works before Start) and
// consume (Start runs the worker pool). api/scheduler use it to publish;
// the worker process additionally calls Start.
type RiverBus struct {
	db         *pgxpool.Pool
	client     *river.Client[pgx.Tx]
	dispatcher *EventDispatcher
	worker     *EventWorker
}

// NewRiverBus builds a RiverBus over the given pool. The database must already
// have the river schema applied (migrations/0016-0022 in this repo).
func NewRiverBus(db *pgxpool.Pool, cfg RiverBusConfig) (*RiverBus, error) {
	if cfg.Schema == "" {
		cfg.Schema = DefaultRiverSchema
	}
	if cfg.Queue == "" {
		cfg.Queue = river.QueueDefault
	}
	if cfg.MaxWorkers == 0 {
		cfg.MaxWorkers = 100
	}

	dispatcher := NewEventDispatcher()
	bus := &RiverBus{
		db:         db,
		dispatcher: dispatcher,
	}
	bus.worker = NewEventWorker(bus.loadEvent, dispatcher)

	workers := river.NewWorkers()
	river.AddWorker(workers, bus.worker)

	client, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			cfg.Queue: {MaxWorkers: cfg.MaxWorkers},
		},
		RetryPolicy: cfg.RetryPolicy,
		Schema:      cfg.Schema,
		Workers:     workers,
	})
	if err != nil {
		return nil, fmt.Errorf("eventbus: create river client: %w", err)
	}
	bus.client = client

	return bus, nil
}

// Publish appends the event to world.events and enqueues it for the worker in a
// single transaction. A nil or zero ID gets a fresh UUID; a zero OccurredAt
// defaults to now.
func (b *RiverBus) Publish(ctx context.Context, event *Event) error {
	if event == nil {
		return errors.New("eventbus: publish nil event")
	}
	if b.db == nil {
		return errors.New("eventbus: bus has no database pool")
	}
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now()
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("eventbus: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit decides the outcome

	if _, err := tx.Exec(ctx, `
		INSERT INTO world.events
			(id, world_id, world_tick, event_type, actor_type, actor_id,
			 payload, explanation, caused_by_event_id, random_seed, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING`,
		event.ID,
		event.WorldID,
		event.WorldTick,
		event.EventType,
		event.ActorType,
		event.ActorID,
		json.RawMessage(event.Payload),
		jsonOrNil(event.Explanation),
		event.CausedByEventID,
		event.RandomSeed,
		event.OccurredAt,
	); err != nil {
		return fmt.Errorf("eventbus: insert event log: %w", err)
	}

	// EventJobArgs.InsertOpts makes the enqueue unique by args; passing nil opts
	// lets the args' own InsertOpts apply.
	if _, err := b.client.InsertTx(ctx, tx, &EventJobArgs{EventID: event.ID}, nil); err != nil {
		return fmt.Errorf("eventbus: enqueue event job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("eventbus: commit event: %w", err)
	}
	return nil
}

// Subscribe registers an event-type handler on the shared dispatcher. Register
// before Start in the worker process.
func (b *RiverBus) Subscribe(ctx context.Context, eventType string, handler EventHandler) error {
	return b.dispatcher.Subscribe(eventType, handler)
}

// Start begins the river worker pool. Call this only in a process that should
// consume events (the worker binary).
func (b *RiverBus) Start(ctx context.Context) error {
	if err := b.client.Start(ctx); err != nil {
		return fmt.Errorf("eventbus: start river client: %w", err)
	}
	return nil
}

// Stop gracefully stops the river client.
func (b *RiverBus) Stop(ctx context.Context) error {
	return b.client.Stop(ctx)
}

// loadEvent reads the authoritative event row back from world.events. Any error
// propagates to the river worker as a retryable failure.
func (b *RiverBus) loadEvent(ctx context.Context, id uuid.UUID) (*Event, error) {
	var (
		e            Event
		payload      json.RawMessage
		explanation  json.RawMessage
	)
	err := b.db.QueryRow(ctx, `
		SELECT id, world_id, world_tick, event_type, actor_type, actor_id,
		       payload, explanation, caused_by_event_id, random_seed, occurred_at
		FROM world.events
		WHERE id = $1`, id,
	).Scan(
		&e.ID, &e.WorldID, &e.WorldTick, &e.EventType, &e.ActorType, &e.ActorID,
		&payload, &explanation, &e.CausedByEventID, &e.RandomSeed, &e.OccurredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("eventbus: event %s not found in world.events", id)
	}
	if err != nil {
		return nil, fmt.Errorf("eventbus: load event %s: %w", id, err)
	}
	e.Payload = payload
	e.Explanation = explanation
	return &e, nil
}

func jsonOrNil(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}