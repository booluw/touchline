package world

import (
	"time"

	"github.com/google/uuid"
)

type World struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type WorldEvent struct {
	ID         uuid.UUID  `json:"id"`
	Type       string     `json:"type"` // PLAYER_SOLD, PLAYER_INJURED, TRANSFER_BID_RECEIVED, etc.
	OccurredAt string     `json:"occurred_at"`
	WorldTick  int64      `json:"world_tick"`
	ActorID    *uuid.UUID `json:"actor_id,omitempty"`  // manager who triggered it
	Payload    []byte     `json:"payload"`             // JSON, typed per event Type
	CausedBy   *uuid.UUID `json:"caused_by,omitempty"` // parent event ID, for causal chains
}

type NewsStory struct {
	ID        uuid.UUID `json:"id"`
	WorldID   uuid.UUID `json:"world_id"`
	Headline  string    `json:"headline"`
	Body      string    `json:"body"`
	EventID   uuid.UUID `json:"event_id"`
	CreatedAt string    `json:"created_at"`
}

type WorldConfig struct {
	ID               uuid.UUID `json:"id"`
	WorldID          uuid.UUID `json:"world_id"`
	MatchTickCadence string    `json:"match_tick_cadence"` // e.g. "*/15 * * * *" for 15-min
	HourlyCadence    string    `json:"hourly_cadence"`
	DailyCadence     string    `json:"daily_cadence"`
	WeeklyCadence    string    `json:"weekly_cadence"`
	MonthlyCadence   string    `json:"monthly_cadence"`
	SeasonalCadence  string    `json:"seasonal_cadence"`
}
