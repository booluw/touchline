package social

import "github.com/google/uuid"

type Relationship struct {
	ID                uuid.UUID `json:"id"`
	EntityAID         uuid.UUID `json:"entity_a_id"`
	EntityAType       string    `json:"entity_a_type"` // player, manager, club
	EntityBID         uuid.UUID `json:"entity_b_id"`
	EntityBType       string    `json:"entity_b_type"`
	RelationshipType  string    `json:"relationship_type"` // friendship, rivalry, mentorship, professional_respect, dislike, family, national_team, academy, former_teammate, former_manager, agent
	Strength          int       `json:"strength"`
	Trust             int       `json:"trust"`
	Sentiment         int       `json:"sentiment"`
	LastInteractionAt string    `json:"last_interaction_at"`
}

type Message struct {
	ID         uuid.UUID `json:"id"`
	WorldID    uuid.UUID `json:"world_id"`
	SenderID   uuid.UUID `json:"sender_id"`
	ReceiverID uuid.UUID `json:"receiver_id"`
	Content    string    `json:"content"`
	Read       bool      `json:"read"`
	CreatedAt  string    `json:"created_at"`
}

type Promise struct {
	ID            uuid.UUID `json:"id"`
	ManagerID     uuid.UUID `json:"manager_id"`
	PlayerID      uuid.UUID `json:"player_id"`
	Description   string    `json:"description"`
	Deadline      string    `json:"deadline"`
	Confidence    int       `json:"confidence"`
	Importance    int       `json:"importance"`
	Status        string    `json:"status"` // pending, fulfilled, broken
	Consequence   string    `json:"consequence"`
}

type ManagerTrustScore struct {
	ManagerID  uuid.UUID `json:"manager_id"`
	WorldID    uuid.UUID `json:"world_id"`
	Score      int       `json:"score"` // derived from event log: broken promises, honored trades, disputes
}

type Service interface {
	GetRelationships(entityID uuid.UUID) ([]*Relationship, error)
	SendMessage(msg *Message) error
	GetMessages(userID uuid.UUID) ([]*Message, error)
}
