package eventbus

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Event is the canonical shape of an event that crosses the bus. Its fields
// mirror the world.events table, so a published event round-trips losslessly
// into the event log and out to subscribers.
type Event struct {
	ID              uuid.UUID `json:"id"`
	WorldID         uuid.UUID `json:"world_id"`
	WorldTick       int64     `json:"world_tick"`
	EventType       string    `json:"event_type"`
	ActorType       *string   `json:"actor_type,omitempty"`
	ActorID         *uuid.UUID `json:"actor_id,omitempty"`
	Payload         []byte    `json:"payload"`
	Explanation     []byte    `json:"explanation,omitempty"`
	CausedByEventID *uuid.UUID `json:"caused_by_event_id,omitempty"`
	RandomSeed      *int64    `json:"random_seed,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

// EventHandler processes a single delivered event. The bus delivers events
// at-least-once, so a handler may run more than once for the same event after a
// crash or retry. Handlers MUST be idempotent: any side effect has to be keyed
// on event.ID so replays do not create duplicate state changes.
type EventHandler func(Event) error

// EventBus defines the interface for event publishing and subscribing.
// Phase 0 implementation uses river (Postgres-backed).
// Can be swapped for NATS JetStream later without domain-logic changes.
type EventBus interface {
	Publish(ctx context.Context, event *Event) error
	Subscribe(ctx context.Context, eventType string, handler EventHandler) error
}