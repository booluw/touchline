package auth

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	pkgauth "github.com/touchline/backend/pkg/auth"
)

// insertSession records a hashed refresh session inside the caller's tx.
// The raw refresh token is never stored — only its SHA-256 hash. The IP is
// stored as inet text (nil when unavailable).
func insertSession(ctx context.Context, tx pgx.Tx, userID uuid.UUID, refreshToken string, ttl time.Duration, ip *netip.Addr, deviceFingerprint string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO auth.sessions (user_id, refresh_token_hash, device_fingerprint, ip_address, expires_at)
		VALUES ($1, $2, $3, $4::inet, now() + $5::interval)`,
		userID, pkgauth.HashRefreshToken(refreshToken), nullIfEmpty(deviceFingerprint), addrToString(ip), ttl,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func addrToString(ip *netip.Addr) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}
