package competition

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetFixtures lists a competition's fixtures, optionally filtered to one
// matchday. Ordering is stable: matchday, then scheduled_at, then home club.
func (s *Service) GetFixtures(ctx context.Context, leagueID uuid.UUID, worldID uuid.UUID, matchday *int) ([]Fixture, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score, h.name, a.name
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		WHERE f.competition_id = $1 AND f.world_id = $2 AND f.status <> 'cancelled'
		  AND ($3::int IS NULL OR f.matchday = $3)
		ORDER BY f.matchday, f.scheduled_at, f.home_club_id`, leagueID, worldID, matchday)
	if err != nil {
		return nil, fmt.Errorf("query fixtures: %w", err)
	}
	defer rows.Close()

	out := []Fixture{}
	for rows.Next() {
		var f Fixture
		if err := rows.Scan(&f.ID, &f.WorldID, &f.CompetitionID, &f.HomeClubID, &f.AwayClubID,
			&f.Matchday, &f.ScheduledAt, &f.Status, &f.HomeScore, &f.AwayScore,
			&f.HomeClubName, &f.AwayClubName); err != nil {
			return nil, fmt.Errorf("scan fixture: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetStandings returns the current table for a competition's active season.
// Tie-breakers are points, goal difference, goals scored, then club name.
func (s *Service) GetStandings(ctx context.Context, leagueID uuid.UUID, worldID uuid.UUID) (*StandingRowSet, error) {
	var seasonID uuid.UUID
	var label string
	var number int
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT id, season_label, season_number, status
		FROM competition.seasons
		WHERE competition_id = $1 AND world_id = $2 AND status <> 'completed'
		ORDER BY season_number DESC
		LIMIT 1`, leagueID, worldID).
		Scan(&seasonID, &label, &number, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSeason
	}
	if err != nil {
		return nil, fmt.Errorf("active season: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.name, COALESCE(c.short_name, ''),
		       st.played, st.won, st.drawn, st.lost, st.goals_for, st.goals_against, st.points
		FROM competition.standings st
		JOIN club.clubs c ON c.id = st.club_id
		WHERE st.season_id = $1
		ORDER BY st.points DESC, (st.goals_for - st.goals_against) DESC, st.goals_for DESC, c.name`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("standings: %w", err)
	}
	defer rows.Close()

	out := &StandingRowSet{
		SeasonID:     seasonID,
		SeasonLabel:  label,
		SeasonNumber: number,
		Status:       status,
		Rows:         []StandingRow{},
	}
	for rows.Next() {
		var r StandingRow
		if err := rows.Scan(&r.ClubName, &r.ClubShort, &r.Played, &r.Won, &r.Drawn, &r.Lost,
			&r.GoalsFor, &r.GoalsAgainst, &r.Points); err != nil {
			return nil, fmt.Errorf("scan standing: %w", err)
		}
		out.Rows = append(out.Rows, r)
	}
	return out, rows.Err()
}

// ApplyResult records a completed match and rolls the competition forward.
// The fixture row is locked so concurrent submissions cannot double-apply;
// once the last fixture of every league in the country has a result, the
// promotion/relegation cascade runs in the same transaction.
func (s *Service) ApplyResult(ctx context.Context, fixtureID uuid.UUID, homeScore, awayScore int) error {
	if homeScore < 0 || awayScore < 0 {
		return ErrInvalidResult
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin result tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		worldID, countryID, leagueID, homeClub, awayClub uuid.UUID
		status                                           string
	)
	err = tx.QueryRow(ctx, `
		SELECT f.world_id, c.country_id, f.competition_id, f.home_club_id, f.away_club_id, f.status
		FROM match.fixtures f
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.id = $1 FOR UPDATE OF f`, fixtureID).
		Scan(&worldID, &countryID, &leagueID, &homeClub, &awayClub, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFixtureNotFound
	}
	if err != nil {
		return fmt.Errorf("lock fixture: %w", err)
	}
	if status == "completed" {
		return ErrResultAlreadyApplied
	}

	season, err := s.activeSeason(ctx, tx, leagueID, worldID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE match.fixtures SET status = 'completed', completed_at = now(),
			ht_score = $2, at_score = $3
		WHERE id = $1`, fixtureID, homeScore, awayScore); err != nil {
		return fmt.Errorf("complete fixture: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE match.matches
		SET status = 'completed', home_score = $2, away_score = $3, ended_at = now()
		WHERE fixture_id = $1 AND status <> 'completed'`,
		fixtureID, homeScore, awayScore); err != nil {
		return fmt.Errorf("update match: %w", err)
	}

	homePts, awayPts := resultPoints(homeScore, awayScore)
	if err := s.upsertStanding(ctx, tx, season.ID, homeClub, homeScore, awayScore, homePts); err != nil {
		return err
	}
	if err := s.upsertStanding(ctx, tx, season.ID, awayClub, awayScore, homeScore, awayPts); err != nil {
		return err
	}

	if err := s.maybeCompleteSeason(ctx, tx, worldID, countryID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) upsertStanding(ctx context.Context, tx pgx.Tx, seasonID, clubID uuid.UUID,
	goalsFor, goalsAgainst, points int) error {
	won, drawn, lost := 0, 0, 0
	switch points {
	case 3:
		won = 1
	case 1:
		drawn = 1
	default:
		lost = 1
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO competition.standings
			(season_id, club_id, played, won, drawn, lost, goals_for, goals_against, points)
		VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (season_id, club_id) DO UPDATE SET
			played        = standings.played + 1,
			won           = standings.won + EXCLUDED.won,
			drawn         = standings.drawn + EXCLUDED.drawn,
			lost          = standings.lost + EXCLUDED.lost,
			goals_for     = standings.goals_for + EXCLUDED.goals_for,
			goals_against = standings.goals_against + EXCLUDED.goals_against,
			points        = standings.points + EXCLUDED.points`,
		seasonID, clubID, won, drawn, lost, goalsFor, goalsAgainst, points)
	if err != nil {
		return fmt.Errorf("upsert standing: %w", err)
	}
	return nil
}

func resultPoints(home, away int) (int, int) {
	switch {
	case home > away:
		return 3, 0
	case away > home:
		return 0, 3
	default:
		return 1, 1
	}
}

func (s *Service) activeSeason(ctx context.Context, tx pgx.Tx, leagueID, worldID uuid.UUID) (*Season, error) {
	var se Season
	err := tx.QueryRow(ctx, `
		SELECT id, competition_id, season_label, season_number, status
		FROM competition.seasons
		WHERE competition_id = $1 AND world_id = $2 AND status <> 'completed'
		ORDER BY season_number DESC LIMIT 1`, leagueID, worldID).
		Scan(&se.ID, &se.CompetitionID, &se.SeasonLabel, &se.SeasonNumber, &se.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSeason
	}
	if err != nil {
		return nil, fmt.Errorf("active season: %w", err)
	}
	return &se, nil
}

// maybeCompleteSeason checks whether every league in the country is done for
// its current season; when all are, seasons get completed and the cascade runs.
func (s *Service) maybeCompleteSeason(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) error {
	var remaining int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM match.fixtures f
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE c.country_id = $1 AND f.world_id = $2 AND f.status <> 'completed'`,
		countryID, worldID).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining fixtures: %w", err)
	}
	if remaining > 0 {
		return nil
	}
	return s.rolloverCountry(ctx, tx, worldID, countryID)
}
