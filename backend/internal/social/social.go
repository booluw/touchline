package social

import (
	"time"

	"github.com/google/uuid"
	"github.com/touchline/backend/pkg/apiref"
)

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
