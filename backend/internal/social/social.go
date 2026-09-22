package social

import (
	"time"

	"github.com/google/uuid"
	"github.com/touchline/backend/pkg/apiref"
)

// Relationship is one polymorphic graph edge on social.relationships
// (entity_a ↔ entity_b with relationship_type and signed strength/trust/
// sentiment). Entity types are player, manager, club; relationship types
// include rivalry, friendship, mentorship, professional_respect, dislike,
// family, national_team, academy, former_teammate, former_manager, agent.
type Relationship struct {
	ID                uuid.UUID `json:"id"`
	WorldID           uuid.UUID `json:"world_id"`
	EntityAID         uuid.UUID `json:"entity_a_id"`
	EntityAType       string    `json:"entity_a_type"`
	EntityBID         uuid.UUID `json:"entity_b_id"`
	EntityBType       string    `json:"entity_b_type"`
	RelationshipType  string    `json:"relationship_type"`
	Strength          int       `json:"strength"`
	Trust             int       `json:"trust"`
	Sentiment         int       `json:"sentiment"`
	LastInteractionAt time.Time `json:"last_interaction_at"`
	CreatedAt         time.Time `json:"created_at"`
}

// Message is one direct message on social.messages (S06-04b). Flat sender_id/
// recipient_id stay internal; the wire carries nested identity refs.
type Message struct {
	ID            uuid.UUID         `json:"id"`
	WorldID       uuid.UUID         `json:"world_id"`
	SenderID      uuid.UUID         `json:"-"`
	SenderType    string            `json:"-"` // manager, system
	Sender        *apiref.EntityRef `json:"sender"`
	RecipientID   uuid.UUID         `json:"-"`
	RecipientType string            `json:"-"` // manager, system
	Recipient     *apiref.EntityRef `json:"recipient"`
	Subject       *string           `json:"subject"`
	Body          string            `json:"body"`
	SentAt        time.Time         `json:"sent_at"`
	ReadAt        *time.Time        `json:"read_at"`
}

// Promise is one structured social.promises row. S06-03 writes playing-time
// promises; later sprints extend the promise library.
type Promise struct {
	ID           uuid.UUID  `json:"id"`
	WorldID      uuid.UUID  `json:"world_id"`
	PlayerID     uuid.UUID  `json:"player_id"`
	ManagerID    uuid.UUID  `json:"manager_id"`
	PromiseType  string     `json:"promise_type"`
	Explicitness string     `json:"explicitness"`
	Deadline     *time.Time `json:"deadline"`
	Confidence   int        `json:"confidence"`
	Importance   int        `json:"importance"`
	Status       string     `json:"status"` // pending, fulfilled, broken, adapted
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
}
