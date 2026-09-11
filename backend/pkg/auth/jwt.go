package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// GenerateTokenPair creates access + refresh tokens for a manager.
func GenerateTokenPair(cfg JWTConfig, identity ManagerIdentity) (*TokenPair, error) {
	now := time.Now()
	accessExpiry := now.Add(cfg.AccessTTL)

	accessClaims := jwt.MapClaims{
		"sub": identity.ManagerID.String(),
		"world_id": identity.WorldID.String(),
		"user_id": identity.UserID.String(),
		"exp": accessExpiry.Unix(),
		"iat": now.Unix(),
		"type": "access",
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	refreshExpiry := now.Add(cfg.RefreshTTL)
	refreshClaims := jwt.MapClaims{
		"sub": identity.ManagerID.String(),
		"exp": refreshExpiry.Unix(),
		"iat": now.Unix(),
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

// ValidateToken validates a JWT and returns the manager identity.
func ValidateToken(cfg JWTConfig, tokenStr string) (*ManagerIdentity, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.Secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	managerID, err := uuid.Parse(claims["sub"].(string))
	if err != nil {
		return nil, fmt.Errorf("invalid manager_id in token")
	}

	worldID, err := uuid.Parse(claims["world_id"].(string))
	if err != nil {
		return nil, fmt.Errorf("invalid world_id in token")
	}

	userID, err := uuid.Parse(claims["user_id"].(string))
	if err != nil {
		return nil, fmt.Errorf("invalid user_id in token")
	}

	return &ManagerIdentity{
		ManagerID: managerID,
		WorldID:   worldID,
		UserID:    userID,
	}, nil
}
