package eventbus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RiverBus is a Postgres-backed event bus using river.
type RiverBus struct {
	db *pgxpool.Pool
}

func NewRiverBus(db *pgxpool.Pool) *RiverBus {
	return &RiverBus{db: db}
}

func (b *RiverBus) Publish(ctx context.Context, event *Event) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	_, err = b.db.Exec(ctx,
		`INSERT INTO world.events (id, type, world_id, world_tick, actor_id, payload, caused_by, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		event.ID, event.Type, event.WorldID, event.WorldTick, event.ActorID, payload, event.CausedBy,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

func (b *RiverBus) Subscribe(ctx context.Context, eventType string, handler func(Event) error) error {
	// Phase 0: polling-based subscription from world.events table
	// Will be replaced by river job processing or NATS subscription
	_ = handler
	_ = eventType
	return nil
}
