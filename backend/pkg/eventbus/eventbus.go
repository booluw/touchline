package eventbus

import (
	"context"

	"github.com/google/uuid"
)

// Event represents a typed event published to the event bus.
type Event struct {
	ID        uuid.UUID `json:"id"`
	Type      string    `json:"type"`
	WorldID   uuid.UUID `json:"world_id"`
	WorldTick int64     `json:"world_tick"`
	ActorID   *uuid.UUID `json:"actor_id,omitempty"`
	Payload   []byte    `json:"payload"`
	CausedBy  *uuid.UUID `json:"caused_by,omitempty"`
}

// EventBus defines the interface for event publishing and subscribing.
// Phase 0 implementation uses river (Postgres-backed).
// Can be swapped for NATS JetStream later without domain-logic changes.
type EventBus interface {
	Publish(ctx context.Context, event *Event) error
	Subscribe(ctx context.Context, eventType string, handler func(Event) error) error
}
