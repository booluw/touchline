// Package scout owns opponent scouting read-models: manager-facing dossiers
// drawn from persisted state (club identity, reputation, league position,
// rolling form, squad headline) with the pre-match context of one fixture.
// It is deliberately a dedicated package (not part of internal/competition)
// so later scout features — injury/tactics briefings, full-season scouting —
// can grow here without touching competition logic.
package scout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
)

// ManagerRef names the opponent's current manager without account data.
type ManagerRef struct {
	ID          uuid.UUID `json:"id"`
	IsPolicyBot bool      `json:"is_policy_bot"`
}

// FormSummary is the opponent's rolling-form read-model: the trailing-5
// results string (oldest first, '-' padded) and the persisted EWMA rating.
type FormSummary struct {
	FormString    string  `json:"form_string"`
	CurrentRating float64 `json:"current_rating"`
}

// KeyPlayer is one headline opponent player for a scouting brief.
type KeyPlayer struct {
	ID              uuid.UUID `json:"id"`
	PersonID        uuid.UUID `json:"person_id"`
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	DisplayName     string    `json:"display_name"`
	PrimaryPosition string    `json:"primary_position"`
	Rating          int       `json:"rating"`
}

// Report is an opponent dossier served to the manager who is about to face
// them. LeaguePosition is nil when the opponent has no current league table
// position (off-season, no league, or ahead of their first result).
type Report struct {
	Club           apiref.ClubRef     `json:"club"`
	Country        *apiref.CountryRef `json:"country,omitempty"`
	IsAIControlled bool               `json:"is_ai_controlled"`
	Manager        *ManagerRef        `json:"manager,omitempty"`
	Reputation     int                `json:"reputation"`
	Tier           int                `json:"tier"`
	LeaguePosition *int               `json:"league_position,omitempty"`
	Form           FormSummary        `json:"form"`
	SquadCount     int                `json:"squad_count"`
	TopPlayers     []KeyPlayer        `json:"top_players"`
}

// NextFixtureView is GET /api/clubs/:id/next-fixture's payload: the club's
// next match (real-world kickoff in scheduled_at, plus its gameweek) and the
// opponent dossier with the matchup context a manager needs to prepare.
type NextFixtureView struct {
	Fixture        *competition.Fixture `json:"fixture"`
	Gameweek       int                  `json:"gameweek"`
	HomeOrAway     string               `json:"home_or_away"`
	Derby          bool                 `json:"derby"`
	DerbyIntensity int                  `json:"derby_intensity,omitempty"`
	GoldenGoal     bool                 `json:"golden_goal"`
	Opponent       *Report              `json:"opponent,omitempty"`
}

// Service builds scouting read-models. It reads persisted state only.
type Service struct {
	pool *pgxpool.Pool
	comp *competition.Service
	form *form.Store
}

// NewService builds the scouting service. Fixture reads are delegated to the
// competition service so scouting stays a read-model over the game layer.
func NewService(pool *pgxpool.Pool, comp *competition.Service) *Service {
	return &Service{pool: pool, comp: comp, form: form.NewStore(pool)}
}

// NextFixture resolves the club's upcoming fixture plus its opponent dossier.
// A nil view (nil, nil) means the club has no upcoming fixture (off-season).
// World/ownership errors surface exactly like the club fixture reads:
// competition.ErrClubNotFound / ErrClubWorldMismatch.
func (s *Service) NextFixture(ctx context.Context, worldID, clubID uuid.UUID) (*NextFixtureView, error) {
	f, err := s.comp.NextClubFixture(ctx, worldID, clubID)
	if err != nil || f == nil {
		return nil, err
	}
	homeOrAway := "away"
	opponentID := f.HomeClub.ID
	if f.HomeClub.ID == clubID {
		homeOrAway = "home"
		opponentID = f.AwayClub.ID
	}
	report, err := s.Report(ctx, worldID, opponentID)
	if err != nil {
		return nil, err
	}
	intensity, goldenGoal, err := s.matchup(ctx, f)
	if err != nil {
		return nil, err
	}
	return &NextFixtureView{
		Fixture:        f,
		Gameweek:       f.Matchday,
		HomeOrAway:     homeOrAway,
		Derby:          intensity >= squad.RivalryIntensityThreshold,
		DerbyIntensity: intensity,
		GoldenGoal:     goldenGoal,
		Opponent:       report,
	}, nil
}

// Report builds an opponent dossier from persisted state only.
func (s *Service) Report(ctx context.Context, worldID, clubID uuid.UUID) (*Report, error) {
	r := &Report{TopPlayers: []KeyPlayer{}}
	var (
		countryName string
		countryID   *uuid.UUID
		countryCode *string
		mgrID       *uuid.UUID
		isBot       *bool
	)
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.name, COALESCE(c.short_name, ''), c.country, c.is_ai_controlled,
		       c.reputation, c.tier, c.current_manager_id, m.is_policy_bot,
		       wc.id, wc.code
		FROM club.clubs c
		LEFT JOIN manager.managers m ON m.id = c.current_manager_id
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND lower(wc.name) = lower(c.country)
		WHERE c.id = $1 AND c.world_id = $2`, clubID, worldID).
		Scan(&r.Club.ID, &r.Club.Name, &r.Club.Short, &countryName, &r.IsAIControlled,
			&r.Reputation, &r.Tier, &mgrID, &isBot, &countryID, &countryCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, competition.ErrClubNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("opponent identity: %w", err)
	}
	if countryID != nil {
		code := ""
		if countryCode != nil {
			code = *countryCode
		}
		r.Country = &apiref.CountryRef{ID: *countryID, Name: countryName, Code: code}
	} else {
		r.Country = &apiref.CountryRef{Name: countryName}
	}
	if mgrID != nil {
		m := &ManagerRef{ID: *mgrID}
		if isBot != nil {
			m.IsPolicyBot = *isBot
		}
		r.Manager = m
	}

	fs, ok, err := s.form.Get(ctx, clubID)
	if err != nil {
		return nil, fmt.Errorf("opponent form: %w", err)
	}
	if !ok {
		fs = form.Neutral(clubID, 0)
	}
	r.Form = FormSummary{FormString: fs.FormString, CurrentRating: fs.CurrentRating}

	pos, err := s.leaguePosition(ctx, worldID, clubID)
	if err != nil {
		return nil, err
	}
	r.LeaguePosition = pos

	count, top, err := s.squadScouting(ctx, clubID)
	if err != nil {
		return nil, err
	}
	r.SquadCount = count
	r.TopPlayers = top
	return r, nil
}

// leaguePosition finds the opponent's current league-table position through
// their single league membership (schema guarantees one league per club).
// Nil when the club is in no league, off-season, or ahead of its first result.
func (s *Service) leaguePosition(ctx context.Context, worldID, clubID uuid.UUID) (*int, error) {
	var leagueID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT c.id
		FROM competition.competitions c
		JOIN competition.club_competitions cc ON cc.competition_id = c.id
			AND cc.club_id = $1 AND cc.role = 'league'
		WHERE c.world_id = $2 AND c.competition_type = 'league'
		LIMIT 1`, clubID, worldID).Scan(&leagueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opponent league: %w", err)
	}
	st, err := s.comp.GetStandings(ctx, leagueID, worldID)
	if errors.Is(err, competition.ErrNoSeason) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opponent standings: %w", err)
	}
	for i := range st.Rows {
		if st.Rows[i].Club.ID == clubID {
			p := i + 1
			return &p, nil
		}
	}
	return nil, nil
}

// matchup reads the fixture's competition format (golden goal on knockout
// ties) and the rivalry intensity between the two clubs.
func (s *Service) matchup(ctx context.Context, f *competition.Fixture) (intensity int, goldenGoal bool, err error) {
	var format string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(r.format, '')
		FROM competition.competitions c
		LEFT JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.id = $1`, f.Competition.ID).Scan(&format); err != nil {
		return 0, false, fmt.Errorf("fixture format: %w", err)
	}
	goldenGoal = format == "knockout"
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(intensity), 0)
		FROM club.rivalries
		WHERE (club_a_id = $1 AND club_b_id = $2) OR (club_a_id = $2 AND club_b_id = $1)`,
		f.HomeClub.ID, f.AwayClub.ID).Scan(&intensity); err != nil {
		return 0, false, fmt.Errorf("rivalry intensity: %w", err)
	}
	return intensity, goldenGoal, nil
}

// squadScouting returns the opponent's squad size and their top players by
// position-weighted overall rating (three headline names for the brief).
func (s *Service) squadScouting(ctx context.Context, clubID uuid.UUID) (int, []KeyPlayer, error) {
	var count int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM player.players WHERE club_id = $1`, clubID).Scan(&count); err != nil {
		return 0, nil, fmt.Errorf("squad count: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.person_id, pe.first_name, pe.last_name, pe.display_name, p.primary_position
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		WHERE p.club_id = $1`, clubID)
	if err != nil {
		return 0, nil, fmt.Errorf("squad players: %w", err)
	}
	defer rows.Close()

	var members []KeyPlayer
	for rows.Next() {
		var kp KeyPlayer
		if err := rows.Scan(&kp.ID, &kp.PersonID, &kp.FirstName, &kp.LastName,
			&kp.DisplayName, &kp.PrimaryPosition); err != nil {
			return 0, nil, fmt.Errorf("scan squad player: %w", err)
		}
		members = append(members, kp)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	sums := map[uuid.UUID]map[string]int{}
	counts := map[uuid.UUID]map[string]int{}
	aRows, err := s.pool.Query(ctx, `
		SELECT a.player_id, a.attribute_category, SUM(a.value)::int, COUNT(a.value)::int
		FROM player.player_attributes a
		JOIN player.players p ON p.id = a.player_id AND p.club_id = $1
		GROUP BY a.player_id, a.attribute_category`, clubID)
	if err != nil {
		return 0, nil, fmt.Errorf("squad attributes: %w", err)
	}
	defer aRows.Close()
	for aRows.Next() {
		var (
			pid uuid.UUID
			cat string
			sum int
			cnt int
		)
		if err := aRows.Scan(&pid, &cat, &sum, &cnt); err != nil {
			return 0, nil, fmt.Errorf("scan attribute row: %w", err)
		}
		if sums[pid] == nil {
			sums[pid] = map[string]int{}
			counts[pid] = map[string]int{}
		}
		sums[pid][cat] = sum
		counts[pid][cat] = cnt
	}
	if err := aRows.Err(); err != nil {
		return 0, nil, err
	}

	for i := range members {
		members[i].Rating = s.overall(sums[members[i].ID], counts[members[i].ID], members[i].PrimaryPosition)
	}
	sort.Slice(members, func(a, b int) bool {
		if members[a].Rating != members[b].Rating {
			return members[a].Rating > members[b].Rating
		}
		return members[a].DisplayName < members[b].DisplayName
	})
	if len(members) > 3 {
		members = members[:3]
	}
	return count, members, nil
}

// overall is the position-weighted blended rating across the attribute EAV
// (squad.PlayerOverall arithmetic over category means), 0 when the player has
// no recorded attributes.
func (s *Service) overall(sum, cnt map[string]int, position string) int {
	if cnt == nil {
		return 0
	}
	mean := func(cat string) int {
		n := cnt[cat]
		if n <= 0 {
			return 0
		}
		return int(math.Round(float64(sum[cat]) / float64(n)))
	}
	snap := squad.AttributeSnapshot{
		Technical:   mean("technical"),
		Physical:    mean("physical"),
		Mental:      mean("mental"),
		Tactical:    mean("tactical"),
		Goalkeeping: mean("goalkeeping"),
		Positional:  mean("positional"),
	}
	return squad.PlayerOverall(snap, position)
}
