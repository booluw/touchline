package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func fixtureConfig() JWTConfig {
	return JWTConfig{
		Secret:     "test-secret",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 720 * time.Hour,
	}
}

func fixtureIdentity() ManagerIdentity {
	return ManagerIdentity{
		ManagerID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	}
}

func TestGeneratePairAndValidateAccess(t *testing.T) {
	cfg := fixtureConfig()
	identity := fixtureIdentity()

	pair, err := GenerateTokenPair(cfg, identity)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected non-empty tokens")
	}
	if pair.ExpiresAt.IsZero() {
		t.Fatal("expected access expiry set")
	}

	got, err := ValidateAccessToken(cfg, pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if got.ManagerID != identity.ManagerID {
		t.Errorf("ManagerID = %v, want %v", got.ManagerID, identity.ManagerID)
	}
	if got.UserID != identity.UserID {
		t.Errorf("UserID = %v, want %v", got.UserID, identity.UserID)
	}
	if got.WorldID != uuid.Nil {
		t.Errorf("WorldID = %v, want zero (world is not part of the token)", got.WorldID)
	}
}

func TestValidateRefreshReturnsManagerID(t *testing.T) {
	cfg := fixtureConfig()
	pair, err := GenerateTokenPair(cfg, fixtureIdentity())
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	managerID, err := ValidateRefreshToken(cfg, pair.RefreshToken)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if managerID != fixtureIdentity().ManagerID {
		t.Errorf("managerID = %v, want %v", managerID, fixtureIdentity().ManagerID)
	}
}

// Access tokens must never be accepted where a refresh token is expected and
// vice versa — the type claim is enforced by both validators.
func TestTokenTypeEnforcement(t *testing.T) {
	cfg := fixtureConfig()
	pair, err := GenerateTokenPair(cfg, fixtureIdentity())
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	if _, err := ValidateAccessToken(cfg, pair.RefreshToken); err == nil {
		t.Error("refresh token accepted as access token")
	}
	if _, err := ValidateRefreshToken(cfg, pair.AccessToken); err == nil {
		t.Error("access token accepted as refresh token")
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	pair, err := GenerateTokenPair(fixtureConfig(), fixtureIdentity())
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	if _, err := ValidateAccessToken(JWTConfig{Secret: "wrong"}, pair.AccessToken); err == nil {
		t.Error("expected error for wrong secret")
	}
}

// A token signed with a different algorithm (e.g. none) must be rejected even
// if it carries the right claims — the parser pins HS256.
func TestValidateAccessToken_RejectsAlgorithmConfusion(t *testing.T) {
	cfg := fixtureConfig()
	identity := fixtureIdentity()

	tampered := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub":     identity.ManagerID.String(),
		"user_id": identity.UserID.String(),
		"exp":     time.Now().Add(time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"type":    "access",
	})
	tokStr, err := tampered.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none-token: %v", err)
	}

	if _, err := ValidateAccessToken(cfg, tokStr); err == nil {
		t.Error("expected algorithm-confusion token to be rejected")
	}
}

// Rotation depends on every token being unique: two pairs minted within the
// same second for the same manager must still differ (the jti claim guarantees
// this), otherwise a refreshed token would collide with its predecessor on
// auth.sessions.refresh_token_hash.
func TestTokenPairUniqueness(t *testing.T) {
	cfg := fixtureConfig()
	identity := fixtureIdentity()

	a, err := GenerateTokenPair(cfg, identity)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}
	b, err := GenerateTokenPair(cfg, identity)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	if a.RefreshToken == b.RefreshToken {
		t.Error("two refresh tokens minted back-to-back must not be identical")
	}
	if a.AccessToken == b.AccessToken {
		t.Error("two access tokens minted back-to-back must not be identical")
	}
}

func TestHashRefreshToken(t *testing.T) {
	h1 := HashRefreshToken("token-a")
	h2 := HashRefreshToken("token-a")
	h3 := HashRefreshToken("token-b")

	if h1 == "" || h1 != h2 {
		t.Errorf("hash not deterministic: %q vs %q", h1, h2)
	}
	if h1 == h3 {
		t.Error("different tokens should hash differently")
	}
	if len(h1) != 64 {
		t.Errorf("expected sha256 hex (64 chars), got %d", len(h1))
	}
}
