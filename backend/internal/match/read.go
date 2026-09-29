package match

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// Read path
// ---------------------------------------------------------------------------

// GetFixture returns a fixture row for the feed/read path.
func (s *Service) GetFixture(ctx context.Context, id uuid.UUID) (*Fixture, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	f := &Fixture{}
	var hcName, hcShort, acName, acShort, compName string
	err = conn.QueryRow(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id,
		       COALESCE(f.matchday, 0), f.scheduled_at, f.status,
		       hc.name, COALESCE(hc.short_name, ''), ac.name, COALESCE(ac.short_name, ''),
		       c.name
		FROM match.fixtures f
		JOIN club.clubs hc ON hc.id = f.home_club_id
		JOIN club.clubs ac ON ac.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.id = $1`, id).
		Scan(&f.ID, &f.WorldID, &f.Competition.ID, &f.HomeClub.ID, &f.AwayClub.ID,
			&f.Matchday, &f.ScheduledAt, &f.Status,
			&hcName, &hcShort, &acName, &acShort, &compName)
	if err != nil {
		return nil, err
	}
	f.Gameweek = f.Matchday
	f.Competition.Name = compName
	f.HomeClub.Name = hcName
	f.HomeClub.Short = hcShort
	f.AwayClub.Name = acName
	f.AwayClub.Short = acShort
	return f, nil
}

// GetFixtureMatch aggregates the match-screen header for a fixture: the fixture
// with club names plus its match view (status, live clock, server-computed
// scoreline), or a nil Match when the fixture has not kicked off yet.
func (s *Service) GetFixtureMatch(ctx context.Context, fixtureID uuid.UUID) (*FixtureMatch, error) {
	f, err := s.GetFixture(ctx, fixtureID)
	if err != nil {
		return nil, err
	}
	out := &FixtureMatch{Fixture: f}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	mv := &MatchView{}
	err = conn.QueryRow(ctx, `
		SELECT id, status, COALESCE(current_minute, 0), COALESCE(home_score, 0), COALESCE(away_score, 0)
		FROM match.matches WHERE fixture_id = $1`, fixtureID).
		Scan(&mv.ID, &mv.Status, &mv.Minute, &mv.HomeScore, &mv.AwayScore)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil // not kicked off yet
		}
		return nil, err
	}
	home, away, err := s.ScoreLine(ctx, mv.ID)
	if err != nil {
		return nil, err
	}
	mv.HomeScore = home
	mv.AwayScore = away
	out.Match = mv
	return out, nil
}

// GetMatchEvents returns a completed match's full cast feed.
func (s *Service) GetMatchEvents(ctx context.Context, matchID uuid.UUID) ([]*MatchEventRow, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	rows, err := conn.Query(ctx, `
		SELECT id, match_id, sequence, minute, event_type, club_id, player_id, related_player_id, detail
		FROM match.match_events WHERE match_id = $1 ORDER BY sequence`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MatchEventRow
	for rows.Next() {
		e := &MatchEventRow{}
		var clubID, playerID, relatedID *uuid.UUID
		if err := rows.Scan(&e.ID, &e.Match.ID, &e.Sequence, &e.Minute, &e.Type,
			&clubID, &playerID, &relatedID, &e.Detail); err != nil {
			return nil, err
		}
		e.Club = clubRef(clubID)
		e.Player = playerRef(playerID)
		e.RelatedPlayer = playerRef(relatedID)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	resolveEventRefs(ctx, s.pool, out)
	return out, nil
}

// GetMatch returns a match row by id for the read/feed path.
func (s *Service) GetMatch(ctx context.Context, id uuid.UUID) (*Match, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	m := &Match{}
	err = conn.QueryRow(ctx, `
		SELECT id, fixture_id, world_id, seed, engine_version,
		       COALESCE(home_score, 0), COALESCE(away_score, 0), status, ended_at
		FROM match.matches WHERE id = $1`, id).
		Scan(&m.ID, &m.FixtureID, &m.WorldID, &m.Seed, &m.EngineVersion,
			&m.HomeGoals, &m.AwayGoals, &m.Status, &m.EndedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ScoreLine returns a match's goal tally server-side. While live it aggregates
// goal/penalty events from the same persisted feed the whole pipeline serves,
// so the client never computes outcomes; at completion the stamped match
// scorelines are returned directly. On sql.ErrNoRows the match does not exist.
func (s *Service) ScoreLine(ctx context.Context, matchID uuid.UUID) (int, int, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Release()

	var status string
	var stampedHome, stampedAway int
	if err := conn.QueryRow(ctx, `
		SELECT status, COALESCE(home_score, 0), COALESCE(away_score, 0)
		FROM match.matches WHERE id = $1`, matchID).
		Scan(&status, &stampedHome, &stampedAway); err != nil {
		return 0, 0, err
	}
	if status == "completed" {
		return stampedHome, stampedAway, nil
	}

	var home, away int
	if err := conn.QueryRow(ctx, `
		SELECT
			count(me.id) FILTER (WHERE me.club_id = f.home_club_id AND me.event_type IN ('goal', 'penalty_scored')),
			count(me.id) FILTER (WHERE me.club_id = f.away_club_id AND me.event_type IN ('goal', 'penalty_scored'))
		FROM match.matches m
		JOIN match.fixtures f ON f.id = m.fixture_id
		LEFT JOIN match.match_events me ON me.match_id = m.id
		WHERE m.id = $1
		GROUP BY m.id`, matchID).Scan(&home, &away); err != nil {
		return 0, 0, err
	}
	return home, away, nil
}
