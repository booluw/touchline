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
	ErrNoManager          = errors.New("account has no manager in any world — create a world to get started")
	ErrNotMemberOfWorld   = errors.New("account has no manager in that world")
	ErrInvalidRefresh     = errors.New("invalid or expired session")
)

// dummyHash is compared against (useless) credentials when no user matches an
// email, so unknown-user and wrong-password logins take the same bcrypt time
// and do not leak which accounts exist.
const dummyHash = "$2a$12$16pzz.IboYrC5hC08qKLIuOHpkudC0uTfyiIfigHGf2crA7vk9ZPG"

// WorldOption is one world the account can log in to.
type WorldOption struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

type managerRow struct {
	id          uuid.UUID
	worldID     uuid.UUID
	worldName   string
	worldStatus string
	status      string
	hasJob      bool
}

// LoginParams is the resolved login request.
type LoginParams struct {
	Email             string
	Password          string
	WorldID           *uuid.UUID // optional explicit choice when jobless + multi-world
	IP                *netip.Addr
	DeviceFingerprint string
}

// LoginResult is either a session (TokenPair + Identity) or, when the account
// is jobless and spans multiple worlds, the Worlds list for the client picker.
type LoginResult struct {
	TokenPair   *pkgauth.TokenPair
	Identity    *pkgauth.ManagerIdentity
	DisplayName string
	Worlds      []WorldOption
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

// Login verifies credentials and resolves the session's world (OPD-15):
//
//  1. a job ANYWHERE (status='active' AND current_club_id IS NOT NULL) wins —
//     a user holds at most one global job, so the session always lands there;
//  2. jobless + explicit WorldID -> that world, if the account has a row there;
//  3. jobless + one active world -> that world;
//  4. jobless + several active worlds -> Worlds list (client picks, re-posts
//     with WorldID);
//  5. jobless + no active world -> single row, else all rows, else ErrNoManager.
func (s *Service) Login(ctx context.Context, params LoginParams) (*LoginResult, error) {
	email := strings.ToLower(strings.TrimSpace(params.Email))
	if email == "" || params.Password == "" {
		return nil, ErrInvalidCredentials
	}

	var userID uuid.UUID
	var hash, name string
	err := s.pool.QueryRow(ctx,
		`SELECT id, password_hash, display_name FROM auth.users WHERE email = $1`, email,
	).Scan(&userID, &hash, &name)
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

	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.world_id, m.status, m.current_club_id, w.name, w.status
		FROM manager.managers m
		JOIN world.worlds w ON w.id = m.world_id
		WHERE m.user_id = $1
		ORDER BY m.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("load managers: %w", err)
	}
	managers, err := scanManagers(rows)
	if err != nil {
		return nil, err
	}

	target, options, err := resolveWorld(params.WorldID, managers)
	if err != nil {
		return nil, err
	}
	if options != nil {
		return &LoginResult{Worlds: options}, nil
	}

	identity := &pkgauth.ManagerIdentity{
		ManagerID: target.id,
		WorldID:   target.worldID,
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
// new pair for the SAME manager (world stays bound to the manager row — a
// refresh is a continuation, never a re-login).
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
	err = tx.QueryRow(ctx,
		`SELECT world_id, user_id FROM manager.managers WHERE id = $1`, managerID,
	).Scan(&worldID, &managerUserID)
	if err != nil {
		return nil, ErrInvalidRefresh
	}
	if managerUserID != sessionUID {
		return nil, ErrInvalidRefresh
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

// resolveWorld implements the OPD-15 ladder. Returns:
//   - (target, nil, nil)          when a session should be minted for target;
//   - (nil, options, nil)         when the client must pick a world first;
//   - (nil, nil, ErrNoManager)    when the account has no manager row;
//   - (nil, nil, ErrNotMemberOfWorld) when an explicit pick is not the account's.
func resolveWorld(pick *uuid.UUID, managers []managerRow) (*managerRow, []WorldOption, error) {
	for i := range managers {
		if managers[i].hasJob {
			return &managers[i], nil, nil
		}
	}

	if pick != nil {
		for i := range managers {
			if managers[i].worldID == *pick {
				return &managers[i], nil, nil
			}
		}
		return nil, nil, ErrNotMemberOfWorld
	}

	if len(managers) == 0 {
		return nil, nil, ErrNoManager
	}

	active := filter(managers, func(r managerRow) bool { return r.status == "active" })
	if len(active) == 1 {
		return &active[0], nil, nil
	}
	if len(active) > 1 {
		return nil, toOptions(active), nil
	}
	if len(managers) == 1 {
		return &managers[0], nil, nil
	}
	return nil, toOptions(managers), nil
}

func filter(rows []managerRow, keep func(managerRow) bool) []managerRow {
	var out []managerRow
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func toOptions(rows []managerRow) []WorldOption {
	out := make([]WorldOption, 0, len(rows))
	for _, r := range rows {
		out = append(out, WorldOption{ID: r.worldID, Name: r.worldName, Status: r.worldStatus})
	}
	return out
}

func scanManagers(rows pgx.Rows) ([]managerRow, error) {
	var out []managerRow
	for rows.Next() {
		var (
			r      managerRow
			clubID *uuid.UUID
		)
		if err := rows.Scan(&r.id, &r.worldID, &r.status, &clubID, &r.worldName, &r.worldStatus); err != nil {
			return nil, fmt.Errorf("scan manager row: %w", err)
		}
		r.hasJob = r.status == "active" && clubID != nil
		out = append(out, r)
	}
	return out, rows.Err()
}
