package competition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
)

// FixtureMatchday is one fixture date of a season calendar, holding that
// matchday's fixtures. Every fixture of a matchday shares one scheduled day.
type FixtureMatchday struct {
	Matchday    int       `json:"matchday"`
	ScheduledAt time.Time `json:"scheduled_at"`
	Fixtures    []Fixture `json:"fixtures"`
}

// FixtureWeek is one game-week of a season calendar. Week 0 starts on the
// season's start date; a matchday on game-day d sits in week d / daysPerWeek.
type FixtureWeek struct {
	Week      int               `json:"week"`
	FirstDay  time.Time         `json:"first_day"`
	Matchdays []FixtureMatchday `json:"matchdays"`
}

// SeasonCalendar is a competition's current-season fixture calendar, grouped
// into game-weeks (IM03). The same pacing parameters used when the season was
// materialized are re-read here so the grouping always matches the fixtures.
type SeasonCalendar struct {
	Season apiref.SeasonRef `json:"season"`
	Weeks  []FixtureWeek    `json:"weeks"`
}

// seasonRefByLeague returns the league's active (non-completed) season, newest
// first — the season the calendar and standings describe.
func (s *Service) seasonRefByLeague(ctx context.Context, leagueID, worldID uuid.UUID) (apiref.SeasonRef, error) {
	var sr apiref.SeasonRef
	err := s.pool.QueryRow(ctx, `
		SELECT id, season_label, season_number, status
		FROM competition.seasons
		WHERE competition_id = $1 AND world_id = $2 AND status <> 'completed'
		ORDER BY season_number DESC
		LIMIT 1`, leagueID, worldID).
		Scan(&sr.ID, &sr.Label, &sr.Number, &sr.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return apiref.SeasonRef{}, ErrNoSeason
	}
	if err != nil {
		return apiref.SeasonRef{}, fmt.Errorf("active season: %w", err)
	}
	return sr, nil
}

// GetSeasonCalendar returns a league's fixture calendar grouped by game-week
// (IM03). With season == nil it serves the active (non-completed) season;
// otherwise that numbered season (ErrSeasonNotFound if it does not exist).
// Weeks are derived from the same scheduling parameters that paced the
// fixtures, so a league's override and the world's calendar.days_per_week both
// land in the same weeks on every read.
func (s *Service) GetSeasonCalendar(ctx context.Context, worldID, leagueID uuid.UUID, season *int) (*SeasonCalendar, error) {
	if _, err := s.GetLeague(ctx, worldID, leagueID); err != nil {
		return nil, err
	}
	var (
		seasonRef apiref.SeasonRef
		startDate time.Time
	)
	switch {
	case season == nil:
		var err error
		seasonRef, err = s.seasonRefByLeague(ctx, leagueID, worldID)
		if err != nil {
			return nil, err
		}
	default:
		err := s.pool.QueryRow(ctx, `
			SELECT id, season_label, season_number, status, start_date
			FROM competition.seasons
			WHERE competition_id = $1 AND world_id = $2 AND season_number = $3`,
			leagueID, worldID, *season).
			Scan(&seasonRef.ID, &seasonRef.Label, &seasonRef.Number, &seasonRef.Status, &startDate)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSeasonNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("season %d: %w", *season, err)
		}
	}
	if season == nil {
		if err := s.pool.QueryRow(ctx,
			`SELECT start_date FROM competition.seasons WHERE id = $1`, seasonRef.ID).Scan(&startDate); err != nil {
			return nil, fmt.Errorf("season start date: %w", err)
		}
	}
	fixtures, err := s.fixturesInWindow(ctx, leagueID, worldID, seasonRef.Number, startDate)
	if err != nil {
		return nil, err
	}
	p, err := s.scheduleParams(ctx, s.pool, leagueID, worldID)
	if err != nil {
		return nil, err
	}

	start := daysTruncate(startDate)
	cal := &SeasonCalendar{Season: seasonRef, Weeks: []FixtureWeek{}}
	weekIdx := map[int]int{}
	for _, f := range fixtures {
		worldDay := int(daysBetween(start, daysTruncate(f.ScheduledAt)))
		week := worldDay / p.daysPerWeek
		wi, ok := weekIdx[week]
		if !ok {
			weekIdx[week] = len(cal.Weeks)
			wi = len(cal.Weeks)
			cal.Weeks = append(cal.Weeks, FixtureWeek{
				Week:     week,
				FirstDay: start.AddDate(0, 0, week*p.daysPerWeek),
			})
		}
		w := &cal.Weeks[wi]
		if n := len(w.Matchdays); n == 0 || w.Matchdays[n-1].Matchday != f.Matchday {
			w.Matchdays = append(w.Matchdays, FixtureMatchday{Matchday: f.Matchday, ScheduledAt: f.ScheduledAt})
		}
		last := &w.Matchdays[len(w.Matchdays)-1]
		last.Fixtures = append(last.Fixtures, f)
	}
	return cal, nil
}

// fixturesInWindow returns a season's fixtures: everything scheduled from its
// start_date up to (but excluding) the next season's start_date. match.fixtures
// carries no season id, so the boundary is derived from the start_date
// sequence; windows are contiguous because each season's fixtures are anchored
// ≥ its own start_date and every later season starts after the previous one's
// last matchday (the off-season gap, IM01).
func (s *Service) fixturesInWindow(ctx context.Context, leagueID, worldID uuid.UUID, seasonNumber int, startDate time.Time) ([]Fixture, error) {
	all, err := s.GetFixtures(ctx, leagueID, worldID, nil)
	if err != nil {
		return nil, err
	}
	var nextStart time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT start_date FROM competition.seasons
		WHERE competition_id = $1 AND world_id = $2 AND season_number > $3
		ORDER BY season_number ASC LIMIT 1`, leagueID, worldID, seasonNumber).Scan(&nextStart)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("next season boundary: %w", err)
	}
	start := daysTruncate(startDate)
	next := time.Time{}
	if !nextStart.IsZero() {
		next = daysTruncate(nextStart)
	}
	fixtures := make([]Fixture, 0, len(all))
	for _, f := range all {
		if f.ScheduledAt.Before(start) || (!next.IsZero() && !f.ScheduledAt.Before(next)) {
			continue
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

// ListClubFixtures lists a club's fixtures, earliest scheduled first, scoped to
// the caller's world (IM03: a season-wide look-ahead for one club). The handler
// verifies the club belongs to the caller's world like other club reads.
func (s *Service) ListClubFixtures(ctx context.Context, worldID, clubID uuid.UUID, limit int) ([]Fixture, error) {
	var clubWorld uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT world_id FROM club.clubs WHERE id = $1`, clubID).Scan(&clubWorld)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("club world: %w", err)
	}
	if clubWorld != worldID {
		return nil, ErrClubWorldMismatch
	}
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.world_id = $1 AND (f.home_club_id = $2 OR f.away_club_id = $2)
		  AND f.status <> 'cancelled'
		ORDER BY f.scheduled_at, f.matchday, f.home_club_id
		LIMIT $3`, worldID, clubID, limit)
	if err != nil {
		return nil, fmt.Errorf("query club fixtures: %w", err)
	}
	return scanFixtures(rows)
}

// daysBetween is the whole-day count between two truncated UTC dates (exact,
// because both are midnight UTC).
func daysBetween(a, b time.Time) int {
	return int(b.Sub(a) / (24 * time.Hour))
}
