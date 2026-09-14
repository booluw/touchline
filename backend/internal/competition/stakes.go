package competition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// stakesPointsGap is the points window within which two clubs engaged in the
// same promotion/relegation battle count as a six-pointer. Product-owned
// tuning; documented in the S04-02 delivery evidence.
const stakesPointsGap = 3

// stakesClub snapshots one club for a standings-stakes decision.
type stakesClub struct {
	clubID    uuid.UUID
	position  int // 1-based table position
	points    int
	remaining int // unplayed future fixtures (kickoffs after this match's)
	promSafe  bool
	relegSafe bool
}

// stakes is the precomputed input for IsSixPointer/IsDeadRubber, derived in
// one read pass over the league's current table.
type stakes struct {
	teamCount   int
	promotions  int
	relegations int
	home        stakesClub
	away        stakesClub
}

// tableRow is one ordered league-table line used by the stakes math.
type tableRow struct {
	clubID uuid.UUID
	points int
}

// IsSixPointer reports whether a fixture is a promotion/relegation six-pointer:
// both clubs sit in the same battle band (off the promotion line or in the
// drop fight) within stakesPointsGap points. Leagues without movement rules
// and matches with no table info yet are reported as false (no stakes).
func (s *Service) IsSixPointer(ctx context.Context, fixtureID uuid.UUID) (bool, error) {
	st, ok, err := s.stakesFor(ctx, fixtureID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return st.sixPointer(), nil
}

// IsDeadRubber reports whether a fixture's outcome is provably irrelevant to
// both clubs' promotion/relegation fate: even on the best/worst possible
// points runs across the remaining games, neither club can reach a promotion
// slot nor fall into the relegation zone. The checks err toward "live" —
// early-season tables and anything unprovable report false.
func (s *Service) IsDeadRubber(ctx context.Context, fixtureID uuid.UUID) (bool, error) {
	st, ok, err := s.stakesFor(ctx, fixtureID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return st.deadRubber(), nil
}

// sixPointer: both clubs in the promotion band (positions 1..promotions+1) or
// both in the relegation fight (the drop zone plus the first safe slot above),
// within stakesPointsGap.
func (st *stakes) sixPointer() bool {
	if st.promotions == 0 && st.relegations == 0 {
		return false
	}
	gap := st.home.points - st.away.points
	if gap < -stakesPointsGap || gap > stakesPointsGap {
		return false
	}
	if st.promotions > 0 &&
		st.home.position <= st.promotions+1 && st.away.position <= st.promotions+1 {
		return true
	}
	if st.relegations > 0 {
		bandEdge := st.teamCount - st.relegations
		if st.home.position >= bandEdge && st.away.position >= bandEdge {
			return true
		}
	}
	return false
}

// deadRubber: both clubs are simultaneously promotion-safe and relegation-safe.
func (st *stakes) deadRubber() bool {
	return st.home.promSafe && st.home.relegSafe &&
		st.away.promSafe && st.away.relegSafe
}

// stakesFor loads a fixture's league table in one pass and computes the risk
// bands. ok=false means there is nothing to stake against (no rules row, no
// active season, no standings yet, or a club missing from the table) —
// callers report "no stakes" rather than an error so a scheduled fixture in a
// half-seeded world still simulates normally.
func (s *Service) stakesFor(ctx context.Context, fixtureID uuid.UUID) (*stakes, bool, error) {
	var (
		worldID, leagueID, homeID, awayID uuid.UUID
		kickoff                           time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT world_id, competition_id, home_club_id, away_club_id, scheduled_at
		FROM match.fixtures WHERE id = $1`, fixtureID).
		Scan(&worldID, &leagueID, &homeID, &awayID, &kickoff)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrFixtureNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("stakes fixture: %w", err)
	}

	st := &stakes{}
	err = s.pool.QueryRow(ctx, `
		SELECT c.team_count, r.promotions, r.relegations
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.id = $1`, leagueID).
		Scan(&st.teamCount, &st.promotions, &st.relegations)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil // custom competition without a rules row
	}
	if err != nil {
		return nil, false, fmt.Errorf("stakes rules: %w", err)
	}

	var seasonID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM competition.seasons
		WHERE competition_id = $1 AND world_id = $2 AND status <> 'completed'
		ORDER BY season_number DESC LIMIT 1`, leagueID, worldID).Scan(&seasonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("stakes active season: %w", err)
	}

	table := []tableRow{}
	rows, err := s.pool.Query(ctx, `
		SELECT st.club_id, st.points
		FROM competition.standings st
		JOIN club.clubs c ON c.id = st.club_id
		WHERE st.season_id = $1
		ORDER BY st.points DESC, (st.goals_for - st.goals_against) DESC, st.goals_for DESC, c.name`,
		seasonID)
	if err != nil {
		return nil, false, fmt.Errorf("stakes table: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r tableRow
		if err := rows.Scan(&r.clubID, &r.points); err != nil {
			return nil, false, fmt.Errorf("stakes scan: %w", err)
		}
		table = append(table, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("stakes iterate: %w", err)
	}
	if len(table) == 0 {
		return nil, false, nil // no results recorded yet — nothing to decide
	}

	// Future kickoffs still to play per club: the games that could still move
	// either club's fate.
	remaining := map[uuid.UUID]int{}
	rem, err := s.pool.Query(ctx, `
		SELECT club_id, COUNT(*)
		FROM (
			SELECT home_club_id AS club_id FROM match.fixtures
			WHERE competition_id = $1 AND world_id = $2 AND id <> $3
			  AND scheduled_at >= $4 AND status NOT IN ('completed','cancelled')
			UNION ALL
			SELECT away_club_id AS club_id FROM match.fixtures
			WHERE competition_id = $1 AND world_id = $2 AND id <> $3
			  AND scheduled_at >= $4 AND status NOT IN ('completed','cancelled')
		) future GROUP BY club_id`, leagueID, worldID, fixtureID, kickoff)
	if err != nil {
		return nil, false, fmt.Errorf("stakes remaining: %w", err)
	}
	defer rem.Close()
	for rem.Next() {
		var (
			id  uuid.UUID
			cnt int
		)
		if err := rem.Scan(&id, &cnt); err != nil {
			return nil, false, fmt.Errorf("stakes remaining scan: %w", err)
		}
		remaining[id] = cnt
	}
	if err := rem.Err(); err != nil {
		return nil, false, fmt.Errorf("stakes remaining iterate: %w", err)
	}

	position := map[uuid.UUID]int{}
	points := map[uuid.UUID]int{}
	for i, r := range table {
		position[r.clubID] = i + 1
		points[r.clubID] = r.points
	}

	homePos, homeOK := position[homeID]
	awayPos, awayOK := position[awayID]
	if !homeOK || !awayOK {
		return nil, false, nil
	}

	st.home = stakesClubOf(homeID, homePos, points[homeID], remaining, table, position, st)
	st.away = stakesClubOf(awayID, awayPos, points[awayID], remaining, table, position, st)

	// Promotion line: the final promoted slot's current points. A club whose
	// best possible finish (pts + 3 per remaining game) stays below that line
	// cannot reach promotion — the line can only rise from here.
	promotionLine := -1
	if st.promotions > 0 && st.promotions <= len(table) {
		promotionLine = table[st.promotions-1].points
	}
	if st.promotions == 0 {
		st.home.promSafe, st.away.promSafe = true, true
	} else {
		st.home.promSafe = points[homeID]+3*st.home.remaining < promotionLine
		st.away.promSafe = points[awayID]+3*st.away.remaining < promotionLine
	}
	return st, true, nil
}

// stakesClubOf fills a club's relegation safety. With no relegation rule the
// club is trivially safe; otherwise the worst-case rank is computed: everyone
// ranked above stays ahead, and every club below that can overtake the club's
// points floor (pts, if it loses its remaining games) leapfrogs it. If even
// that rank stays out of the drop zone the club is provably safe.
func stakesClubOf(clubID uuid.UUID, position, pts int, remaining map[uuid.UUID]int,
	table []tableRow, positionOf map[uuid.UUID]int, st *stakes) stakesClub {
	cl := stakesClub{clubID: clubID, position: position, points: pts, remaining: remaining[clubID]}
	if st.relegations == 0 {
		cl.relegSafe = true
		return cl
	}
	above := position - 1
	belowCanPass := 0
	for _, r := range table {
		if positionOf[r.clubID] > position && r.points+3*remaining[r.clubID] > pts {
			belowCanPass++
		}
	}
	cl.relegSafe = above+belowCanPass+1 <= st.teamCount-st.relegations
	return cl
}
