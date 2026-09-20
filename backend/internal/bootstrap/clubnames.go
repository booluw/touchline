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
	ErrInvalidClubNamePart    = errors.New("club name part kind must be 'stem' or 'suffix'")
	ErrClubNamePartRequired   = errors.New("club name part value is required")
	ErrInvalidClubNameCountry = errors.New("club name part country code must be empty (global) or 2-4 lowercase letters/digits")
)

// ClubPool is one club-name fragment pool. Code "" holds the global fallback;
// any other code is a country-scoped regional pool.
type ClubPool struct {
	Code     string
	Stems    []string
	Suffixes []string
}

// LoadClubNamePools reads the club-name pools (ref.club_name_parts) in a stable
// order so league seeding draws names deterministically. It runs against a
// transaction so S04-01 seeding stays atomic. The key "" (global fallback)
// must be non-empty — ErrRefDataMissing otherwise — while a regional pool may
// legitimately be empty (the caller falls back to the global pool).
func LoadClubNamePools(ctx context.Context, tx pgx.Tx) (map[string]ClubPool, error) {
	rows, err := tx.Query(ctx, `
		SELECT country_code, kind, value FROM ref.club_name_parts
		ORDER BY country_code, kind, value`)
	if err != nil {
		return nil, fmt.Errorf("query club name parts: %w", err)
	}
	defer rows.Close()

	pools := map[string]ClubPool{}
	for rows.Next() {
		var code, kind, value string
		if err := rows.Scan(&code, &kind, &value); err != nil {
			return nil, fmt.Errorf("scan club name part: %w", err)
		}
		p := pools[code]
		p.Code = code
		if kind == "stem" {
			p.Stems = append(p.Stems, value)
		} else {
			p.Suffixes = append(p.Suffixes, value)
		}
		pools[code] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate club name parts: %w", err)
	}
	global := pools[""]
	if len(global.Stems) == 0 || len(global.Suffixes) == 0 {
		return nil, fmt.Errorf("%w: global club name pools are empty (run cmd/ref-seed)", ErrRefDataMissing)
	}
	return pools, nil
}

// ListClubNameParts returns one country-scoped club-name pool for the admin
// surface. An empty countryCode lists the global pool.
func (s *Service) ListClubNameParts(ctx context.Context, countryCode string) (stems, suffixes []string, err error) {
	countryCode, err = normalizeCountryCode(countryCode)
	if err != nil {
		return nil, nil, err
	}
	stems = []string{}
	suffixes = []string{}
	rows, err := s.pool.Query(ctx, `
		SELECT kind, value FROM ref.club_name_parts
		WHERE country_code = $1
		ORDER BY kind, value`, countryCode)
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

// AddClubNamePart upserts one stem or suffix into a country-scoped pool.
func (s *Service) AddClubNamePart(ctx context.Context, kind, value, countryCode string) error {
	kind, value, err := normalizeClubNamePart(kind, value)
	if err != nil {
		return err
	}
	countryCode, err = normalizeCountryCode(countryCode)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO ref.club_name_parts (country_code, kind, value, frequency_weight)
		VALUES ($1, $2, $3, 1.0)
		ON CONFLICT (country_code, kind, value) DO NOTHING`, countryCode, kind, value); err != nil {
		return fmt.Errorf("add club name part: %w", err)
	}
	return nil
}

// RemoveClubNamePart deletes one stem or suffix from a country-scoped pool.
// Removing an entry that isn't present is a no-op success.
func (s *Service) RemoveClubNamePart(ctx context.Context, kind, value, countryCode string) error {
	kind, value, err := normalizeClubNamePart(kind, value)
	if err != nil {
		return err
	}
	countryCode, err = normalizeCountryCode(countryCode)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM ref.club_name_parts WHERE country_code = $1 AND kind = $2 AND value = $3`,
		countryCode, kind, value); err != nil {
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

// normalizeCountryCode accepts "" (the global pool) or a valid country code.
func normalizeCountryCode(code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return "", nil
	}
	if len(code) < 2 || len(code) > 4 {
		return "", ErrInvalidClubNameCountry
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", ErrInvalidClubNameCountry
		}
	}
	return code, nil
}
