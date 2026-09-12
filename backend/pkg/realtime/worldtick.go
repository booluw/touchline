package realtime

import (
	"github.com/google/uuid"
)

// WorldTickPayload is the payload carried by a world_tick event. It is the
// S02-04 proof shape pushed to browsers after the worker consumes a
// WORLD_TICK event from the event bus.
type WorldTickPayload struct {
	EventID     string `json:"event_id"`
	Granularity string `json:"granularity"`
	Tick        int64  `json:"tick"`
}

// BuildWorldTick builds the world_tick envelope for a consumed WORLD_TICK
// event. cmd/worker and the Phase-0 slice integration test both use it so the
// wire shape has a single source of truth.
func BuildWorldTick(worldID uuid.UUID, eventID, granularity string, tick int64) (Event, error) {
	return NewEvent(EventWorldTick, worldID, WorldTickPayload{
		EventID:     eventID,
		Granularity: granularity,
		Tick:        tick,
	})
}
