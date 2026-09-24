package competition

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Regions (IM06): world-scoped administrative country groupings.
//
// A region belongs to exactly one world; its name is unique within that world.
// Countries are assigned to a region via their nullable world.countries
// region_id (an unassigned country is never an error — the geography an admin
// assigns is simply the geography regional competitions later draw from).
// Region CRUD stays metadata-only: no events, no news, matching the CreateCountry
// precedent. The cup_qualification / manager_cup_choices tables migration 0052
// adds are schema-only until IM07/IM09 consume them.
// ---------------------------------------------------------------------------

// reputationInRange reports whether a league reputation sits in the documented
// 0..100 envelope (IM06). Extracted from SetLeagueReputation for unit tests.
func reputationInRange(n int) bool {
	return n >= 0 && n <= 100
}

// CreateRegion adds a world-scoped region. Names are trimmed and unique per
// world.
func (s *Service) CreateRegion(ctx context.Context, worldID uuid.UUID, name string) (*Region, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("region name is required")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM world.worlds WHERE id = $1)`, worldID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check world: %w", err)
	}
	if !exists {
		return nil, ErrWorldNotFound
	}

	var r Region
	err := s.pool.QueryRow(ctx, `
		INSERT INTO world.regions (world_id, name)
		VALUES ($1, $2) RETURNING id, world_id, name`,
		worldID, name,
	).Scan(&r.ID, &r.WorldID, &r.Name)
	if isUniqueViolation(err) {
		return nil, ErrRegionNameCollision
	}
	if err != nil {
		return nil, fmt.Errorf("create region: %w", err)
	}
	return &r, nil
}

// ListRegions returns a world's regions in name order.
func (s *Service) ListRegions(ctx context.Context, worldID uuid.UUID) ([]Region, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, world_id, name FROM world.regions
		WHERE world_id = $1 ORDER BY name`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()
	out := []Region{}
	for rows.Next() {
		var r Region
		if err := rows.Scan(&r.ID, &r.WorldID, &r.Name); err != nil {
			return nil, fmt.Errorf("scan region: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RenameRegion renames a world-scoped region. The target name must stay unique
// within the region's world.
func (s *Service) RenameRegion(ctx context.Context, regionID uuid.UUID, name string) (*Region, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("region name is required")
	}
	var r Region
	err := s.pool.QueryRow(ctx, `
		UPDATE world.regions SET name = $2 WHERE id = $1
		RETURNING id, world_id, name`,
		regionID, name,
	).Scan(&r.ID, &r.WorldID, &r.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRegionNotFound
	}
	if isUniqueViolation(err) {
		return nil, ErrRegionNameCollision
	}
	if err != nil {
		return nil, fmt.Errorf("rename region: %w", err)
	}
	return &r, nil
}

// DeleteRegion removes a region; its countries fall back to unassigned
// (world.countries.region_id is ON DELETE SET NULL).
func (s *Service) DeleteRegion(ctx context.Context, regionID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM world.regions WHERE id = $1`, regionID)
	if err != nil {
		return fmt.Errorf("delete region: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRegionNotFound
	}
	return nil
}

// SetCountryRegion assigns a country to a region (or clears the assignment
// with a nil regionID). The region must belong to the country's world;
// validation happens before any write, so a failed assignment leaves the
// country untouched.
func (s *Service) SetCountryRegion(ctx context.Context, countryID uuid.UUID, regionID *uuid.UUID) (*Country, error) {
	var worldID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT world_id FROM world.countries WHERE id = $1`, countryID).Scan(&worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load country: %w", err)
	}
	if regionID != nil {
		var regionWorld uuid.UUID
		err := s.pool.QueryRow(ctx,
			`SELECT world_id FROM world.regions WHERE id = $1`, *regionID).Scan(&regionWorld)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRegionNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("load region: %w", err)
		}
		if regionWorld != worldID {
			return nil, ErrRegionWorldMismatch
		}
	}

	var country Country
	err = s.pool.QueryRow(ctx, `
		UPDATE world.countries SET region_id = $2 WHERE id = $1
		RETURNING id, world_id, code, name, region_id`,
		countryID, regionID,
	).Scan(&country.ID, &country.WorldID, &country.Code, &country.Name, &country.RegionID)
	if err != nil {
		return nil, fmt.Errorf("assign country region: %w", err)
	}
	return &country, nil
}

// SetLeagueReputation sets a league's admin-managed reputation (0..100). The
// value is the input to the IM08 default-band calculator; the match engine does
// not read it (FixtureContext.LeagueTier is written but never consumed).
func (s *Service) SetLeagueReputation(ctx context.Context, leagueID uuid.UUID, reputation int) (*League, error) {
	if !reputationInRange(reputation) {
		return nil, ErrReputationOutOfRange
	}
	league, err := s.getLeague(ctx, s.pool, leagueID)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE competition.competitions SET reputation = $2
		WHERE id = $1 AND competition_type = 'league'`, leagueID, reputation); err != nil {
		return nil, fmt.Errorf("update league reputation: %w", err)
	}
	league.Reputation = reputation
	return league, nil
}
