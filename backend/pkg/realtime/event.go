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
	// EventMatchTick is published by the matchday runner (S04-03) once per
	// paced simulated minute and again at full time. Its payload carries the
	// minute's persisted match_events rows plus the server-computed scoreline;
	// the client renders it verbatim and never derives outcomes.
	EventMatchTick = "match_tick"
	// EventSocialMessage is pushed to a world's socket feed when a direct
	// manager→manager message lands (S06-04b). Its payload carries the
	// persisted message plus the sender's display name; the client routes it to
	// the recipient's inbox. Postgres stays authoritative — the push is
	// best-effort and the inbox read remains the source of truth.
	EventSocialMessage = "social_message"
	// EventRelationshipChange is pushed to a world's socket feed when a
	// completed fixture creates or updates a rivalry edge (S06-04c). Its payload
	// carries the fixture pair plus the changed edges; the graph read (profile /
	// /api/relationships) stays authoritative — the push is best-effort.
	EventRelationshipChange = "relationship_change"
	// EventDashboardUpdate is pushed to a manager's socket feed when the home
	// dashboard aggregator surfaces new items (S07-01): init / bid events and
	// the world-tick sweep. Its payload is a DashboardUpdatePayload carrying
	// the section and the not-yet-pushed items; the GET /api/dashboard read
	// stays authoritative — the push is best-effort and the client dedupes by
	// stable item ID.
	EventDashboardUpdate = "dashboard_update"
	// EventPong answers a client ping.
	EventPong = "pong"
	// EventError reports a client-side protocol problem (unknown type, malformed
	// message) without closing the connection.
	EventError = "error"
)
