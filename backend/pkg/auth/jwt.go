package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// GenerateTokenPair creates access + refresh tokens for a manager.
//
// The tokens deliberately do NOT carry a world_id claim: a manager's world is a
// property of their manager row (manager.managers), not of the session. The
// access token identifies the manager (sub) and their account (user_id); the
// refresh token carries only sub = manager_id, so a refresh re-derives the full
// identity from the database.
func GenerateTokenPair(cfg JWTConfig, identity ManagerIdentity) (*TokenPair, error) {
	now := time.Now()
	accessExpiry := now.Add(cfg.AccessTTL)

	accessJTI := uuid.New().String()
	accessClaims := jwt.MapClaims{
		"sub":     identity.ManagerID.String(),
		"user_id": identity.UserID.String(),
		"jti":     accessJTI,
		"exp":     accessExpiry.Unix(),
		"iat":     now.Unix(),
		"type":    "access",
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	refreshExpiry := now.Add(cfg.RefreshTTL)
	refreshClaims := jwt.MapClaims{
		"sub":  identity.ManagerID.String(),
		"jti":  uuid.New().String(),
		"exp":  refreshExpiry.Unix(),
		"iat":  now.Unix(),
		"type": "refresh",
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresAt:    accessExpiry,
	}, nil
}

// ValidateAccessToken validates an access token (type=access, HS256 pinned) and
// returns the manager identity. WorldID is zero — callers resolve the world from
// manager.managers by ManagerID when an engine endpoint needs it.
func ValidateAccessToken(cfg JWTConfig, tokenStr string) (*ManagerIdentity, error) {
	claims, err := parseClaims(cfg, tokenStr)
	if err != nil {
		return nil, err
	}
	if tokenType := claims["type"]; tokenType != "access" {
		return nil, fmt.Errorf("expected access token, got %v", tokenType)
	}

	managerIDStr, ok := claims["sub"].(string)
	if !ok {
		return nil, fmt.Errorf("missing sub claim")
	}
	managerID, err := uuid.Parse(managerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid manager_id in token")
	}

	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing user_id claim")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user_id in token")
	}

	return &ManagerIdentity{
		ManagerID: managerID,
		UserID:    userID,
	}, nil
}

// ValidateRefreshToken validates a refresh token (type=refresh, HS256 pinned)
// and returns the manager_id (sub).
func ValidateRefreshToken(cfg JWTConfig, tokenStr string) (uuid.UUID, error) {
	claims, err := parseClaims(cfg, tokenStr)
	if err != nil {
		return uuid.Nil, err
	}
	if tokenType := claims["type"]; tokenType != "refresh" {
		return uuid.Nil, fmt.Errorf("expected refresh token, got %v", tokenType)
	}

	managerIDStr, ok := claims["sub"].(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("missing sub claim")
	}
	managerID, err := uuid.Parse(managerIDStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid manager_id in token")
	}
	return managerID, nil
}

// HashRefreshToken hashes a refresh token for storage in auth.sessions
// (refresh_token_hash): at-rest storage never holds the raw token.
func HashRefreshToken(tokenStr string) string {
	sum := sha256.Sum256([]byte(tokenStr))
	return hex.EncodeToString(sum[:])
}

// parseClaims parses and validates signature + expiry with the signing
// algorithm pinned to HS256 (no algorithm-confusion attacks).
func parseClaims(cfg JWTConfig, tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
