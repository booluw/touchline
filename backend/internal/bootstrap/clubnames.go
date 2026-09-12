package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Sentinel errors for the club-name-parts admin surface.
var (
	ErrInvalidClubNamePart  = errors.New("club name part kind must be 'stem' or 'suffix'")
	ErrClubNamePartRequired = errors.New("club name part value is required")
)

// LoadClubNameParts reads the global club-name pools (ref.club_name_parts) in
// a stable order so league seeding draws names deterministically. It runs
// against a transaction so S04-01 seeding stays atomic. An empty pool is
// ErrRefDataMissing — seeding must never silently skip over a missing pool.
func LoadClubNameParts(ctx context.Context, tx pgx.Tx) (stems, suffixes []string, err error) {
	stems = []string{}
	suffixes = []string{}
	rows, err := tx.Query(ctx, `
		SELECT kind, value FROM ref.club_name_parts
		ORDER BY kind, value`)
	if err != nil {
		return nil, nil, fmt.Errorf("query club name parts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, nil, fmt.Errorf("scan club name part: %w", err)
		}
		if kind == "stem" {
			stems = append(stems, value)
		} else {
			suffixes = append(suffixes, value)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate club name parts: %w", err)
	}
	if len(stems) == 0 || len(suffixes) == 0 {
		return nil, nil, fmt.Errorf("%w: club name pools are empty (run cmd/ref-seed)", ErrRefDataMissing)
	}
	return stems, suffixes, nil
}

// ListClubNameParts returns the global club-name pools for the admin surface.
func (s *Service) ListClubNameParts(ctx context.Context) (stems, suffixes []string, err error) {
	stems = []string{}
	suffixes = []string{}
	rows, err := s.pool.Query(ctx, `
		SELECT kind, value FROM ref.club_name_parts
		ORDER BY kind, value`)
	if err != nil {
		return nil, nil, fmt.Errorf("query club name parts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err := rows.Scan(&kind, &value); err != nil {
			return nil, nil, fmt.Errorf("scan club name part: %w", err)
		}
		if kind == "stem" {
			stems = append(stems, value)
		} else {
			suffixes = append(suffixes, value)
		}
	}
	return stems, suffixes, rows.Err()
}

// AddClubNamePart upserts one stem or suffix into the global pool.
func (s *Service) AddClubNamePart(ctx context.Context, kind, value string) error {
	kind, value, err := normalizeClubNamePart(kind, value)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO ref.club_name_parts (kind, value, frequency_weight)
		VALUES ($1, $2, 1.0)
		ON CONFLICT (kind, value) DO NOTHING`, kind, value); err != nil {
		return fmt.Errorf("add club name part: %w", err)
	}
	return nil
}

// RemoveClubNamePart deletes one stem or suffix from the global pool. Removing
// an entry that isn't present is a no-op success.
func (s *Service) RemoveClubNamePart(ctx context.Context, kind, value string) error {
	kind, value, err := normalizeClubNamePart(kind, value)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM ref.club_name_parts WHERE kind = $1 AND value = $2`, kind, value); err != nil {
		return fmt.Errorf("remove club name part: %w", err)
	}
	return nil
}

func normalizeClubNamePart(kind, value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if kind != "stem" && kind != "suffix" {
		return "", "", ErrInvalidClubNamePart
	}
	if value == "" {
		return "", "", ErrClubNamePartRequired
	}
	return kind, value, nil
}
