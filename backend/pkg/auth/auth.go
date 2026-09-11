package auth

import (
	"time"

	"github.com/google/uuid"
)

// JWTConfig holds configuration for JWT authentication.
type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// TokenPair represents an access + refresh token pair.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ManagerIdentity represents the authenticated manager.
type ManagerIdentity struct {
	ManagerID uuid.UUID `json:"manager_id"`
	WorldID   uuid.UUID `json:"world_id"`
	UserID    uuid.UUID `json:"user_id"`
}
