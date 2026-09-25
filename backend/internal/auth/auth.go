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

	"github.com/touchline/backend/pkg/apiref"
	pkgauth "github.com/touchline/backend/pkg/auth"
)

// Sentinel errors. Handlers map these to HTTP status codes; all other errors
// surface as 500.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotAuthorized      = errors.New("login is currently limited to administrators")
	ErrInvalidRefresh     = errors.New("invalid or expired session")
	ErrNoManager          = errors.New("no world joined — an administrator must give this account a world before it can log in")
	ErrNotMember          = errors.New("this account is not a member of the requested world")
	ErrEmailTaken         = errors.New("an account with this email already exists")
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
	WorldID           *uuid.UUID // OPD-15(4)(b): explicit world pick on re-post
}

// WorldInfo is one world a non-admin account can log into, shown by the
// world-picker response when the account is a member of several worlds.
type WorldInfo struct {
	ID     uuid.UUID `json:"world_id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

// LoginResult is either a session (TokenPair + Identity), or — for a non-admin
// with memberships in several worlds — the Worlds list the client must choose
// from (Worlds non-nil, TokenPair/Identity nil). An administrator always gets a
// world-less console session.
type LoginResult struct {
	TokenPair   *pkgauth.TokenPair
	Identity    *pkgauth.ManagerIdentity
	Worlds      []WorldInfo
	DisplayName string
	IsAdmin     bool
	CreatedAt   time.Time
	ID          uuid.UUID
	Club        *apiref.ClubRef // the manager's current club, or nil when unemployed/admin
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

// Login verifies credentials and establishes a session.
//
// An administrator always receives a world-less console session: admins run the
// global configuration console rather than a manager's world (recorded decision
// in A12). Any other account is resolved per OPD-15(4): the world where the
// account has a job wins; a jobless account may pick a world explicitly via
// LoginParams.WorldID, or is auto-resolved when it has exactly one joined world;
// with several joined worlds the caller gets the Worlds list (no session) and
// must re-post with a chosen world; with no joined worlds it gets ErrNoManager.
func (s *Service) Login(ctx context.Context, params LoginParams) (*LoginResult, error) {
	email := strings.ToLower(strings.TrimSpace(params.Email))
	if email == "" || params.Password == "" {
		return nil, ErrInvalidCredentials
	}

	var userID uuid.UUID
	var hash, name string
	var isAdmin bool
	var createdAt time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT id, password_hash, display_name, created_at, is_admin FROM auth.users WHERE email = $1`, email,
	).Scan(&userID, &hash, &name, &createdAt, &isAdmin)
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

	if isAdmin {
		return s.adminSession(ctx, userID, name, createdAt, params.IP, params.DeviceFingerprint)
	}
	return s.managerLogin(ctx, userID, name, createdAt, params)
}

// managerLogin resolves a non-admin account to a manager/world session per
// OPD-15(4), or returns the world picker when the account is a member of
// several worlds.
func (s *Service) managerLogin(ctx context.Context, userID uuid.UUID, name string, createdAt time.Time, params LoginParams) (*LoginResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.world_id, m.id, w.name, w.status, m.status, m.current_club_id
		FROM manager.managers m
		JOIN world.worlds w ON w.id = m.world_id
		WHERE m.user_id = $1 AND w.status <> 'archived'
		ORDER BY m.world_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("load memberships: %w", err)
	}
	type membership struct {
		managerID uuid.UUID
		world     WorldInfo
	}
	var memberships []membership
	var employed uuid.UUID
	for rows.Next() {
		var m membership
		var status string
		var currentClub *uuid.UUID
		if err := rows.Scan(&m.world.ID, &m.managerID, &m.world.Name, &m.world.Status, &status, &currentClub); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		// The unique one-job-per-user index guarantees at most one such row.
		if status == "active" && currentClub != nil {
			employed = m.managerID
		}
		memberships = append(memberships, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memberships: %w", err)
	}

	// OPD-15(4)(a): the world where the account has a job always wins.
	if employed != uuid.Nil {
		return s.mintManagerSession(ctx, userID, employed, name, createdAt, params)
	}

	switch {
	case params.WorldID != nil:
		// OPD-15(4)(b): explicit pick — must be one of the account's worlds.
		for _, m := range memberships {
			if m.world.ID == *params.WorldID {
				return s.mintManagerSession(ctx, userID, m.managerID, name, createdAt, params)
			}
		}
		return nil, ErrNotMember

	case len(memberships) == 1:
		// OPD-15(4)(c): exactly one joined world.
		return s.mintManagerSession(ctx, userID, memberships[0].managerID, name, createdAt, params)

	case len(memberships) > 1:
		// OPD-15(4)(d): world picker — no session until the client re-posts.
		worlds := make([]WorldInfo, 0, len(memberships))
		for _, m := range memberships {
			worlds = append(worlds, m.world)
		}
		return &LoginResult{Worlds: worlds, DisplayName: name, CreatedAt: createdAt, ID: userID}, nil

	default:
		// OPD-15(4)(g): account exists, no world joined.
		return nil, ErrNoManager
	}
}

// adminSession mints the world-less console session for an administrator.
func (s *Service) adminSession(ctx context.Context, userID uuid.UUID, name string, createdAt time.Time, ip *netip.Addr, deviceFingerprint string) (*LoginResult, error) {
	identity := &pkgauth.ManagerIdentity{
		ManagerID: uuid.Nil,
		WorldID:   uuid.Nil,
		UserID:    userID,
	}
	return s.completeLogin(ctx, userID, identity, name, createdAt, true, ip, deviceFingerprint)
}

// mintManagerSession mints a manager-bound session for the given manager row.
func (s *Service) mintManagerSession(ctx context.Context, userID uuid.UUID, managerID uuid.UUID, name string, createdAt time.Time, params LoginParams) (*LoginResult, error) {
	var worldID uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT world_id FROM manager.managers WHERE id = $1`, managerID).Scan(&worldID); err != nil {
		return nil, fmt.Errorf("load manager world: %w", err)
	}
	identity := &pkgauth.ManagerIdentity{
		ManagerID: managerID,
		WorldID:   worldID,
		UserID:    userID,
	}
	res, err := s.completeLogin(ctx, userID, identity, name, createdAt, false, params.IP, params.DeviceFingerprint)
	if err != nil {
		return nil, err
	}
	res.Club, err = s.managerClub(ctx, managerID)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// managerClub resolves the club an employed manager currently manages, or nil
// when they hold no club (unemployed, sacked, or resigned). The FK on
// current_club_id guarantees the club row exists when the id is set.
func (s *Service) managerClub(ctx context.Context, managerID uuid.UUID) (*apiref.ClubRef, error) {
	var (
		clubID    *uuid.UUID
		name      string
		shortName string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT m.current_club_id, c.name, c.short_name
		FROM manager.managers m
		LEFT JOIN club.clubs c ON c.id = m.current_club_id
		WHERE m.id = $1`, managerID).Scan(&clubID, &name, &shortName)
	if err != nil {
		return nil, fmt.Errorf("load manager club: %w", err)
	}
	if clubID == nil {
		return nil, nil
	}
	return &apiref.ClubRef{ID: *clubID, Name: name, Short: shortName}, nil
}

// completeLogin mints the token pair, records the hashed refresh session in one
// transaction, and returns the assembled result.
func (s *Service) completeLogin(ctx context.Context, userID uuid.UUID, identity *pkgauth.ManagerIdentity, name string, createdAt time.Time, isAdmin bool, ip *netip.Addr, deviceFingerprint string) (*LoginResult, error) {
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
	// One live session per account (IM13): logging in invalidates and deletes
	// any prior session. Hard delete happens BEFORE the insert, in the same
	// transaction, so the partial unique index uq_sessions_one_live_per_user is
	// never violated and no two live rows can ever coexist.
	if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id = $1`, userID); err != nil {
		return nil, fmt.Errorf("clear prior session: %w", err)
	}
	if err := insertSession(ctx, tx, userID, pair.RefreshToken, s.cfg.RefreshTTL, ip, deviceFingerprint); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit session: %w", err)
	}

	return &LoginResult{
		TokenPair:   pair,
		Identity:    identity,
		DisplayName: name,
		IsAdmin:     isAdmin,
		CreatedAt:   createdAt,
		ID:          userID,
	}, nil
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
			return nil, ErrNotAuthorized
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

	var club *apiref.ClubRef
	if managerID != uuid.Nil {
		if club, err = s.managerClub(ctx, managerID); err != nil {
			return nil, err
		}
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

	return &LoginResult{TokenPair: pair, Identity: identity, Club: club}, nil
}

// Logout deletes the session matching the presented refresh token (IM13). The
// plaintext token is hashed the same way insertSession stores it. Logout is
// idempotent by design: an absent, already-consumed, or already-logged-out
// token simply matches no row and returns nil — there is never anything for
// the caller to distinguish.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	if rawRefresh == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM auth.sessions WHERE refresh_token_hash = $1`,
		pkgauth.HashRefreshToken(rawRefresh)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// insertSession records a rotating refresh session. The plaintext token is
// never stored — only its SHA-256 hash.
