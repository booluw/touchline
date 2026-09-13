package eventbus

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Event is the canonical shape of an event that crosses the bus. Its fields
// mirror the world.events table, so a published event round-trips losslessly
// into the event log and out to subscribers.
type Event struct {
	ID              uuid.UUID  `json:"id"`
	WorldID         uuid.UUID  `json:"world_id"`
	WorldTick       int64      `json:"world_tick"`
	EventType       string     `json:"event_type"`
	ActorType       *string    `json:"actor_type,omitempty"`
	ActorID         *uuid.UUID `json:"actor_id,omitempty"`
	Payload         []byte     `json:"payload"`
	Explanation     []byte     `json:"explanation,omitempty"`
	CausedByEventID *uuid.UUID `json:"caused_by_event_id,omitempty"`
	RandomSeed      *int64     `json:"random_seed,omitempty"`
	OccurredAt      time.Time  `json:"occurred_at"`
}

// EventHandler processes a single delivered event. The bus delivers events
// at-least-once, so a handler may run more than once for the same event after a
// crash or retry. Handlers MUST be idempotent: any side effect has to be keyed
// on event.ID so replays do not create duplicate state changes.
type EventHandler func(Event) error

// EventBus defines the interface for event publishing and subscribing.
// Phase 0 implementation uses river (Postgres-backed).
// Can be swapped for NATS JetStream later without domain-logic changes.
// PublishTx (the transactional outbox, OPD-23) is part of the contract: an
// implementation must be able to record an event and enqueue its dispatch
// inside a caller's open transaction.
type EventBus interface {
	Publish(ctx context.Context, event *Event) error
	PublishTx(ctx context.Context, tx pgx.Tx, event *Event) error
	Subscribe(ctx context.Context, eventType string, handler EventHandler) error
}

// Publisher is the event sink producers push through. It is intentionally
// narrower than EventBus and tx-scoped: the event row and its dispatch job are
// written inside the caller's transaction (the transactional outbox, OPD-23),
// so a committed state change is never left undispatched.
type Publisher interface {
	PublishTx(ctx context.Context, tx pgx.Tx, event *Event) error
}

// RecordTx appends event e to the authoritative world.events log inside
// tx without enqueueing any dispatch job. Shared by every producer path so the
// log row is always written even when no bus (Publisher) is wired; a zero ID or
// OccurredAt is filled before writing so the in-memory event always matches the
// persisted identity.
func RecordTx(ctx context.Context, tx pgx.Tx, e *Event) error {
	normalizeEvent(e)
	_, err := tx.Exec(ctx, `
		INSERT INTO world.events
			(id, world_id, world_tick, event_type, actor_type, actor_id,
			 payload, explanation, caused_by_event_id, random_seed, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING`,
		e.ID,
		e.WorldID,
		e.WorldTick,
		e.EventType,
		e.ActorType,
		e.ActorID,
		payloadRaw(e.Payload),
		jsonOrNil(e.Explanation),
		e.CausedByEventID,
		e.RandomSeed,
		e.OccurredAt,
	)
	if err != nil {
		return err
	}
	return nil
}

// WriteTx records event e into world.events and, when pub is non-nil, enqueues
// its dispatch job in the same transaction. nil pub means log-only: the row is
// still the authoritative audit trail, but no river job is created. Producers
// MUST call this inside the same tx that mutates their business state.
func WriteTx(ctx context.Context, pub Publisher, tx pgx.Tx, e *Event) error {
	if pub != nil {
		return pub.PublishTx(ctx, tx, e)
	}
	return RecordTx(ctx, tx, e)
}

// normalizeEvent fills a zero ID and zero OccurredAt so the in-memory event is
// always identical to what gets persisted (replay, dispatch, causal chains).
func normalizeEvent(e *Event) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
}

func payloadRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

func jsonOrNil(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}
