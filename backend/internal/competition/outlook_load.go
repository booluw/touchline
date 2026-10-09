package competition

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
)

// recentGames is the trailing window for the GD and xG trend factors.
const recentGames = 5

// ClubOutlook assembles the league outlook for one club (IM56/IM57). It is a
// pure read. ErrNoSeason when the league has no active season;
// ErrClubNotFound when the club is not in its table.
func (s *Service) ClubOutlook(ctx context.Context, worldID, leagueID, clubID uuid.UUID) (*Outlook, error) {
	standings, err := s.GetStandings(ctx, leagueID, worldID)
	if err != nil {
		return nil, err
	}
	us := -1
	table := make([]olClub, len(standings.Rows))
	index := make(map[uuid.UUID]int, len(standings.Rows))
	for i, r := range standings.Rows {
		table[i] = olClub{id: r.Club.ID, name: r.Club.Name, pts: r.Points, gf: r.GoalsFor, ga: r.GoalsAgainst}
		index[r.Club.ID] = i
		if r.Club.ID == clubID {
			us = i
		}
	}
	if us < 0 {
		return nil, ErrClubNotFound
	}

	var start time.Time
	var promotions, relegations int
	err = s.pool.QueryRow(ctx, `
		SELECT s.start_date, COALESCE(r.promotions, 0), COALESCE(r.relegations, 0)
		FROM competition.seasons s
		LEFT JOIN competition.competition_rules r ON r.competition_id = s.competition_id
		WHERE s.id = $1`, standings.Season.ID).Scan(&start, &promotions, &relegations)
	if err != nil {
		return nil, fmt.Errorf("outlook season rules: %w", err)
	}

	all, err := s.GetFixtures(ctx, leagueID, worldID, nil)
	if err != nil {
		return nil, err
	}
	fixtures := fixturesInWindow(all, start, nil)

	out := &Outlook{
		Season:   standings.Season,
		Club:     standings.Rows[us].Club,
		Position: us + 1,
		Points:   table[us].pts,
	}
	remaining, rates, next, completed := splitFixtures(fixtures, index, table, clubID)
	out.GamesLeft = table[us].left

	out.Finish = finishRange(table, us)
	out.Races = leagueRaces(table, us, out.Finish, promotions, relegations)
	if out.Attachments, err = s.attachmentRaces(ctx, leagueID, table, us, out.Finish); err != nil {
		return nil, err
	}
	out.Stakes = pickStakes(out.Races, out.Attachments, out.Position)
	out.GuaranteedAtLeast = guaranteedAtLeast(out.Attachments, out.Finish)

	if next != nil {
		opp, ok := index[opponentOf(*next, clubID)]
		if !ok {
			opp = -1
		}
		w, d, l := swingPositions(table, us, opp)
		out.NextMatch = &NextMatchSwing{Fixture: *next, IfWin: w, IfDraw: d, IfLoss: l}
	}

	seed1 := binary.BigEndian.Uint64(standings.Season.ID[:8])
	pos, rng := projectFinish(table, us, remaining, rates, seed1, uint64(len(fixtures)-len(remaining)))
	out.Projection = Projection{Position: pos, Range: rng, Runs: projectionRuns}
	if len(remaining) == 0 {
		out.Projection.Runs = 0
	}

	in := factorInputs{clubs: len(table), inTop6: us < 6, remaining: table[us].left}
	for _, f := range remaining {
		if f.home == us && f.away < 6 || f.away == us && f.home < 6 {
			in.remainingTop6++
		}
	}
	in.leagueHomePPG = leagueHomePPG(rates)
	ourTrends(completed, clubID, &in)
	if err := s.xgTrend(ctx, completed, clubID, &in); err != nil {
		return nil, err
	}
	onDate := time.Now().UTC()
	if next != nil {
		onDate = next.ScheduledAt
	}
	players, err := squad.NewStore(s.pool).LoadSquad(ctx, clubID, onDate)
	if err != nil {
		return nil, fmt.Errorf("outlook squad: %w", err)
	}
	in.xiUnavailable = bestXIUnavailable(players)
	out.Projection.Factors = projectionFactors(in)
	return out, nil
}

// splitFixtures walks the season's league fixtures once: it counts each
// club's games left, collects unplayed fixtures for the projection, tallies
// home/away points per club, finds our next fixture, and returns our
// completed fixtures in play order.
func splitFixtures(fixtures []Fixture, index map[uuid.UUID]int, table []olClub, clubID uuid.UUID) (
	remaining []olFixture, rates []olRates, next *Fixture, completed []Fixture) {
	rates = make([]olRates, len(table))
	for _, f := range fixtures {
		h, okH := index[f.HomeClub.ID]
		a, okA := index[f.AwayClub.ID]
		if !okH || !okA {
			continue
		}
		if f.Status != "completed" || f.HomeScore == nil || f.AwayScore == nil {
			table[h].left++
			table[a].left++
			remaining = append(remaining, olFixture{home: h, away: a})
			if next == nil && (f.HomeClub.ID == clubID || f.AwayClub.ID == clubID) {
				n := f
				next = &n
			}
			continue
		}
		hp, ap := resultPoints(*f.HomeScore, *f.AwayScore)
		rates[h].homePts += hp
		rates[h].homeG++
		rates[a].awayPts += ap
		rates[a].awayG++
		if f.HomeClub.ID == clubID || f.AwayClub.ID == clubID {
			completed = append(completed, f)
		}
	}
	return remaining, rates, next, completed
}

func leagueHomePPG(rates []olRates) float64 {
	pts, g := 0, 0
	for _, r := range rates {
		pts += r.homePts
		g += r.homeG
	}
	if g == 0 {
		return projectionDefaultPPG
	}
	return float64(pts) / float64(g)
}

// ourTrends fills home form and the goal-difference trend from our completed
// league fixtures (play order).
func ourTrends(completed []Fixture, clubID uuid.UUID, in *factorInputs) {
	seasonGD := 0
	for i, f := range completed {
		gd := *f.HomeScore - *f.AwayScore
		if f.AwayClub.ID == clubID {
			gd = -gd
		} else {
			switch {
			case gd > 0:
				in.homeW++
			case gd == 0:
				in.homeD++
			default:
				in.homeL++
			}
		}
		seasonGD += gd
		if i >= len(completed)-recentGames {
			in.recentGD += gd
			in.recentGames++
		}
	}
	if len(completed) > 0 {
		in.seasonGDPerG = float64(seasonGD) / float64(len(completed))
	}
}

// xgTrend fills the chance-quality factor from the persisted xG of our
// completed league matches (IM58). Matches without xG are skipped; with none
// the factor is omitted.
func (s *Service) xgTrend(ctx context.Context, completed []Fixture, clubID uuid.UUID, in *factorInputs) error {
	if len(completed) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(completed))
	for i, f := range completed {
		ids[i] = f.ID
	}
	rows, err := s.pool.Query(ctx, `
		SELECT fixture_id, home_xg::float8, away_xg::float8
		FROM match.matches
		WHERE fixture_id = ANY($1) AND status = 'completed'
		  AND home_xg IS NOT NULL AND away_xg IS NOT NULL`, ids)
	if err != nil {
		return fmt.Errorf("outlook xg: %w", err)
	}
	defer rows.Close()
	xgd := map[uuid.UUID]float64{}
	for rows.Next() {
		var id uuid.UUID
		var h, a float64
		if err := rows.Scan(&id, &h, &a); err != nil {
			return fmt.Errorf("scan outlook xg: %w", err)
		}
		xgd[id] = h - a
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var withXG []float64
	for _, f := range completed {
		d, ok := xgd[f.ID]
		if !ok {
			continue
		}
		if f.AwayClub.ID == clubID {
			d = -d
		}
		withXG = append(withXG, d)
	}
	if len(withXG) == 0 {
		return nil
	}
	total := 0.0
	for i, d := range withXG {
		total += d
		if i >= len(withXG)-recentGames {
			in.recentXGD += d
			in.recentXGGames++
		}
	}
	in.seasonXGDPerG = total / float64(len(withXG))
	return nil
}

// bestXIUnavailable counts how many of the club's eleven highest-rated
// players cannot play on the squad's date (injury, suspension, …).
func bestXIUnavailable(players []squad.LoadedPlayer) int {
	sorted := append([]squad.LoadedPlayer(nil), players...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return squad.PositionalOverall(sorted[i].Position, sorted[i].Attributes) >
			squad.PositionalOverall(sorted[j].Position, sorted[j].Attributes)
	})
	if len(sorted) > 11 {
		sorted = sorted[:11]
	}
	n := 0
	for _, p := range sorted {
		if !p.Available {
			n++
		}
	}
	return n
}

// attachmentRaces turns every cup_qualification band on this league (every
// scope: domestic, regional, continental, …) into a qualification race.
func (s *Service) attachmentRaces(ctx context.Context, leagueID uuid.UUID, table []olClub, us int, f FinishRange) ([]Race, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.competition_type, q.from_position, q.to_position
		FROM competition.cup_qualification q
		JOIN competition.competitions c ON c.id = q.cup_id
		WHERE q.league_id = $1
		ORDER BY q.from_position, c.name`, leagueID)
	if err != nil {
		return nil, fmt.Errorf("outlook attachments: %w", err)
	}
	defer rows.Close()
	out := []Race{}
	for rows.Next() {
		var cup CupRef
		var from int
		var to *int
		if err := rows.Scan(&cup.ID, &cup.Name, &cup.Scope, &from, &to); err != nil {
			return nil, fmt.Errorf("scan outlook attachment: %w", err)
		}
		end := len(table)
		if to != nil && *to < end {
			end = *to
		}
		if from > end {
			continue // band starts below the bottom of this table
		}
		r := buildRace(RaceQualification, table, us, f, from, end)
		r.Cup = &cup
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
