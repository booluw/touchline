package competition

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
)

// CompetitionDetail is the admin read-model for one competition (league or
// cup). League and Cup are mutually exclusive, discriminated by
// CompetitionType, mirroring the ClubCompetitionItem club dossier. It bundles
// the base identity (scope, tier, size), the past-winners and top-scorers
// history, and the type-specific dossier (the league table plus movement, or
// the cup bracket plus per-entrant progress).
type CompetitionDetail struct {
	ID              uuid.UUID          `json:"id"`
	WorldID         uuid.UUID          `json:"world_id"`
	Name            string             `json:"name"`
	CompetitionType string             `json:"competition_type"`
	Status          string             `json:"status"`
	Country         *apiref.CountryRef `json:"country,omitempty"`
	Region          *RegionRef         `json:"region,omitempty"`
	Tier            *int               `json:"tier,omitempty"`
	TeamCount       int                `json:"team_count"`
	Reputation      int                `json:"reputation,omitempty"`
	SeasonsTotal    int                `json:"seasons_total"`
	PastWinners     []PastWinner       `json:"past_winners"`
	TopScorers      ScorerList         `json:"top_scorers"`
	League          *LeagueDetail      `json:"league,omitempty"`
	Cup             *CupDetail         `json:"cup,omitempty"`
}

// PastWinner is one completed season's champion. For leagues the champion is
// the standings leader of that season; for cups it is the competition_entries
// champion record of the completed campaign.
type PastWinner struct {
	Season   apiref.SeasonRef `json:"season"`
	Champion apiref.ClubRef   `json:"champion"`
}

// ScorerList is the top-goalscorer history: the current/latest season's table
// and the all-time table for the competition. Goals counts penalties scored
// in; Penalties is the subset so clients can split the two.
type ScorerList struct {
	CurrentSeason []TopScorer `json:"current_season"`
	AllTime       []TopScorer `json:"all_time"`
}

// TopScorer is one rank in a top-scorers table. Club is the player's current
// club (nil for free agents).
type TopScorer struct {
	Rank      int              `json:"rank"`
	Player    apiref.PlayerRef `json:"player"`
	Club      *apiref.ClubRef  `json:"club,omitempty"`
	Goals     int              `json:"goals"`
	Penalties int              `json:"penalties"`
}

// StreakInfo is a club's leading result streak: the consecutive-run outcome
// ("W", "D", or "L") and its length in fixtures. Length 0 means no completed
// fixtures yet for the season.
type StreakInfo struct {
	Outcome string `json:"outcome"`
	Length  int    `json:"length"`
}

// LeagueDetail is the league half of a CompetitionDetail: the season archive,
// the current/latest season's fixture progress, its table enriched with each
// club's country, result streak and next fixture, and the latest season's
// membership movement (promoted in / relegated out).
type LeagueDetail struct {
	Season    *SeasonDetail      `json:"season,omitempty"`
	Seasons   []apiref.SeasonRef `json:"seasons"`
	Standings []StandingRowExt   `json:"standings"`
	Movement  LeagueMovement     `json:"movement"`
}

// SeasonDetail is the current (latest) season with its fixture progress.
type SeasonDetail struct {
	apiref.SeasonRef
	FixturesPlayed int `json:"fixtures_played"`
	FixturesTotal  int `json:"fixtures_total"`
}

// StandingRowExt is a league-table line enriched for the admin dossier.
type StandingRowExt struct {
	StandingRow
	Country     apiref.CountryRef `json:"country"`
	Streak      StreakInfo        `json:"streak"`
	NextFixture *Fixture          `json:"next_fixture,omitempty"`
}

// LeagueMovement is the current season's churn relative to its immediate
// predecessor: clubs that joined (promoted up or relegated down into the
// league) and clubs that departed.
type LeagueMovement struct {
	PromotedIn   []apiref.ClubRef `json:"promoted_in"`
	RelegatedOut []apiref.ClubRef `json:"relegated_out"`
}

// CupDetail is the cup half of a CompetitionDetail: the latest campaign
// season, the materialized bracket with ties and byes, the champion once
// decided, and per-entrant progress (round reached, elimination, streak,
// next tie).
type CupDetail struct {
	Season         *apiref.SeasonRef `json:"season,omitempty"`
	LateEntryRound int               `json:"late_entry_round"`
	TotalRounds    int               `json:"total_rounds"`
	Champion       *apiref.ClubRef   `json:"champion,omitempty"`
	Rounds         []CupRound        `json:"rounds"`
	Clubs          []CupClubRow      `json:"clubs"`
}

// CupClubRow is one cup entrant's progress in the current campaign.
type CupClubRow struct {
	Club         apiref.ClubRef    `json:"club"`
	Country      apiref.CountryRef `json:"country"`
	Eliminated   bool              `json:"eliminated"`
	CurrentRound int               `json:"current_round,omitempty"`
	Streak       StreakInfo        `json:"streak"`
	NextFixture  *Fixture          `json:"next_fixture,omitempty"`
}

// CompetitionDetail assembles the full admin dossier for one competition.
// Unlike the manager-facing gets (GetLeague/GetCup are world-scoped) this is
// a global admin read; competition_id alone drives every query.
func (s *Service) CompetitionDetail(ctx context.Context, competitionID uuid.UUID) (*CompetitionDetail, error) {
	base, err := s.detailBase(ctx, competitionID)
	if err != nil {
		return nil, err
	}

	seasons, err := s.detailSeasons(ctx, competitionID)
	if err != nil {
		return nil, err
	}

	out := &CompetitionDetail{
		ID:              base.ID,
		WorldID:         base.WorldID,
		Name:            base.Name,
		CompetitionType: base.CompetitionType,
		Status:          base.Status,
		Country:         base.countryRef(),
		Region:          base.regionRef(),
		Tier:            base.Tier,
		TeamCount:       clamp(base.TeamCount),
		SeasonsTotal:    len(seasons),
	}

	// Top-scorer history: all-time, plus the current/latest season's window.
	out.TopScorers.AllTime, err = s.topScorers(ctx, competitionID, nil, nil)
	if err != nil {
		return nil, err
	}
	if out.SeasonsTotal > 0 {
		cur := seasons[0]
		out.TopScorers.CurrentSeason, err = s.topScorers(ctx, competitionID, &cur.start, cur.end)
		if err != nil {
			return nil, err
		}
	}

	switch base.CompetitionType {
	case "league":
		out.Reputation = clamp(base.Reputation)
		out.PastWinners, err = s.pastWinnersLeague(ctx, competitionID)
		if err != nil {
			return nil, err
		}
		detail, err := s.leagueDetail(ctx, base, seasons)
		if err != nil {
			return nil, err
		}
		out.League = detail
	case "domestic_cup", "continental":
		out.PastWinners, err = s.pastWinnersCup(ctx, competitionID)
		if err != nil {
			return nil, err
		}
		detail, err := s.cupDetail(ctx, base, seasons)
		if err != nil {
			return nil, err
		}
		out.Cup = detail
	}
	return out, nil
}

// detailBase is the base competition row joined with its country/region scope.
type detailBase struct {
	ID              uuid.UUID
	WorldID         uuid.UUID
	CountryID       *uuid.UUID
	RegionID        *uuid.UUID
	Tier            *int
	TeamCount       int
	Reputation      int
	Name            string
	CompetitionType string
	Status          string
	countryName     string
	countryCode     string
	regionName      string
}

func (b *detailBase) countryRef() *apiref.CountryRef {
	if b.CountryID == nil {
		return nil
	}
	return &apiref.CountryRef{ID: *b.CountryID, Name: b.countryName, Code: b.countryCode}
}

func (b *detailBase) regionRef() *RegionRef {
	if b.RegionID == nil {
		return nil
	}
	return &RegionRef{ID: *b.RegionID, Name: b.regionName}
}

func (s *Service) detailBase(ctx context.Context, competitionID uuid.UUID) (*detailBase, error) {
	var b detailBase
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.region_id, c.tier, c.team_count, c.reputation,
		       c.name, c.competition_type, c.status,
		       COALESCE(wc.name, ''), COALESCE(wc.code, ''), COALESCE(wr.name, '')
		FROM competition.competitions c
		LEFT JOIN world.countries wc ON wc.id = c.country_id
		LEFT JOIN world.regions wr ON wr.id = c.region_id
		WHERE c.id = $1`, competitionID).
		Scan(&b.ID, &b.WorldID, &b.CountryID, &b.RegionID, &b.Tier, &b.TeamCount, &b.Reputation,
			&b.Name, &b.CompetitionType, &b.Status, &b.countryName, &b.countryCode, &b.regionName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompetitionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("detail base: %w", err)
	}
	return &b, nil
}

// detailSeason is a season row with its date window, newest first.
type detailSeason struct {
	ref   apiref.SeasonRef
	start time.Time
	end   *time.Time
}

func (s *Service) detailSeasons(ctx context.Context, competitionID uuid.UUID) ([]detailSeason, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, season_label, season_number, status, start_date, end_date
		FROM competition.seasons
		WHERE competition_id = $1
		ORDER BY season_number DESC`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("detail seasons: %w", err)
	}
	defer rows.Close()
	out := []detailSeason{}
	for rows.Next() {
		var d detailSeason
		if err := rows.Scan(&d.ref.ID, &d.ref.Label, &d.ref.Number, &d.ref.Status,
			&d.start, &d.end); err != nil {
			return nil, fmt.Errorf("scan detail season: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// topScorers returns the top 10 goalscorers for a competition, optionally
// windowed to a season's date range (start inclusive, end exclusive; nil end
// means unbounded). Goals includes penalties; penalties is the subset.
func (s *Service) topScorers(ctx context.Context, competitionID uuid.UUID,
	windowStart, windowEnd *time.Time) ([]TopScorer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, COALESCE(pp.first_name || COALESCE(' ' || pp.last_name, ''), ''),
		       p.club_id, COALESCE(c.name, ''), COALESCE(c.short_name, ''),
		       COUNT(*) FILTER (WHERE e.event_type = 'goal')::int,
		       COUNT(*) FILTER (WHERE e.event_type = 'penalty_scored')::int
		FROM match.match_events e
		JOIN match.matches m ON m.id = e.match_id
		JOIN match.fixtures f ON f.id = m.fixture_id
		JOIN player.players p ON p.id = e.player_id
		JOIN person.people pp ON pp.id = p.person_id
		LEFT JOIN club.clubs c ON c.id = p.club_id
		WHERE f.competition_id = $1 AND e.event_type IN ('goal', 'penalty_scored')
		  AND ($2::timestamptz IS NULL OR f.scheduled_at >= $2)
		  AND ($3::timestamptz IS NULL OR f.scheduled_at < $3)
		GROUP BY p.id, pp.first_name, pp.last_name, p.club_id, c.name, COALESCE(c.short_name, '')
		ORDER BY (COUNT(*) FILTER (WHERE e.event_type IN ('goal', 'penalty_scored')))::int DESC,
		         COUNT(*) FILTER (WHERE e.event_type = 'penalty_scored')::int DESC,
		         pp.last_name, pp.first_name
		LIMIT 10`, competitionID, windowStart, windowEnd)
	if err != nil {
		return nil, fmt.Errorf("top scorers: %w", err)
	}
	defer rows.Close()
	out := []TopScorer{}
	i := 0
	for rows.Next() {
		var (
			t         TopScorer
			clubID    *uuid.UUID
			clubName  string
			clubShort string
			openGoals int
		)
		if err := rows.Scan(&t.Player.ID, &t.Player.Name, &clubID, &clubName, &clubShort,
			&openGoals, &t.Penalties); err != nil {
			return nil, fmt.Errorf("scan top scorer: %w", err)
		}
		i++
		t.Rank = i
		t.Goals = openGoals + t.Penalties
		if clubID != nil {
			t.Club = &apiref.ClubRef{ID: *clubID, Name: clubName, Short: clubShort}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// pastWinnersLeague lists each completed season's champion, newest first. The
// champion is the standings leader using the same tie-breakers as GetStandings.
func (s *Service) pastWinnersLeague(ctx context.Context, competitionID uuid.UUID) ([]PastWinner, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (sn.id) sn.id, sn.season_label, sn.season_number, sn.status,
		       st.club_id, c.name, COALESCE(c.short_name, '')
		FROM competition.seasons sn
		JOIN competition.standings st ON st.season_id = sn.id
		JOIN club.clubs c ON c.id = st.club_id
		WHERE sn.competition_id = $1 AND sn.status = 'completed'
		ORDER BY sn.id, st.points DESC, (st.goals_for - st.goals_against) DESC,
		         st.goals_for DESC, c.name`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("league winners: %w", err)
	}
	defer rows.Close()
	out := []PastWinner{}
	for rows.Next() {
		var w PastWinner
		if err := rows.Scan(&w.Season.ID, &w.Season.Label, &w.Season.Number, &w.Season.Status,
			&w.Champion.ID, &w.Champion.Name, &w.Champion.Short); err != nil {
			return nil, fmt.Errorf("scan league winner: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Season.Number > out[b].Season.Number })
	return out, nil
}

// pastWinnersCup lists each completed campaign's champion, newest first.
func (s *Service) pastWinnersCup(ctx context.Context, competitionID uuid.UUID) ([]PastWinner, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sn.id, sn.season_label, sn.season_number, sn.status,
		       e.club_id, c.name, COALESCE(c.short_name, '')
		FROM competition.competition_entries e
		JOIN competition.seasons sn ON sn.id = e.season_id
		JOIN club.clubs c ON c.id = e.club_id
		WHERE sn.competition_id = $1 AND sn.status = 'completed' AND e.status = 'champion'
		ORDER BY sn.season_number DESC`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("cup winners: %w", err)
	}
	defer rows.Close()
	out := []PastWinner{}
	for rows.Next() {
		var w PastWinner
		if err := rows.Scan(&w.Season.ID, &w.Season.Label, &w.Season.Number, &w.Season.Status,
			&w.Champion.ID, &w.Champion.Name, &w.Champion.Short); err != nil {
			return nil, fmt.Errorf("scan cup winner: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// leagueDetail assembles the league dossier: season archive, the latest
// season's table enriched with country/streak/next fixture, and movement.
func (s *Service) leagueDetail(ctx context.Context, base *detailBase, seasons []detailSeason) (*LeagueDetail, error) {
	d := &LeagueDetail{
		Seasons:   []apiref.SeasonRef{},
		Standings: []StandingRowExt{},
		Movement:  LeagueMovement{},
	}
	for _, se := range seasons {
		d.Seasons = append(d.Seasons, se.ref)
	}
	if len(seasons) == 0 {
		return d, nil
	}
	cur := seasons[0]

	fixtures, err := s.GetFixtures(ctx, base.ID, base.WorldID, nil)
	if err != nil {
		return nil, err
	}
	window := fixturesInWindow(fixtures, cur.start, cur.end)
	played := 0
	for _, f := range window {
		if f.Status == "completed" {
			played++
		}
	}
	d.Season = &SeasonDetail{SeasonRef: cur.ref, FixturesPlayed: played, FixturesTotal: len(window)}

	standings, err := s.leagueStandings(ctx, cur.ref.ID)
	if err != nil {
		return nil, err
	}
	countryIDs := make([]uuid.UUID, 0, len(standings))
	for _, r := range standings {
		countryIDs = append(countryIDs, r.Club.ID)
	}
	countries, err := s.clubsCountries(ctx, countryIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range standings {
		ext := StandingRowExt{
			StandingRow: r,
			Country:     countries[r.Club.ID],
		}
		ext.Streak, ext.NextFixture = clubStreakAndNext(window, r.Club.ID)
		d.Standings = append(d.Standings, ext)
	}

	// Movement vs the immediately preceding completed season (membership diff:
	// latest season's entrants minus the predecessor's = joined; the converse
	// = departed). Robust to the shape of CLUB_PROMOTED/CLUB_RELEGATED events,
	// which are keyed by the source league's season, not this league's.
	prev := findPredecessor(seasons)
	if prev != nil {
		joined, departed, err := s.membershipDiff(ctx, cur.ref.ID, prev.ref.ID)
		if err != nil {
			return nil, err
		}
		d.Movement.PromotedIn, err = s.clubRefs(ctx, joined)
		if err != nil {
			return nil, err
		}
		d.Movement.RelegatedOut, err = s.clubRefs(ctx, departed)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

// leagueStandings loads one season's table with the GetStandings ordering.
func (s *Service) leagueStandings(ctx context.Context, seasonID uuid.UUID) ([]StandingRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, COALESCE(c.short_name, ''),
		       st.played, st.won, st.drawn, st.lost, st.goals_for, st.goals_against, st.points
		FROM competition.standings st
		JOIN club.clubs c ON c.id = st.club_id
		WHERE st.season_id = $1
		ORDER BY st.points DESC, (st.goals_for - st.goals_against) DESC, st.goals_for DESC, c.name`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("detail standings: %w", err)
	}
	defer rows.Close()
	out := []StandingRow{}
	for rows.Next() {
		var r StandingRow
		if err := rows.Scan(&r.Club.ID, &r.Club.Name, &r.Club.Short, &r.Played, &r.Won, &r.Drawn,
			&r.Lost, &r.GoalsFor, &r.GoalsAgainst, &r.Points); err != nil {
			return nil, fmt.Errorf("scan standing: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// membershipDiff returns the club ids that joined the latest season relative
// to the predecessor, and the ids that departed.
func (s *Service) membershipDiff(ctx context.Context, latestID, prevID uuid.UUID) ([]uuid.UUID, []uuid.UUID, error) {
	latest, err := s.membershipIDs(ctx, latestID)
	if err != nil {
		return nil, nil, err
	}
	prev, err := s.membershipIDs(ctx, prevID)
	if err != nil {
		return nil, nil, err
	}
	prevSet := map[uuid.UUID]bool{}
	for _, id := range prev {
		prevSet[id] = true
	}
	latestSet := map[uuid.UUID]bool{}
	for _, id := range latest {
		latestSet[id] = true
	}
	joined := []uuid.UUID{}
	for _, id := range latest {
		if !prevSet[id] {
			joined = append(joined, id)
		}
	}
	departed := []uuid.UUID{}
	for _, id := range prev {
		if !latestSet[id] {
			departed = append(departed, id)
		}
	}
	return joined, departed, nil
}

func (s *Service) membershipIDs(ctx context.Context, seasonID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT club_id FROM competition.competition_entries
		WHERE season_id = $1 AND status = 'registered' ORDER BY club_id`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("season members: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan season member: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// cupDetail assembles the cup dossier: the latest campaign season, the
// materialized bracket (reusing the tested GetCup campaign), the champion,
// and per-entrant progress.
func (s *Service) cupDetail(ctx context.Context, base *detailBase, seasons []detailSeason) (*CupDetail, error) {
	cam, err := s.GetCup(ctx, base.WorldID, base.ID)
	if err != nil {
		return nil, err
	}
	d := &CupDetail{
		LateEntryRound: cam.LateEntryRound,
		TotalRounds:    cam.TotalRounds,
		Champion:       cam.Champion,
		Rounds:         cam.Rounds,
		Clubs:          []CupClubRow{},
	}
	if len(seasons) == 0 {
		return d, nil
	}
	cur := seasons[0]
	curRef := cur.ref
	d.Season = &curRef

	// GetCup only reads the champion of a live (non-completed) season, so
	// resolve the champion of the latest campaign directly instead.
	champion, err := s.cupChampion(ctx, cur.ref.ID)
	if err != nil {
		return nil, err
	}
	d.Champion = champion

	entries, err := s.cupEntrants(ctx, cur.ref.ID)
	if err != nil {
		return nil, err
	}

	fixtures, err := s.GetFixtures(ctx, base.ID, base.WorldID, nil)
	if err != nil {
		return nil, err
	}
	window := fixturesInWindow(fixtures, cur.start, cur.end)

	countries, err := s.clubsCountries(ctx, entryIDs(entries))
	if err != nil {
		return nil, err
	}
	rounds := cupClubRounds(cam.Rounds)

	for _, e := range entries {
		row := CupClubRow{
			Club:         e.club,
			Country:      countries[e.club.ID],
			Eliminated:   e.eliminated,
			CurrentRound: rounds[e.club.ID],
		}
		row.Streak, row.NextFixture = clubStreakAndNext(window, e.club.ID)
		d.Clubs = append(d.Clubs, row)
	}
	return d, nil
}

// cupEntrant is one entrant of a cup season with its progress status.
type cupEntrant struct {
	club       apiref.ClubRef
	eliminated bool
}

func (s *Service) cupEntrants(ctx context.Context, seasonID uuid.UUID) ([]cupEntrant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.club_id, c.name, COALESCE(c.short_name, ''), (e.status = 'eliminated')
		FROM competition.competition_entries e
		JOIN club.clubs c ON c.id = e.club_id
		WHERE e.season_id = $1
		ORDER BY c.name`, seasonID)
	if err != nil {
		return nil, fmt.Errorf("cup entrants: %w", err)
	}
	defer rows.Close()
	out := []cupEntrant{}
	for rows.Next() {
		var e cupEntrant
		if err := rows.Scan(&e.club.ID, &e.club.Name, &e.club.Short, &e.eliminated); err != nil {
			return nil, fmt.Errorf("scan cup entrant: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func entryIDs(entries []cupEntrant) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.club.ID)
	}
	return out
}

// cupChampion resolves the champion club ref of one campaign season (nil when
// the season has no champion record yet).
func (s *Service) cupChampion(ctx context.Context, seasonID uuid.UUID) (*apiref.ClubRef, error) {
	var clubID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT club_id FROM competition.competition_entries
		WHERE season_id = $1 AND status = 'champion' LIMIT 1`, seasonID).Scan(&clubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cup champion: %w", err)
	}
	var name, short string
	if err := s.pool.QueryRow(ctx,
		`SELECT name, COALESCE(short_name, '') FROM club.clubs WHERE id = $1`, clubID).
		Scan(&name, &short); err != nil {
		return nil, fmt.Errorf("cup champion club: %w", err)
	}
	return &apiref.ClubRef{ID: clubID, Name: name, Short: short}, nil
}

// cupClubRounds maps each club to the highest materialized round it reached
// (via ties or byes) in the campaign bracket.
func cupClubRounds(rounds []CupRound) map[uuid.UUID]int {
	out := map[uuid.UUID]int{}
	for _, r := range rounds {
		for _, b := range r.Byes {
			out[b.ID] = max(out[b.ID], r.Round)
		}
		for _, t := range r.Ties {
			out[t.Home.ID] = max(out[t.Home.ID], r.Round)
			out[t.Away.ID] = max(out[t.Away.ID], r.Round)
		}
	}
	return out
}

// clubsCountries resolves each club's country (name + world-scoped code).
func (s *Service) clubsCountries(ctx context.Context, clubIDs []uuid.UUID) (map[uuid.UUID]apiref.CountryRef, error) {
	out := map[uuid.UUID]apiref.CountryRef{}
	if len(clubIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT cl.id, cl.country, wc.id, COALESCE(wc.code, '')
		FROM club.clubs cl
		LEFT JOIN world.countries wc ON wc.name = cl.country AND wc.world_id = cl.world_id
		WHERE cl.id = ANY($1::uuid[])`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("club countries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
			cid  *uuid.UUID
			code string
		)
		if err := rows.Scan(&id, &name, &cid, &code); err != nil {
			return nil, fmt.Errorf("scan club country: %w", err)
		}
		ref := apiref.CountryRef{Name: name, Code: code}
		if cid != nil {
			ref.ID = *cid
		}
		out[id] = ref
	}
	return out, rows.Err()
}

// clubRefs resolves club identity rows in name order.
func (s *Service) clubRefs(ctx context.Context, clubIDs []uuid.UUID) ([]apiref.ClubRef, error) {
	out := []apiref.ClubRef{}
	if len(clubIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, COALESCE(short_name, '') FROM club.clubs
		WHERE id = ANY($1::uuid[]) ORDER BY name`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("club refs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r apiref.ClubRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Short); err != nil {
			return nil, fmt.Errorf("scan club ref: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// findPredecessor returns the completed season immediately before the latest
// season (the one the latest season's movement was calculated against).
func findPredecessor(seasons []detailSeason) *detailSeason {
	for i := 1; i < len(seasons); i++ {
		if seasons[i].ref.Status == "completed" {
			return &seasons[i]
		}
	}
	return nil
}

// fixturesInWindow keeps fixtures whose scheduled kickoff falls in
// [start, end) (inclusive start, exclusive end; nil end is unbounded).
func fixturesInWindow(fixtures []Fixture, start time.Time, end *time.Time) []Fixture {
	out := []Fixture{}
	for _, f := range fixtures {
		if f.ScheduledAt.Before(start) {
			continue
		}
		if end != nil && !f.ScheduledAt.Before(*end) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// clubStreakAndNext computes a club's leading result streak and its next
// scheduled fixture from a season-windowed fixture list (ascending).
func clubStreakAndNext(fixtures []Fixture, clubID uuid.UUID) (StreakInfo, *Fixture) {
	var next *Fixture
	completed := []Fixture{}
	for _, f := range fixtures {
		if f.HomeClub.ID != clubID && f.AwayClub.ID != clubID {
			continue
		}
		if f.Status == "completed" {
			completed = append(completed, f)
			continue
		}
		if next == nil {
			n := f
			next = &n
		}
	}
	run := StreakInfo{}
	for i := len(completed) - 1; i >= 0; i-- {
		f := completed[i]
		if f.HomeScore == nil || f.AwayScore == nil {
			break
		}
		outcome := fixtureOutcome(f, clubID)
		if run.Outcome == "" {
			run = StreakInfo{Outcome: outcome, Length: 1}
			continue
		}
		if outcome != run.Outcome {
			break
		}
		run.Length++
	}
	return run, next
}

// fixtureOutcome reports "W", "D", or "L" for a club in a decided fixture.
func fixtureOutcome(f Fixture, clubID uuid.UUID) string {
	my, opp := *f.HomeScore, *f.AwayScore
	if f.HomeClub.ID != clubID {
		my, opp = *f.AwayScore, *f.HomeScore
	}
	switch {
	case my > opp:
		return "W"
	case my < opp:
		return "L"
	default:
		return "D"
	}
}

func clamp(i int) int {
	if i < 0 {
		return 0
	}
	return i
}
