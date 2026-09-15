package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// RegisterParams is a self-service registration request (A13).
type RegisterParams struct {
	Email       string
	Password    string
	DisplayName string
}

// RegisterResult is the outcome of creating an account and, when there is
// exactly one playable world, joining it as an unemployed manager.
type RegisterResult struct {
	UserID      uuid.UUID
	DisplayName string
	ManagerID   *uuid.UUID
	JoinedWorld *WorldInfo // nil when no world was joined
}

// Register creates a plain (non-admin) account via the product signup flow
// (A13). Per the recorded onboarding decision an account is only auto-joined
// when there is EXACTLY one playable world ('active'/'open_beta'): with zero
// or several playable worlds the new account is created world-less and an
// administrator must join it later. A duplicate email is ErrEmailTaken. All
// writes happen in one transaction; no session is minted by registration.
//
// No email verification/recovery is implemented (OPD-02, open decision).
func (s *Service) Register(ctx context.Context, params RegisterParams) (*RegisterResult, error) {
	email := strings.ToLower(strings.TrimSpace(params.Email))
	if email == "" || params.Password == "" {
		return nil, fmt.Errorf("register: %w", ErrInvalidCredentials)
	}
	name := strings.TrimSpace(params.DisplayName)
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(params.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("register hash: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin register tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO auth.users (email, password_hash, display_name, is_admin)
		VALUES ($1, $2, $3, FALSE)
		RETURNING id`, email, string(hash), name).Scan(&userID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}

	// Join when exactly one playable world exists.
	rows, err := tx.Query(ctx, `
		SELECT id, name, status FROM world.worlds
		WHERE status IN ('active', 'open_beta')
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list playable worlds: %w", err)
	}
	var playable []WorldInfo
	for rows.Next() {
		var w WorldInfo
		if err := rows.Scan(&w.ID, &w.Name, &w.Status); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan playable world: %w", err)
		}
		playable = append(playable, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playable worlds: %w", err)
	}

	var managerID *uuid.UUID
	var joined *WorldInfo
	if len(playable) == 1 {
		var mid uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO manager.managers (world_id, user_id, status)
			VALUES ($1, $2, 'unemployed')
			RETURNING id`, playable[0].ID, userID).Scan(&mid); err != nil {
			return nil, fmt.Errorf("insert manager: %w", err)
		}
		managerID = &mid
		joined = &playable[0]
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit register: %w", err)
	}

	res := &RegisterResult{UserID: userID, DisplayName: name}
	if managerID != nil {
		res.ManagerID = managerID
		res.JoinedWorld = joined
	}
	return res, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
