package competition

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SeasonFixture is one row of a club's fixtures & results page (IM67).
type SeasonFixture struct {
	Fixture
	// Attendance is the crowd (IM66); null until kickoff and for matches
	// played before crowds were modelled.
	Attendance *int `json:"attendance"`
	// PositionAfter is the club's league position once this league match's
	// matchday is in; null for cup ties and unplayed fixtures.
	PositionAfter *int `json:"position_after"`
	// LastMeeting is the most recent completed fixture between the two clubs
	// (any competition); only on unplayed fixtures, null when they never met.
	LastMeeting *Fixture `json:"last_meeting"`
	// Difficulty is set on unplayed fixtures only (IM59 rating).
	Difficulty *Difficulty `json:"difficulty"`
}

// ClubSeasonFixtures lists every fixture of the club's current season, all
// competitions, earliest first. The season is the club's league season
// window: from its start_date up to the next season's. A club with no league
// or no active season gets an empty list.
func (s *Service) ClubSeasonFixtures(ctx context.Context, worldID, clubID uuid.UUID) ([]SeasonFixture, error) {
	if err := s.checkClubWorld(ctx, worldID, clubID); err != nil {
		return nil, err
	}
	var leagueID uuid.UUID
	var seasonNumber int
	var start time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT cc.competition_id, se.season_number, se.start_date
		FROM competition.club_competitions cc
		JOIN competition.seasons se ON se.competition_id = cc.competition_id AND se.world_id = $2
		WHERE cc.club_id = $1 AND cc.role = 'league' AND se.status <> 'completed'
		ORDER BY se.season_number DESC LIMIT 1`, clubID, worldID).Scan(&leagueID, &seasonNumber, &start)
	if errors.Is(err, pgx.ErrNoRows) {
		return []SeasonFixture{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("club league season: %w", err)
	}

	leagueFixtures, err := s.fixturesInWindow(ctx, leagueID, worldID, seasonNumber, start)
	if err != nil {
		return nil, err
	}
	// The window runs to the next league season's start, so cup ties after
	// the last league matchday are included. Zero = open-ended (no next season yet).
	var next time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT start_date FROM competition.seasons
		WHERE competition_id = $1 AND world_id = $2 AND season_number > $3
		ORDER BY season_number ASC LIMIT 1`, leagueID, worldID, seasonNumber).Scan(&next)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("next season boundary: %w", err)
	}
	if !next.IsZero() {
		next = daysTruncate(next)
	}
	fixtures, err := s.clubFixturesBetween(ctx, worldID, clubID, daysTruncate(start), next)
	if err != nil {
		return nil, err
	}

	attendance, err := s.fixtureAttendances(ctx, fixtures)
	if err != nil {
		return nil, err
	}
	positions := positionsAfter(leagueFixtures, clubID)

	var upcoming []Fixture
	out := make([]SeasonFixture, 0, len(fixtures))
	for _, f := range fixtures {
		row := SeasonFixture{Fixture: f}
		if a, ok := attendance[f.ID]; ok {
			row.Attendance = &a
		}
		if p, ok := positions[f.ID]; ok {
			row.PositionAfter = &p
		}
		if f.Status == "scheduled" || f.Status == "postponed" {
			upcoming = append(upcoming, f)
		}
		out = append(out, row)
	}
	return out, s.attachUpcoming(ctx, worldID, clubID, upcoming, out)
}

// clubFixturesBetween is ListClubFixtures bounded to [from, to) (to zero =
// no upper bound) with no row cap: a season is ~50 fixtures.
func (s *Service) clubFixturesBetween(ctx context.Context, worldID, clubID uuid.UUID, from, to time.Time) ([]Fixture, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name,
		       (SELECT m.id FROM match.matches m WHERE m.fixture_id = f.id AND m.status = 'completed')
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.world_id = $1 AND (f.home_club_id = $2 OR f.away_club_id = $2)
		  AND f.status <> 'cancelled'
		  AND f.scheduled_at >= $3 AND ($4::timestamptz IS NULL OR f.scheduled_at < $4)
		ORDER BY f.scheduled_at, f.matchday, f.home_club_id`,
		worldID, clubID, from, nullTime(to))
	if err != nil {
		return nil, fmt.Errorf("query club season fixtures: %w", err)
	}
	return scanFixtures(rows)
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (s *Service) fixtureAttendances(ctx context.Context, fixtures []Fixture) (map[uuid.UUID]int, error) {
	ids := make([]uuid.UUID, 0, len(fixtures))
	for _, f := range fixtures {
		ids = append(ids, f.ID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT fixture_id, attendance FROM match.matches
		WHERE fixture_id = ANY($1) AND attendance IS NOT NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("query attendance: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID]int, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan attendance: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}

// positionsAfter replays the league table matchday by matchday and returns,
// for each completed league fixture of clubID, the club's position once that
// matchday's played results are in. Ordering matches GetStandings: points,
// goal difference, goals for, name.
func positionsAfter(league []Fixture, clubID uuid.UUID) map[uuid.UUID]int {
	type row struct {
		name        string
		pts, gf, ga int
	}
	table := map[uuid.UUID]*row{}
	for _, f := range league {
		for _, c := range []struct {
			id   uuid.UUID
			name string
		}{{f.HomeClub.ID, f.HomeClub.Name}, {f.AwayClub.ID, f.AwayClub.Name}} {
			if table[c.id] == nil {
				table[c.id] = &row{name: c.name}
			}
		}
	}
	rank := func() int {
		ids := make([]uuid.UUID, 0, len(table))
		for id := range table {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := table[ids[i]], table[ids[j]]
			if a.pts != b.pts {
				return a.pts > b.pts
			}
			if a.gf-a.ga != b.gf-b.ga {
				return a.gf-a.ga > b.gf-b.ga
			}
			if a.gf != b.gf {
				return a.gf > b.gf
			}
			return a.name < b.name
		})
		for i, id := range ids {
			if id == clubID {
				return i + 1
			}
		}
		return 0
	}

	out := map[uuid.UUID]int{}
	var pending []uuid.UUID // the club's played fixtures in the current matchday
	for i, f := range league {
		if f.Status == "completed" && f.HomeScore != nil && f.AwayScore != nil {
			h, a := table[f.HomeClub.ID], table[f.AwayClub.ID]
			hp, ap := resultPoints(*f.HomeScore, *f.AwayScore)
			h.pts, h.gf, h.ga = h.pts+hp, h.gf+*f.HomeScore, h.ga+*f.AwayScore
			a.pts, a.gf, a.ga = a.pts+ap, a.gf+*f.AwayScore, a.ga+*f.HomeScore
			if f.HomeClub.ID == clubID || f.AwayClub.ID == clubID {
				pending = append(pending, f.ID)
			}
		}
		if (i == len(league)-1 || league[i+1].Matchday != f.Matchday) && len(pending) > 0 {
			p := rank()
			for _, id := range pending {
				out[id] = p
			}
			pending = pending[:0]
		}
	}
	return out
}

// attachUpcoming fills difficulty and last meeting on the unplayed rows of out.
func (s *Service) attachUpcoming(ctx context.Context, worldID, clubID uuid.UUID, upcoming []Fixture, out []SeasonFixture) error {
	if len(upcoming) == 0 {
		return nil
	}
	// ponytail: two squad loads per unplayed fixture (~25 at season start);
	// cache per (club, day) if this endpoint shows up in latency.
	rated, err := s.rateUpcoming(ctx, clubID, upcoming)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]*SeasonFixture, len(out))
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for i := range rated {
		d := rated[i].Difficulty
		row := byID[rated[i].ID]
		row.Difficulty = &d
		last, err := s.lastMeeting(ctx, worldID, clubID, opponentOf(rated[i].Fixture, clubID), rated[i].ScheduledAt)
		if err != nil {
			return err
		}
		row.LastMeeting = last
	}
	return nil
}

func (s *Service) lastMeeting(ctx context.Context, worldID, clubID, oppID uuid.UUID, before time.Time) (*Fixture, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name,
		       (SELECT m.id FROM match.matches m WHERE m.fixture_id = f.id AND m.status = 'completed')
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.world_id = $1 AND f.status = 'completed' AND f.scheduled_at < $4
		  AND ((f.home_club_id = $2 AND f.away_club_id = $3) OR (f.home_club_id = $3 AND f.away_club_id = $2))
		ORDER BY f.scheduled_at DESC LIMIT 1`, worldID, clubID, oppID, before)
	if err != nil {
		return nil, fmt.Errorf("query last meeting: %w", err)
	}
	fs, err := scanFixtures(rows)
	if err != nil || len(fs) == 0 {
		return nil, err
	}
	return &fs[0], nil
}
