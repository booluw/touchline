package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGenerateAndValidateToken(t *testing.T) {
	cfg := JWTConfig{
		Secret:     "test-secret-key",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 720 * time.Hour,
	}

	identity := ManagerIdentity{
		ManagerID: uuid.New(),
		WorldID:   uuid.New(),
		UserID:    uuid.New(),
	}

	pair, err := GenerateTokenPair(cfg, identity)
	if err != nil {
		t.Fatalf("GenerateTokenPair failed: %v", err)
	}

	if pair.AccessToken == "" {
		t.Error("access token is empty")
	}
	if pair.RefreshToken == "" {
		t.Error("refresh token is empty")
	}

	// Validate access token
	validated, err := ValidateToken(cfg, pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if validated.ManagerID != identity.ManagerID {
		t.Errorf("manager ID mismatch: got %v, want %v", validated.ManagerID, identity.ManagerID)
	}
	if validated.WorldID != identity.WorldID {
		t.Errorf("world ID mismatch: got %v, want %v", validated.WorldID, identity.WorldID)
	}
}

func TestValidateToken_InvalidSecret(t *testing.T) {
	cfg := JWTConfig{
		Secret:     "correct-secret",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 720 * time.Hour,
	}

	identity := ManagerIdentity{
		ManagerID: uuid.New(),
		WorldID:   uuid.New(),
		UserID:    uuid.New(),
	}

	pair, _ := GenerateTokenPair(cfg, identity)

	wrongCfg := JWTConfig{Secret: "wrong-secret"}
	_, err := ValidateToken(wrongCfg, pair.AccessToken)
	if err == nil {
		t.Error("expected error with wrong secret, got nil")
	}
}
