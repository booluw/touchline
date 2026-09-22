package academy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/apiref"
)

// store owns academy persistence. Writers accept the caller's pgx.Tx so intake
// and configuration land atomically with their event row; reads use the pool.
type store struct {
	pool *pgxpool.Pool
}

func newStore(pool *pgxpool.Pool) *store { return &store{pool: pool} }

var errAcademyMissing = errors.New("academy: row missing")

// ensureAcademyTx lazily creates a default tier-1 academy row for a club. Safe
// to call repeatedly (ON CONFLICT DO NOTHING).
func ensureAcademyTx(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO club.academies (club_id, world_id)
		VALUES ($1, $2)
		ON CONFLICT (club_id) DO NOTHING`, clubID, worldID); err != nil {
		return fmt.Errorf("ensure academy: %w", err)
	}
	return nil
}

// loadAcademyTx reads a club's academy row inside a transaction.
func loadAcademyTx(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (Academy, error) {
	return scanAcademy(tx.QueryRow(ctx, academySelect+` WHERE club_id = $1`, clubID))
}

// loadAcademy reads a club's academy row via the pool.
func (s *store) loadAcademy(ctx context.Context, clubID uuid.UUID) (Academy, error) {
	return scanAcademy(s.pool.QueryRow(ctx, academySelect+` WHERE club_id = $1`, clubID))
}

// lockAcademy reads a club's academy row FOR UPDATE (intake serialisation).
func lockAcademyTx(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (Academy, error) {
	return scanAcademy(tx.QueryRow(ctx, academySelect+` WHERE club_id = $1 FOR UPDATE`, clubID))
}

const academySelect = `
	SELECT a.club_id, a.world_id, a.is_active, a.investment_tier, a.facility_level,
	       a.scouting_level, a.staff_quality, COALESCE(a.annual_cost, 0)::bigint,
	       a.reputation, a.regional_reach, a.last_intake_season, a.shutdown_at, a.reopened_at,
	       c.name
	FROM club.academies a
	JOIN club.clubs c ON c.id = a.club_id`

func scanAcademy(row pgx.Row) (Academy, error) {
	var a Academy
	var clubName string
	err := row.Scan(&a.ClubID, &a.WorldID, &a.IsActive, &a.InvestmentTier,
		&a.FacilityLevel, &a.ScoutingLevel, &a.StaffQuality, &a.AnnualCost,
		&a.Reputation, &a.RegionalReach, &a.LastIntakeSeason, &a.ShutdownAt, &a.ReopenedAt,
		&clubName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Academy{}, errAcademyMissing
	}
	if err != nil {
		return Academy{}, fmt.Errorf("scan academy: %w", err)
	}
	if a.RegionalReach == nil {
		a.RegionalReach = []string{}
	}
	a.Club = &apiref.ClubRef{ID: a.ClubID, Name: clubName}
	return a, nil
}

// clubContext is the country context an intake needs for nationality biasing.
type clubContext struct {
	WorldID     uuid.UUID
	Country     string
	CountryID   *uuid.UUID
	CountryCode string
}

// loadClubContext resolves a club's world plus its world.countries row (best
// effort: a club whose `country` name has no matching country row yields nil
// country_id and an empty code, and intake falls back to weighted
// nationality).
func loadClubContext(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (clubContext, error) {
	var c clubContext
	err := tx.QueryRow(ctx, `
		SELECT c.world_id, c.country, wc.id, COALESCE(wc.code, '')
		FROM club.clubs c
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND wc.name = c.country
		WHERE c.id = $1`, clubID).Scan(&c.WorldID, &c.Country, &c.CountryID, &c.CountryCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrClubNotFound
	}
	if err != nil {
		return c, fmt.Errorf("load club context: %w", err)
	}
	return c, nil
}

// validNationality reports whether a country code exists in ref.nationalities,
// so a forced nationality never breaks generation. The code is matched
// case-insensitively against the lowercase ref codes.
func validNationality(ctx context.Context, tx pgx.Tx, code string) bool {
	if code == "" {
		return false
	}
	var ok bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM ref.nationalities WHERE code = $1)`, strings.ToLower(code)).Scan(&ok)
	return ok
}

// markClubIntake stamps a club's last intake season.
func markClubIntake(ctx context.Context, tx pgx.Tx, clubID uuid.UUID, season int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE club.academies SET last_intake_season = $2
		WHERE club_id = $1`, clubID, season); err != nil {
		return fmt.Errorf("stamp club intake: %w", err)
	}
	return nil
}

// tryClaimCountryIntake inserts the (world, country, season) idempotency row,
// returning true when this caller won the claim. A redelivered season hook
// loses the race and must not re-run the street intake.
func tryClaimCountryIntake(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID, season int) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO world.country_academy_intakes (world_id, country_id, season_number, player_count)
		VALUES ($1, $2, $3, 0)
		ON CONFLICT (world_id, country_id, season_number) DO NOTHING`,
		worldID, countryID, season)
	if err != nil {
		return false, fmt.Errorf("claim country intake: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// finishCountryIntake records the generated count on the claim row.
func finishCountryIntake(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID, season, count int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE world.country_academy_intakes SET player_count = $4
		WHERE world_id = $1 AND country_id = $2 AND season_number = $3`,
		worldID, countryID, season, count); err != nil {
		return fmt.Errorf("finish country intake: %w", err)
	}
	return nil
}
