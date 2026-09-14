// Package auth implements the account->manager session flow for the API:
// login world resolution (OPD-15), bcrypt credential verification, and
// rotating refresh sessions stored hashed in auth.sessions.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	pkgauth "github.com/touchline/backend/pkg/auth"
)

// Sentinel errors. Handlers map these to HTTP status codes; all other errors
// surface as 500.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotAuthorized      = errors.New("login is currently limited to administrators")
	ErrInvalidRefresh     = errors.New("invalid or expired session")
)

// dummyHash is compared against (useless) credentials when no user matches an
// email, so unknown-user and wrong-password logins take the same bcrypt time
// and do not leak which accounts exist.
const dummyHash = "$2a$12$16pzz.IboYrC5hC08qKLIuOHpkudC0uTfyiIfigHGf2crA7vk9ZPG"

// LoginParams is the resolved login request.
type LoginParams struct {
	Email             string
	Password          string
	IP                *netip.Addr
	DeviceFingerprint string
}

// LoginResult is either a session (TokenPair + Identity) for an administrator,
// or an error for anyone else.
type LoginResult struct {
	TokenPair   *pkgauth.TokenPair
	Identity    *pkgauth.ManagerIdentity
	DisplayName string
}

// Service resolves credentials and manages rotating sessions.
type Service struct {
	pool *pgxpool.Pool
	cfg  pkgauth.JWTConfig
}

// NewService builds the auth service.
func NewService(pool *pgxpool.Pool, cfg pkgauth.JWTConfig) *Service {
	return &Service{pool: pool, cfg: cfg}
}

// Login verifies credentials and establishes a console session (phase-1 launch
// model): only is_admin accounts may log in, and an admin session is world-less
// (ManagerID and WorldID are uuid.Nil) because admins operate the global
// configuration console rather than a manager's world. The user chain (auth.
// users → manager.managers → a world job) that a login used to walk is
// deferred to open-access; for now a non-admin account is rejected outright
// with ErrNotAuthorized.
func (s *Service) Login(ctx context.Context, params LoginParams) (*LoginResult, error) {
	email := strings.ToLower(strings.TrimSpace(params.Email))
	if email == "" || params.Password == "" {
		return nil, ErrInvalidCredentials
	}

	var userID uuid.UUID
	var hash, name string
	var isAdmin bool
	err := s.pool.QueryRow(ctx,
		`SELECT id, password_hash, display_name, is_admin FROM auth.users WHERE email = $1`, email,
	).Scan(&userID, &hash, &name, &isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(params.Password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(params.Password)) != nil {
		return nil, ErrInvalidCredentials
	}
	if !isAdmin {
		return nil, ErrNotAuthorized
	}

	// Console session: the admin has no manager row (the DB is empty at launch;
	// the first admin is created by cmd/user-create without -world-id), so the
	// token identifies the ACCOUNT only. requireAuth tolerates a nil manager and
	// admin routes authorize off UserID.
	identity := &pkgauth.ManagerIdentity{
		ManagerID: uuid.Nil,
		WorldID:   uuid.Nil,
		UserID:    userID,
	}

	pair, err := pkgauth.GenerateTokenPair(s.cfg, *identity)
	if err != nil {
		return nil, fmt.Errorf("generate tokens: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin session tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `UPDATE auth.users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
		return nil, fmt.Errorf("touch last_login: %w", err)
	}
	if err := insertSession(ctx, tx, userID, pair.RefreshToken, s.cfg.RefreshTTL, params.IP, params.DeviceFingerprint); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit session: %w", err)
	}

	return &LoginResult{TokenPair: pair, Identity: identity, DisplayName: name}, nil
}

// Refresh validates a refresh token, revokes the matched session, and mints a
// new pair for the SAME subject (a refresh is a continuation, never a
// re-login). The nil-manager (admin console) session shape is re-verified
// against auth.users.is_admin here — a revoked admin grant kills the session
// on its next refresh, mirroring requireAdmin's per-request check.
func (s *Service) Refresh(ctx context.Context, rawRefresh string, ip *netip.Addr, deviceFingerprint string) (*LoginResult, error) {
	managerID, err := pkgauth.ValidateRefreshToken(s.cfg, rawRefresh)
	if err != nil {
		return nil, ErrInvalidRefresh
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refresh tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		sessionID  uuid.UUID
		sessionUID uuid.UUID
		expiresAt  time.Time
		revokedAt  *time.Time
	)
	hash := pkgauth.HashRefreshToken(rawRefresh)
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, expires_at, revoked_at FROM auth.sessions
		WHERE refresh_token_hash = $1 FOR UPDATE`, hash,
	).Scan(&sessionID, &sessionUID, &expiresAt, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidRefresh
	}
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if revokedAt != nil || !expiresAt.After(time.Now()) {
		return nil, ErrInvalidRefresh
	}

	var (
		worldID       uuid.UUID
		managerUserID uuid.UUID
	)
	if managerID == uuid.Nil {
		// Admin console session: the refresh token's sub is the nil manager.
		// Re-check admin status; the session must belong to the same account.
		var isAdmin bool
		if err := tx.QueryRow(ctx,
			`SELECT is_admin FROM auth.users WHERE id = $1`, sessionUID).Scan(&isAdmin); err != nil || !isAdmin {
			return nil, ErrInvalidRefresh
		}
	} else {
		err = tx.QueryRow(ctx,
			`SELECT world_id, user_id FROM manager.managers WHERE id = $1`, managerID,
		).Scan(&worldID, &managerUserID)
		if err != nil {
			return nil, ErrInvalidRefresh
		}
		if managerUserID != sessionUID {
			return nil, ErrInvalidRefresh
		}
	}

	identity := &pkgauth.ManagerIdentity{ManagerID: managerID, WorldID: worldID, UserID: sessionUID}
	pair, err := pkgauth.GenerateTokenPair(s.cfg, *identity)
	if err != nil {
		return nil, fmt.Errorf("generate tokens: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE auth.sessions SET revoked_at = now() WHERE id = $1`, sessionID); err != nil {
		return nil, fmt.Errorf("revoke session: %w", err)
	}
	if err := insertSession(ctx, tx, sessionUID, pair.RefreshToken, s.cfg.RefreshTTL, ip, deviceFingerprint); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit refresh: %w", err)
	}

	return &LoginResult{TokenPair: pair, Identity: identity, DisplayName: ""}, nil
}

// insertSession records a rotating refresh session. The plaintext token is
// never stored — only its SHA-256 hash.
