package club

import "github.com/google/uuid"

type Club struct {
	ID        uuid.UUID `json:"id"`
	WorldID   uuid.UUID `json:"world_id"`
	Name      string    `json:"name"`
	ShortName string    `json:"short_name"`
}
