package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	if err := validateBus(b); err != nil {
		return err
	}
	if event == nil {
		return errors.New("eventbus: publish nil event")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("eventbus: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit decides the outcome

	if err := b.PublishTx(ctx, tx, event); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("eventbus: commit event: %w", err)
	}
	return nil
}

// PublishTx enqueues an event inside the caller's open transaction: it appends
// the world.events row and inserts the touchline_event river job in the SAME
// tx, so the record and its dispatch commit (or roll back) atomically with the
// producer's business state. Producers pass the tx that already mutates state —
// a committed event row can never exist without a queueable job (OPD-23). A
// nil or zero ID gets a fresh UUID; a zero OccurredAt defaults to now, both
// written back onto the in-memory event.
func (b *RiverBus) PublishTx(ctx context.Context, tx pgx.Tx, event *Event) error {
	if err := validateBus(b); err != nil {
		return err
	}
	if event == nil {
		return errors.New("eventbus: publish nil event")
	}

	normalizeEvent(event)

	// Duplicate publishes of the same ID are idempotent: the log row is a no-op
	// and the unique-by-args river job is deduped, so a re-publish (e.g. the
	// repair sweep) never double-dispatches.
	if err := RecordTx(ctx, tx, event); err != nil {
		return fmt.Errorf("eventbus: insert event log: %w", err)
	}

	// Passing nil opts lets EventJobArgs' own InsertOpts (unique by args) apply.
	if _, err := b.client.InsertTx(ctx, tx, &EventJobArgs{EventID: event.ID}, nil); err != nil {
		return fmt.Errorf("eventbus: enqueue event job: %w", err)
	}
	return nil
}

// EnqueueRepair re-enqueues a touchline_event job for an event that is already
// persisted in world.events but has never been dispatched (the repair sweep's
// recreation path, OPD-23). It is idempotent: the job is unique by args, so an
// event that already has a dispatch record is a no-op, and a second repair pass
// never enqueues a duplicate. The event row is not rewritten here — it already
// exists and is authoritative.
func (b *RiverBus) EnqueueRepair(ctx context.Context, eventID uuid.UUID) error {
	if err := validateBus(b); err != nil {
		return err
	}
	// Never enqueue a job whose event row is missing: the worker would retry a
	// load failure forever. The sweep and any manual repair see the truth.
	var exists bool
	if err := b.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM world.events WHERE id = $1)`, eventID).Scan(&exists); err != nil {
		return fmt.Errorf("eventbus: check event %s: %w", eventID, err)
	}
	if !exists {
		return fmt.Errorf("eventbus: cannot repair: event %s not found in world.events", eventID)
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("eventbus: begin repair tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // commit decides the outcome

	if _, err := b.client.InsertTx(ctx, tx, &EventJobArgs{EventID: eventID}, nil); err != nil {
		return fmt.Errorf("eventbus: enqueue repair job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("eventbus: commit repair tx: %w", err)
	}
	return nil
}

func validateBus(b *RiverBus) error {
	if b == nil || b.db == nil || b.client == nil {
		return errors.New("eventbus: bus has no database pool")
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
		e           Event
		payload     json.RawMessage
		explanation json.RawMessage
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
