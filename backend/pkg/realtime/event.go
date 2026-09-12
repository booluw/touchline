package realtime

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event is the typed envelope that crosses the single client socket. It is
// deliberately generic: the Type string routes it (match_tick, notification,
// world_tick, ...) and Payload carries the feature-specific JSON. It mirrors
// the frontend SocketEvent shape in composables/useSocket.ts.
//
// WorldID scopes delivery: a hub only forwards an event to clients registered
// for that world, so clients can never observe another world's stream.
type Event struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	WorldID *uuid.UUID      `json:"world_id,omitempty"`
	TS      time.Time       `json:"ts"`
}

// NewEvent builds an event with a payload marshalled to raw JSON. An empty
// payload is allowed for control messages such as pong.
func NewEvent(eventType string, worldID uuid.UUID, payload any) (Event, error) {
	ev := Event{Type: eventType, TS: time.Now().UTC()}
	if worldID != uuid.Nil {
		wid := worldID
		ev.WorldID = &wid
	}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return Event{}, fmt.Errorf("marshal %s payload: %w", eventType, err)
		}
		ev.Payload = raw
	}
	return ev, nil
}

// MustEvent is NewEvent for static payloads where marshalling cannot fail.
func MustEvent(eventType string, worldID uuid.UUID, payload any) Event {
	ev, err := NewEvent(eventType, worldID, payload)
	if err != nil {
		panic(err)
	}
	return ev
}

// Well-known event types. Feature sprints add their own; the socket forwards
// any type, so this list is documentation rather than a whitelist.
const (
	// EventWorldTick is published by the scheduler worker for every world tick
	// granularity. It is the S02-04 proof event that exercises the full
	// worker -> Redis -> API pod -> browser seam.
	EventWorldTick = "world_tick"
	// EventPong answers a client ping.
	EventPong = "pong"
	// EventError reports a client-side protocol problem (unknown type, malformed
	// message) without closing the connection.
	EventError = "error"
)
