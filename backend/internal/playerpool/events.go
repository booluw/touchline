package playerpool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

// recordEvent appends one world.events row inside the caller's transaction as
// a system actor (bootstrap-era or automated pool activity) — the same
// transactional-outbox contract bootstrap and competition follow.
func recordEvent(ctx context.Context, pub eventbus.Publisher, tx pgx.Tx, worldID uuid.UUID, eventType string, payload map[string]any) error {
	actor := "system"
	e := &eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		ActorType: &actor,
		Payload:   mustJSON(payload),
	}
	if err := eventbus.WriteTx(ctx, pub, tx, e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// mustJSON marshals a value without a second thought — callers pass only
// structs/slices of primitives that cannot fail to encode.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("playerpool: marshal event payload: %v", err))
	}
	return b
}
