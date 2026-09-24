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

// ClubCompetitionItem is one competition the caller's club belongs to per
// competition.club_competitions (OPD-01/OPD-20), with a rich manager-facing
// dossier instead of a bare membership row. Exactly one of League or Cup is
// set, discriminated by CompetitionType ("league" | "domestic_cup"); a club
// holds at most one league (schema-enforced) and up to cupMaxMemberships cups.
type ClubCompetitionItem struct {
	CompetitionType string          `json:"competition_type"`
	Role            string          `json:"role"`
	JoinedAt        time.Time       `json:"joined_at"`
	League          *ClubLeagueView `json:"league,omitempty"`
	Cup             *ClubCupView    `json:"cup,omitempty"`
}

// ClubLeagueView is the league dossier: the full League base, the current
// (latest non-completed) season with its started flag, that season's table,
// and the club's next scheduled fixture.
type ClubLeagueView struct {
	Competition League            `json:"competition"`
	Season      *apiref.SeasonRef `json:"season,omitempty"`
	Started     bool              `json:"started"`
	Standings   *StandingRowSet   `json:"standings,omitempty"`
	NextFixture *Fixture          `json:"next_fixture,omitempty"`
}

// ClubCupView is the cup dossier: the full Cup base, the campaign season, the
// club's stage and current round in the bracket, its next fixture in the cup,
// and the champion once decided. Stage is one of:
//
//	not_started  — no campaign season yet (late-phase cups between campaigns);
//	playing      — the club has a fixture scheduled in the current round;
//	waiting      — still alive, but no fixture yet (bye / round unmaterialized);
//	eliminated   — lost a tie in the campaign.
type ClubCupView struct {
	Competition  Cup               `json:"competition"`
	Season       *apiref.SeasonRef `json:"season,omitempty"`
	Started      bool              `json:"started"`
	Stage        string            `json:"stage"`
	CurrentRound int               `json:"current_round,omitempty"`
	TotalRounds  int               `json:"total_rounds,omitempty"`
	NextFixture  *Fixture          `json:"next_fixture,omitempty"`
	Champion     *apiref.ClubRef   `json:"champion,omitempty"`
}

// MyClubCompetitions returns every competition the club belongs to, each with
// its rich manager dossier — league table for the (single) league, current
// round + next fixture for each cup. The list is empty when the club has no
// memberships. The caller resolves the manager's own club first; an unemployed
// manager short-circuits before this method.
func (s *Service) MyClubCompetitions(ctx context.Context, worldID, clubID uuid.UUID) ([]ClubCompetitionItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cc.role, cc.joined_at,
		       cm.competition_type, cm.id, cm.name, cm.status, cm.tier, cm.team_count, cm.prize_pool,
		       wc.id, wc.name, wc.code,
		       r.format, r.is_home_and_away,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'teams', '0')::int,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'enter_when_survivors', '0')::int,
		       COALESCE(r.qualification_rules->'campaign'->>'total_rounds', '0')::int,
		       s.id, s.season_label, s.season_number, s.status
		FROM competition.club_competitions cc
		JOIN competition.competitions cm ON cm.id = cc.competition_id
		JOIN world.countries wc ON wc.id = cm.country_id
		JOIN competition.competition_rules r ON r.competition_id = cm.id
		LEFT JOIN LATERAL (
			SELECT id, season_label, season_number, status
			FROM competition.seasons
			WHERE competition_id = cm.id AND status <> 'completed'
			ORDER BY season_number DESC
			LIMIT 1
		) s ON TRUE
		WHERE cc.world_id = $1 AND cc.club_id = $2
		ORDER BY (cm.competition_type = 'league') DESC, cm.name`, worldID, clubID)
	if err != nil {
		return nil, fmt.Errorf("club memberships: %w", err)
	}
	defer rows.Close()

	out := []ClubCompetitionItem{}
	for rows.Next() {
		var (
			item              ClubCompetitionItem
			compID            uuid.UUID
			compName, status  string
			countryID         uuid.UUID
			countryName       string
			countryCode       string
			format            string
			isHomeAndAway     bool
			tier, teamCount   *int
			prizePool         float64
			firstTierBye      int
			survivorThreshold int
			totalRounds       int
			seasonID          *uuid.UUID
			seasonLabel       string
			seasonNumber      int
			seasonStatus      string
		)
		if err := rows.Scan(&item.Role, &item.JoinedAt,
			&item.CompetitionType, &compID, &compName, &status, &tier, &teamCount, &prizePool,
			&countryID, &countryName, &countryCode,
			&format, &isHomeAndAway,
			&firstTierBye, &survivorThreshold, &totalRounds,
			&seasonID, &seasonLabel, &seasonNumber, &seasonStatus); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}

		switch item.CompetitionType {
		case "league":
			view := &ClubLeagueView{Competition: League{ID: compID}}
			if seasonID != nil {
				view.Season = &apiref.SeasonRef{ID: *seasonID, Label: seasonLabel, Number: seasonNumber, Status: seasonStatus}
				view.Started = true
			}
			item.League = view
		case "domestic_cup":
			view := &ClubCupView{
				Competition: Cup{
					ID: compID, WorldID: worldID, Name: compName, CompetitionType: item.CompetitionType,
					Status: status, PrizePool: prizePool,
					Country:           apiref.CountryRef{ID: countryID, Name: countryName, Code: countryCode},
					Format:            format,
					IsHomeAndAway:     isHomeAndAway,
					FirstTierBye:      firstTierBye,
					SurvivorThreshold: survivorThreshold,
				},
				TotalRounds: totalRounds,
			}
			if seasonID != nil {
				view.Season = &apiref.SeasonRef{ID: *seasonID, Label: seasonLabel, Number: seasonNumber, Status: seasonStatus}
				view.Started = true
				view.Stage = "waiting"
			} else {
				view.Stage = "not_started"
			}
			item.Cup = view
		default:
			continue
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memberships rows: %w", err)
	}

	// League dossier: the full League base (adjacency links included) and the
	// active season's table. No current season means the league has not been
	// started — the standings stay nil and Started stays false.
	for i := range out {
		item := &out[i]
		if item.League == nil {
			continue
		}
		league, err := s.getLeague(ctx, s.pool, item.League.Competition.ID)
		if err != nil {
			return nil, fmt.Errorf("league base: %w", err)
		}
		item.League.Competition = *league
		standings, err := s.GetStandings(ctx, league.ID, worldID)
		if errors.Is(err, ErrNoSeason) {
			standings = nil
		} else if err != nil {
			return nil, fmt.Errorf("league standings: %w", err)
		}
		item.League.Standings = standings
	}

	// Cup dossier: entries + bracket round + champion per cup season.
	for i := range out {
		item := &out[i]
		if item.Cup == nil || item.Cup.Season == nil {
			continue
		}
		entryStatus, maxRound, champion, err := s.cupClubState(ctx, item.Cup.Season.ID, clubID)
		if err != nil {
			return nil, fmt.Errorf("cup club state: %w", err)
		}
		item.Cup.Champion = champion
		if maxRound != nil {
			item.Cup.CurrentRound = *maxRound
		}
		if entryStatus == "eliminated" {
			item.Cup.Stage = "eliminated"
		}
	}

	// Upcoming fixtures in one query, partitioned per competition: the league's
	// next fixture and each cup's next tie (knockout matchday == round number).
	fixtures, err := s.clubUpcomingFixtures(ctx, worldID, clubID)
	if err != nil {
		return nil, err
	}
	nextByCompetition := map[uuid.UUID]*Fixture{}
	for i := range fixtures {
		if _, ok := nextByCompetition[fixtures[i].Competition.ID]; !ok {
			nextByCompetition[fixtures[i].Competition.ID] = &fixtures[i]
		}
	}
	for i := range out {
		switch {
		case out[i].League != nil:
			out[i].League.NextFixture = nextByCompetition[out[i].League.Competition.ID]
		case out[i].Cup != nil:
			if f := nextByCompetition[out[i].Cup.Competition.ID]; f != nil {
				out[i].Cup.NextFixture = f
				out[i].Cup.CurrentRound = f.Matchday
				out[i].Cup.Stage = "playing"
			}
		}
	}

	return out, nil
}

// clubUpcomingFixtures lists a club's non-completed fixtures across its
// competitions (league + cups), ordered by round/matchday then kickoff.
func (s *Service) clubUpcomingFixtures(ctx context.Context, worldID, clubID uuid.UUID) ([]Fixture, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.world_id = $1 AND (f.home_club_id = $2 OR f.away_club_id = $2)
		  AND f.status NOT IN ('completed', 'cancelled')
		ORDER BY f.matchday, f.scheduled_at, f.home_club_id`, worldID, clubID)
	if err != nil {
		return nil, fmt.Errorf("club fixtures: %w", err)
	}
	return scanFixtures(rows)
}

// cupClubState reads a club's campaign state inside one cup season: its entry
// status, the highest bracket round it is seeded into, and the champion once
// decided. A club that never entered (defensive) yields zero values.
func (s *Service) cupClubState(ctx context.Context, seasonID, clubID uuid.UUID) (entryStatus string, maxRound *int, champion *apiref.ClubRef, err error) {
	var champID *uuid.UUID
	var champName, champShort *string
	err = s.pool.QueryRow(ctx, `
		SELECT e.status,
		       (SELECT MAX(cb.round) FROM competition.cup_bracket cb
		         WHERE cb.season_id = e.season_id AND cb.club_id = e.club_id),
		       ch.club_id, ch.name, ch.short
		FROM competition.competition_entries e
		LEFT JOIN LATERAL (
			SELECT ed.club_id, cl.name, COALESCE(cl.short_name, '') AS short
			FROM competition.competition_entries ed
			JOIN club.clubs cl ON cl.id = ed.club_id
			WHERE ed.season_id = e.season_id AND ed.status = 'champion'
			LIMIT 1
		) ch ON TRUE
		WHERE e.season_id = $1 AND e.club_id = $2`, seasonID, clubID).
		Scan(&entryStatus, &maxRound, &champID, &champName, &champShort)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil, nil
	}
	if err != nil {
		return "", nil, nil, err
	}
	if champID != nil {
		name, short := "", ""
		if champName != nil {
			name = *champName
		}
		if champShort != nil {
			short = *champShort
		}
		champion = &apiref.ClubRef{ID: *champID, Name: name, Short: short}
	}
	return entryStatus, maxRound, champion, nil
}
