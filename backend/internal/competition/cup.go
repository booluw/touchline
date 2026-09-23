package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
)

// ---------------------------------------------------------------------------
// Domestic cup (IM04)
//
// The system-seeded knockout cup consumes the schema hooks migrations 0012/
// 0037 reserved: competition_type 'domestic_cup', format 'knockout', role
// 'cup' memberships, and the registered/qualified/eliminated/champion entry
// statuses. It stages a country's league clubs behind a configurable late
// entry (top N of tier 1 join when X bottom-tier survivors remain), draws the
// bracket deterministically from world_seed ⊕ cup_id ⊕ round, and resolves
// level ties by golden goal (overflow from matchsim, wired via format by
// internal/match). Knockout results never touch competition.standings and
// never trigger the country promotion/relegation cascade.
// ---------------------------------------------------------------------------

const (
	// cupEntryKind is the qualification source written to
	// competition_rules.qualification_rules at creation (introspection).
	cupEntryKind = "country_league_members"
	// cupMaxMemberships is the per-club role='cup' cap (migration 0037).
	cupMaxMemberships = 3
	// cup scheduling fallback when the admin declares none: one cup round per
	// weekly game-week, on the default kickoff-hour rotation.
	cupMatchdaysPerWeek = 1
	cupDaysPerWeek      = 7
)

// CupParams is the admin declaration for a new domestic cup. N and X are the
// staged-eligibility variables (see Cup.FirstTierBye/SurvivorThreshold).
type CupParams struct {
	WorldID           uuid.UUID       `json:"world_id"`
	CountryID         uuid.UUID       `json:"country_id"`
	Name              string          `json:"name"`
	FirstTierBye      int             `json:"first_tier_bye"`     // N: top-N tier-1 clubs join late
	SurvivorThreshold int             `json:"survivor_threshold"` // X: survivors remain when they do
	PrizePool         float64         `json:"prize_pool,omitempty"`
	SchedulingRules   json.RawMessage `json:"scheduling_rules,omitempty"`
}

// Cup is a domestic cup competition decorated with its country and rules.
type Cup struct {
	ID                uuid.UUID         `json:"id"`
	WorldID           uuid.UUID         `json:"world_id"`
	Country           apiref.CountryRef `json:"country"`
	Name              string            `json:"name"`
	CompetitionType   string            `json:"competition_type"`
	Status            string            `json:"status"`
	PrizePool         float64           `json:"prize_pool"`
	Format            string            `json:"format"`
	IsHomeAndAway     bool              `json:"is_home_and_away"`
	FirstTierBye      int               `json:"first_tier_bye"`
	SurvivorThreshold int               `json:"survivor_threshold"`
}

// CupTie is one bracket tie: a scheduled fixture with a decided winner where
// the round has completed.
type CupTie struct {
	ID          uuid.UUID       `json:"id"`
	Home        apiref.ClubRef  `json:"home_club"`
	Away        apiref.ClubRef  `json:"away_club"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	Status      string          `json:"status"`
	HomeScore   *int            `json:"home_score,omitempty"`
	AwayScore   *int            `json:"away_score,omitempty"`
	Winner      *apiref.ClubRef `json:"winner,omitempty"`
}

// CupRound is one materialized round: its ties plus the clubs that advanced
// to it on a bye (no tie this round).
type CupRound struct {
	Round       int              `json:"round"`
	ScheduledAt *time.Time       `json:"scheduled_at,omitempty"`
	Ties        []CupTie         `json:"ties"`
	Byes        []apiref.ClubRef `json:"byes,omitempty"`
}

// CupCampaign is the manager cup view: cup, qualification read-back, season,
// the materialized round tree, and the champion once decided.
type CupCampaign struct {
	Cup                Cup             `json:"cup"`
	QualificationRules json.RawMessage `json:"qualification_rules"`
	LateEntryRound     int             `json:"late_entry_round"`
	TotalRounds        int             `json:"total_rounds"`
	Season             *Season         `json:"season,omitempty"`
	Champion           *apiref.ClubRef `json:"champion,omitempty"`
	Rounds             []CupRound      `json:"rounds"`
}

// roundPlan is one round of the computed cup ladder (JSON-tagged so the plan
// persists the ladder for lazy materialization).
type roundPlan struct {
	Round int `json:"round"`
	N     int `json:"n"`
	Ties  int `json:"ties"`
	Byes  int `json:"byes"`
	// Date is the IM05 anchored calendar slot: the final lands a few days
	// after the country's latest league fixture, and earlier rounds walk
	// backward on seeded 2-3-day gaps snapped to league-free days. Nil keeps
	// the legacy one-round-per-week placement (used when the country has no
	// league season to anchor against).
	Date *time.Time `json:"date,omitempty"`
}

// cupPlan is the per-cup campaign read-back stored on
// competition_rules.qualification_rules at campaign time so later rounds
// materialize against the same entrants the bracket was drawn from. The full
// ladder is persisted so lazy materialization reproduces the landing-round
// sizes exactly (naive halving would drift from the join trigger).
type cupPlan struct {
	TopNClubIDs []uuid.UUID `json:"top_n_club_ids"`
	LateEntry   int         `json:"late_entry_round"`
	Total       int         `json:"total_rounds"`
	Ladder      []roundPlan `json:"ladder"`
	Joined      bool        `json:"joined"`
}

// cupLateEntry mirrors the admin-facing qualification JSON.
type cupLateEntry struct {
	Teams              int `json:"teams"`
	EnterWhenSurvivors int `json:"enter_when_survivors"`
}

// cupRulesDoc is the qualification_rules document shape used by both create
// and campaign steps.
type cupRulesDoc struct {
	Entry              string       `json:"entry"`
	FirstTierLateEntry cupLateEntry `json:"first_tier_late_entry"`
}

func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

// ---------------------------------------------------------------------------
// Create / read
// ---------------------------------------------------------------------------

// CreateCup declares a domestic cup under a country. Only the staging
// variables are checked here (N >= 0, X >= 1, X+N >= 2); the field-dependent
// checks run at campaign time when the bottom pool size is known.
func (s *Service) CreateCup(ctx context.Context, p CupParams) (*Cup, error) {
	name := trimSpace(p.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if err := cupStagingBaseValid(p.FirstTierBye, p.SurvivorThreshold); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create cup: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT world_id FROM world.countries WHERE id = $1`, p.CountryID).Scan(&worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load country: %w", err)
	}
	if worldID != p.WorldID {
		return nil, ErrCompetitionWorldMismatch
	}

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.competitions
			WHERE country_id = $1 AND lower(name) = lower($2))`,
		p.CountryID, name).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check cup name: %w", err)
	}
	if exists {
		return nil, ErrNameCollision
	}

	rules, err := cupRulesJSON(p.SchedulingRules)
	if err != nil {
		return nil, err
	}
	qual, err := cupQualificationJSON(p.FirstTierBye, p.SurvivorThreshold)
	if err != nil {
		return nil, err
	}

	var cupID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO competition.competitions
			(world_id, country_id, name, competition_type, prize_pool, status)
		VALUES ($1, $2, $3, 'domestic_cup', $4, 'active')
		RETURNING id`, worldID, p.CountryID, name, p.PrizePool).Scan(&cupID); err != nil {
		return nil, fmt.Errorf("insert cup: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO competition.competition_rules
			(competition_id, format, qualification_rules, is_home_and_away, scheduling_rules)
		VALUES ($1, 'knockout', $2, FALSE, $3)`,
		cupID, qual, rules); err != nil {
		return nil, fmt.Errorf("insert cup rules: %w", err)
	}

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create cup: %w", err)
	}
	return cup, nil
}

// ListCups returns the world's domestic cups ordered by name. It reads the
// staging variables back from qualification_rules.
func (s *Service) ListCups(ctx context.Context, worldID uuid.UUID) ([]Cup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.name, c.competition_type, c.status, c.prize_pool,
		       wc.name, wc.code, r.format, r.is_home_and_away,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'teams', '0')::int,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'enter_when_survivors', '0')::int
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		JOIN world.countries wc ON wc.id = c.country_id
		WHERE c.world_id = $1 AND c.competition_type = 'domestic_cup'
		ORDER BY c.name`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list cups: %w", err)
	}
	defer rows.Close()
	out := []Cup{}
	for rows.Next() {
		var cup Cup
		if err := rows.Scan(&cup.ID, &cup.WorldID, &cup.Country.ID, &cup.Name,
			&cup.CompetitionType, &cup.Status, &cup.PrizePool,
			&cup.Country.Name, &cup.Country.Code, &cup.Format, &cup.IsHomeAndAway,
			&cup.FirstTierBye, &cup.SurvivorThreshold); err != nil {
			return nil, fmt.Errorf("scan cup: %w", err)
		}
		out = append(out, cup)
	}
	return out, rows.Err()
}

// GetCup returns the manager cup view for a world-scoped cup.
func (s *Service) GetCup(ctx context.Context, worldID uuid.UUID, cupID uuid.UUID) (*CupCampaign, error) {
	cup, err := s.getCup(ctx, s.pool, cupID)
	if err != nil {
		return nil, err
	}
	if cup.WorldID != worldID {
		return nil, ErrCompetitionNotFound
	}

	var qual json.RawMessage
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(qualification_rules, '{}'::jsonb)
		FROM competition.competition_rules WHERE competition_id = $1`, cupID).Scan(&qual)
	if err != nil {
		return nil, fmt.Errorf("cup rules: %w", err)
	}

	var plan cupPlan
	if raw, ok := planFromQual(qual); ok {
		plan = raw
	}

	cam := &CupCampaign{
		Cup:                *cup,
		QualificationRules: qual,
		LateEntryRound:     plan.LateEntry,
		TotalRounds:        plan.Total,
		Rounds:             []CupRound{},
	}

	season, err := s.activeSeason(ctx, s.pool, cupID, worldID)
	if errors.Is(err, ErrNoSeason) {
		return cam, nil
	}
	if err != nil {
		return nil, err
	}
	cam.Season = season

	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.world_id, f.competition_id, f.home_club_id, f.away_club_id, f.matchday,
		       f.scheduled_at, f.status, f.ht_score, f.at_score,
		       h.name, COALESCE(h.short_name, ''), a.name, COALESCE(a.short_name, ''), c.name
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE f.competition_id = $1 AND f.world_id = $2 AND f.status <> 'cancelled'
		ORDER BY f.matchday, f.scheduled_at, f.home_club_id`, cupID, worldID)
	if err != nil {
		return nil, fmt.Errorf("cup fixtures: %w", err)
	}
	fixtures, err := scanFixtures(rows)
	if err != nil {
		return nil, err
	}

	byes, err := s.cupByes(ctx, season.ID, 0)
	if err != nil {
		return nil, err
	}
	for _, f := range fixtures {
		tie := CupTie{
			ID: f.ID, Home: f.HomeClub, Away: f.AwayClub,
			ScheduledAt: f.ScheduledAt, Status: f.Status,
			HomeScore: f.HomeScore, AwayScore: f.AwayScore,
		}
		if f.Status == "completed" && f.HomeScore != nil && f.AwayScore != nil &&
			*f.HomeScore != *f.AwayScore {
			if *f.HomeScore > *f.AwayScore {
				tie.Winner = &f.HomeClub
			} else {
				tie.Winner = &f.AwayClub
			}
		}
		if len(cam.Rounds) > 0 && cam.Rounds[len(cam.Rounds)-1].Round == f.Matchday {
			cam.Rounds[len(cam.Rounds)-1].Ties = append(cam.Rounds[len(cam.Rounds)-1].Ties, tie)
			continue
		}
		rb := byes[f.Matchday]
		rnd := CupRound{Round: f.Matchday, Ties: []CupTie{tie}, Byes: rb}
		cam.Rounds = append(cam.Rounds, rnd)
	}

	var championID *uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		SELECT club_id FROM competition.competition_entries
		WHERE season_id = $1 AND status = 'champion' LIMIT 1`, season.ID).Scan(&championID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("cup champion: %w", err)
		}
	}
	if championID != nil {
		var name string
		var short string
		if err := s.pool.QueryRow(ctx,
			`SELECT name, COALESCE(short_name, '') FROM club.clubs WHERE id = $1`, *championID).
			Scan(&name, &short); err != nil {
			return nil, fmt.Errorf("cup champion club: %w", err)
		}
		cam.Champion = &apiref.ClubRef{ID: *championID, Name: name, Short: short}
	}
	return cam, nil
}

// getCup loads one cup competition joined with its country and rules.
func (s *Service) getCup(ctx context.Context, q rowQueryer, cupID uuid.UUID) (*Cup, error) {
	var cup Cup
	err := q.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.name, c.competition_type, c.status, c.prize_pool,
		       wc.name, wc.code, r.format, r.is_home_and_away,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'teams', '0')::int,
		       COALESCE(r.qualification_rules->'first_tier_late_entry'->>'enter_when_survivors', '0')::int
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		JOIN world.countries wc ON wc.id = c.country_id
		WHERE c.id = $1 AND c.competition_type = 'domestic_cup'`, cupID).
		Scan(&cup.ID, &cup.WorldID, &cup.Country.ID, &cup.Name, &cup.CompetitionType,
			&cup.Status, &cup.PrizePool, &cup.Country.Name, &cup.Country.Code,
			&cup.Format, &cup.IsHomeAndAway, &cup.FirstTierBye, &cup.SurvivorThreshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompetitionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cup, nil
}

// cupByes returns, per round, the clubs that advanced on a bye (is_bye rows).
// A round of 0 returns all byes of the season (used by the campaign view).
func (s *Service) cupByes(ctx context.Context, seasonID uuid.UUID, round int) (map[int][]apiref.ClubRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cb.round, cb.club_id, cl.name, COALESCE(cl.short_name, '')
		FROM competition.cup_bracket cb
		JOIN club.clubs cl ON cl.id = cb.club_id
		WHERE cb.season_id = $1 AND ($2 = 0 OR cb.round = $2) AND cb.is_bye
		ORDER BY cb.round, cb.seed`, seasonID, round)
	if err != nil {
		return nil, fmt.Errorf("cup byes: %w", err)
	}
	defer rows.Close()
	out := map[int][]apiref.ClubRef{}
	for rows.Next() {
		var r int
		var ref apiref.ClubRef
		if err := rows.Scan(&r, &ref.ID, &ref.Name, &ref.Short); err != nil {
			return nil, fmt.Errorf("scan cup bye: %w", err)
		}
		out[r] = append(out[r], ref)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Staging validation
// ---------------------------------------------------------------------------

func cupStagingBaseValid(n, x int) error {
	if n < 0 || x < 1 || x+n < 2 {
		return ErrStagingInvalid
	}
	return nil
}

// cupQualificationJSON renders the admin-facing qualification_rules document.
func cupQualificationJSON(n, x int) ([]byte, error) {
	b, err := json.Marshal(cupRulesDoc{
		Entry: cupEntryKind,
		FirstTierLateEntry: cupLateEntry{
			Teams:              n,
			EnterWhenSurvivors: x,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal qualification rules: %w", err)
	}
	return b, nil
}

// cupRulesJSON normalises the cup's scheduling_rules: it always stamps a
// one-round-per-week default so cup rounds do not collide with league pacing.
func cupRulesJSON(raw json.RawMessage) (json.RawMessage, error) {
	declared := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &declared); err != nil {
			return nil, errors.New("scheduling_rules must be a JSON object")
		}
	}
	if _, ok := declared["matchdays_per_week"]; !ok {
		declared["matchdays_per_week"] = cupMatchdaysPerWeek
	}
	if _, ok := declared["days_per_week"]; !ok {
		declared["days_per_week"] = cupDaysPerWeek
	}
	if _, ok := declared["kickoff_hours"]; !ok {
		declared["kickoff_hours"] = DefaultKickoffHours
	}
	b, err := json.Marshal(declared)
	if err != nil {
		return nil, fmt.Errorf("marshal scheduling rules: %w", err)
	}
	return b, nil
}

// planFromQual decodes the campaign plan a StartCupCampaign stored on the
// cup's qualification_rules.
func planFromQual(raw json.RawMessage) (cupPlan, bool) {
	if len(raw) == 0 {
		return cupPlan{}, false
	}
	var doc struct {
		Campaign *cupPlan `json:"campaign"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Campaign == nil {
		return cupPlan{}, false
	}
	return *doc.Campaign, true
}

// stagingFromRules decodes the admin-facing N/X staging variables.
func stagingFromRules(raw json.RawMessage) (n, x int) {
	var doc cupRulesDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, 0
	}
	return doc.FirstTierLateEntry.Teams, doc.FirstTierLateEntry.EnterWhenSurvivors
}

// ---------------------------------------------------------------------------
// Bracket construction
// ---------------------------------------------------------------------------

// cupLadder computes the whole cup from the bottom pool size F and the staging
// variables: early knockout rounds reduce F to exactly X survivors (byes
// arranged so the count lands dead on X), the top-N tier-1 clubs then join for
// a field of X+N, and the remaining rounds play down to a two-club final.
func cupLadder(f, x, n int) ([]roundPlan, error) {
	if err := cupStagingBaseValid(n, x); err != nil {
		return nil, err
	}
	if f < x {
		return nil, ErrStagingInvalid
	}

	rounds := []roundPlan{}
	r := 1
	// Early rounds: F → X survivors.
	for cur := f; cur > x; r++ {
		plan := roundPlan{Round: r, N: cur}
		next := (cur + 1) / 2
		if next >= x {
			plan.Ties, plan.Byes = cur/2, cur%2
		} else {
			// Landing round: eliminate exactly cur-x so survivors are x.
			plan.Ties, plan.Byes = cur-x, 2*x-cur
			next = x
		}
		rounds = append(rounds, plan)
		cur = next
	}

	// Join + finals: X → X+N enters, then halve to the two-club final.
	joined := false
	for cur := x; ; r++ {
		p := cur
		if !joined {
			p = x + n
			joined = true
		}
		rounds = append(rounds, roundPlan{Round: r, N: p, Ties: p / 2, Byes: p % 2})
		if p == 2 {
			break
		}
		cur = (p + 1) / 2
	}
	return rounds, nil
}

// roundSeed folds the cup id and round into the world seed so each round
// draws from its own deterministic sub-stream.
func roundSeed(seed int64, cupID uuid.UUID, round int) int64 {
	return hashMix(seed, cupID) ^ int64(uint64(round)*0x9E3779B97F4A7C15)
}

// drawRound shuffles a canonically sorted participant list deterministically:
// the first `ties` pairs play, the trailing `byes` clubs advance without a
// tie. Pair order and per-tie home flip both come from the same seeded rng.
func drawRound(seed int64, cupID uuid.UUID, round, ties, byes int, clubs []uuid.UUID) ([][2]uuid.UUID, []uuid.UUID) {
	rng := rand.New(rand.NewSource(roundSeed(seed, cupID, round)))
	ids := append([]uuid.UUID(nil), clubs...)
	rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	pairs := [][2]uuid.UUID{}
	for i := 0; i < 2*ties && i+1 < len(ids); i += 2 {
		a, b := ids[i], ids[i+1]
		if rng.Intn(2) != 0 {
			a, b = b, a
		}
		pairs = append(pairs, [2]uuid.UUID{a, b})
	}
	return pairs, ids[len(ids)-byes:]
}

// cupGap is the seeded number of days a cup round sits before the round it
// feeds. The draw streams from world_seed ⊕ cup_id ⊕ round (roundSeed), so a
// round's gap is deterministic and replayable. Close to the final the gap is
// biased toward 3 game-days so the run-in breathes: the round directly before
// the final draws 3 days 75% of the time, one step out 50/50, and every
// earlier round takes the flat 2-day minimum (IM05).
func cupGap(seed int64, cupID uuid.UUID, round, total int) int {
	dist := total - round
	var weight3 int
	switch {
	case dist == 1:
		weight3 = 3
	case dist == 2:
		weight3 = 1
	}
	if weight3 == 0 {
		return 2
	}
	rng := rand.New(rand.NewSource(roundSeed(seed, cupID, round)))
	if rng.Intn(weight3+1) < weight3 {
		return 3
	}
	return 2
}

// countryLeagueDays returns the sorted set of calendar days on which any of
// the country's leagues has a non-cancelled fixture — the days a cup round
// must not land on, because the league calendar is the country's anchor.
func (s *Service) countryLeagueDays(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) ([]time.Time, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT scheduled_at::date
		FROM match.fixtures f
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE c.country_id = $1 AND f.world_id = $2
		  AND c.competition_type = 'league' AND f.status <> 'cancelled'`,
		countryID, worldID)
	if err != nil {
		return nil, fmt.Errorf("country league days: %w", err)
	}
	defer rows.Close()
	seen := map[time.Time]bool{}
	out := []time.Time{}
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("scan league day: %w", err)
		}
		d = daysTruncate(d)
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, rows.Err()
}

// dayClearance is the number of whole days between d and the nearest country
// league day. A clearance of >= 2 is a full rest day; 0 means d itself is a
// league day (a collision to avoid when possible).
func dayClearance(d time.Time, leagueDays []time.Time) int {
	best := 1000
	for _, ld := range leagueDays {
		g := daysBetween(d, ld)
		if g < 0 {
			g = -g
		}
		if g < best {
			best = g
		}
	}
	return best
}

// cupDayScore ranks a candidate round day: a full two-day rest from the
// league calendar dominates, then the borderline "book-ended by league days"
// case, and finally proximity to the backward-walk target. Days that collide
// with a league day score zero, so they are only chosen when nothing else
// survives.
func cupDayScore(d, target time.Time, leagueDays []time.Time) int {
	cleared := dayClearance(d, leagueDays)
	base := 0
	switch {
	case cleared >= 2:
		base = 10000 + cleared
	case cleared == 1:
		base = 1000 // adjacent to a league day — permitted only as last resort
	}
	return base - daysBetween(target, d)
}

// findCupRoundDay places one cup round (IM05). It scans the game-days within
// two of the backward-walk target, bounded so the round keeps at least a
// two-day rest from its successor, and picks the day that (1) honors the
// allowed-weekday set when the cup declares one — falling back to any fit
// only when no allowed weekday exists in the window — (2) keeps the widest
// full-rest clearance from the country's league days (league days themselves
// are never picked while any other day survives), and (3) sits closest to the
// target.
func findCupRoundDay(target, next time.Time, leagueDays []time.Time, weekdays []int) time.Time {
	low := daysTruncate(target).AddDate(0, 0, -2)
	high := target.AddDate(0, 0, 2)
	if cap := daysTruncate(next).AddDate(0, 0, -2); high.After(cap) {
		high = cap
	}
	best, bestScore := time.Time{}, 0
	for d := low; !d.After(high); d = d.AddDate(0, 0, 1) {
		if len(weekdays) > 0 && !isAllowedWeekday(d, weekdays) {
			continue
		}
		score := cupDayScore(d, target, leagueDays)
		if best.IsZero() || score > bestScore {
			best, bestScore = d, score
		}
	}
	if !best.IsZero() {
		return best
	}
	// No allowed weekday fits the window — degrade to the best day regardless
	// of weekday (anchoring beats never playing the round).
	for d := low; !d.After(high); d = d.AddDate(0, 0, 1) {
		score := cupDayScore(d, target, leagueDays)
		if best.IsZero() || score > bestScore {
			best, bestScore = d, score
		}
	}
	if best.IsZero() {
		return daysTruncate(target)
	}
	return best
}

// planCupCalendar derives the anchored calendar slot of every cup round
// (IM05). The final lands the first allowed weekday at least three game-days
// after the country's latest league fixture — a small window ahead of the next
// season's rollover anchor — then each earlier round walks backward on a
// seeded 2-3-day gap (cupGap) and snaps onto the best league-free day
// (findCupRoundDay). Rounds are stamped onto the ladder, which StartCupCampaign
// persists so lazy materialization reproduces them. When the country has no
// league fixtures (no running season), no round is stamped and materializeRound
// keeps the legacy weekly placement.
func (s *Service) planCupCalendar(ctx context.Context, tx pgx.Tx, worldID, countryID, cupID uuid.UUID, seed int64, ladder []roundPlan) ([]roundPlan, error) {
	k := len(ladder)
	if k == 0 {
		return ladder, nil
	}
	days, err := s.countryLeagueDays(ctx, tx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	if len(days) == 0 {
		return ladder, nil
	}
	leagueEnd := days[len(days)-1]

	p, err := s.scheduleParams(ctx, tx, cupID, worldID)
	if err != nil {
		return nil, err
	}

	finalDate := leagueEnd.AddDate(0, 0, 3)
	if len(p.allowedWeekdays) > 0 {
		finalDate = nextAllowedWeekday(finalDate, p.allowedWeekdays)
	}
	ladder[k-1].Date = &finalDate

	for idx := k - 2; idx >= 0; idx-- {
		next := *ladder[idx+1].Date
		round := idx + 1 // 1-based round
		target := next.AddDate(0, 0, -cupGap(seed, cupID, round, k))
		date := findCupRoundDay(target, next, days, p.allowedWeekdays)
		ladder[idx].Date = &date
	}
	return ladder, nil
}

// materializeRound writes a round's bracket rows and fixture list for the
// given participants (canonical order) in one transaction. Called at campaign
// start for Round 1 and lazily, in the result transaction, for every later
// round.
func (s *Service) materializeRound(ctx context.Context, tx pgx.Tx, worldID, cupID uuid.UUID,
	seasonID uuid.UUID, plan roundPlan, participants []uuid.UUID, worldRef time.Time, seed int64) error {

	pairs, byes := drawRound(seed, cupID, plan.Round, plan.Ties, plan.Byes, participants)
	if len(pairs) != plan.Ties || len(byes) != plan.Byes {
		return fmt.Errorf("materialize round %d: draw %d/%d ties/byes, plan %d/%d (off-by-one in cup ladder)",
			plan.Round, len(pairs), len(byes), plan.Ties, plan.Byes)
	}

	byeSet := map[uuid.UUID]bool{}
	for _, b := range byes {
		byeSet[b] = true
	}
	for idx, clubID := range participants {
		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.cup_bracket (season_id, round, seed, club_id, is_bye)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (season_id, round, seed) DO NOTHING`,
			seasonID, plan.Round, idx+1, clubID, byeSet[clubID]); err != nil {
			return fmt.Errorf("insert bracket %d.%d: %w", plan.Round, idx+1, err)
		}
	}

	if plan.Ties == 0 {
		return nil
	}
	p, err := s.scheduleParams(ctx, tx, cupID, worldID)
	if err != nil {
		return err
	}
	// Cup rounds are their own matchday; with the one-round-per-week default
	// each round occupies a fresh game day. When the campaign plan carries an
	// IM05 anchored slot (planCupCalendar) that wins; otherwise the legacy
	// weekly formula applies.
	kickoff := kickoffHour(seed, cupID, p.kickoffHours, plan.Round)
	day := scheduledAtFromDay(worldRef, plan.Round, p.daysPerWeek, p.matchdaysPerWeek, kickoff)
	if plan.Date != nil {
		day = kickOff(daysTruncate(*plan.Date), kickoff)
	}
	for _, pair := range pairs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match.fixtures
				(world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
			VALUES ($1, $2, $3, $4, $5, $6, 'scheduled')`,
			worldID, cupID, pair[0], pair[1], plan.Round, day); err != nil {
			return fmt.Errorf("insert cup fixture round %d: %w", plan.Round, err)
		}
	}
	if err := s.publishCupRoundNews(ctx, tx, worldID, cupID, plan.Round, day); err != nil {
		return err
	}
	return nil
}

// publishCupRoundNews covers a materialized cup round with a country-scoped
// scheduling story.
func (s *Service) publishCupRoundNews(ctx context.Context, tx pgx.Tx, worldID, cupID uuid.UUID, round int, day time.Time) error {
	name, err := s.competitionName(ctx, tx, cupID)
	if err != nil {
		return err
	}
	return s.publishSchedulingNews(ctx, tx, worldID, cupID,
		fmt.Sprintf("%s: round %d schedule set", name, round),
		fmt.Sprintf("The %s round %d ties are set for %s.", name, round, day.Format("Mon 2 Jan 2006")))
}

// ---------------------------------------------------------------------------
// Campaign
// ---------------------------------------------------------------------------

// StartCupCampaign materializes a cup's first campaign: the season row, cup
// memberships for every eligible club, competition_entries, the campaign plan,
// and the Round-1 bracket/fixtures. Mirrors StartSeason: exactly one campaign
// per cup at a time.
func (s *Service) StartCupCampaign(ctx context.Context, worldID, countryID, cupID uuid.UUID) (*Season, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cup campaign: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldStatus string
	var worldRef time.Time
	err = tx.QueryRow(ctx,
		`SELECT status, COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1 FOR UPDATE`, worldID).
		Scan(&worldStatus, &worldRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return nil, ErrWorldArchived
	}

	cup, err := s.getCup(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}
	if cup.WorldID != worldID {
		return nil, ErrCompetitionWorldMismatch
	}
	if cup.Country.ID != countryID {
		return nil, ErrCompetitionWorldMismatch
	}

	var hasSeason bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM competition.seasons WHERE competition_id = $1)`, cupID).
		Scan(&hasSeason); err != nil {
		return nil, fmt.Errorf("check cup seasons: %w", err)
	}
	if hasSeason {
		return nil, ErrCupCampaignExists
	}

	field, tierOne, err := countryField(ctx, tx, worldID, countryID)
	if err != nil {
		return nil, err
	}

	topN, err := topTierOneClubs(ctx, s, tx, worldID, countryID, tierOne)
	if err != nil {
		return nil, err
	}
	if cup.FirstTierBye > 0 && len(topN) < cup.FirstTierBye {
		return nil, ErrStagingInvalid
	}

	bottom := field
	if cup.FirstTierBye > 0 {
		keep := map[uuid.UUID]bool{}
		for _, id := range topN[:cup.FirstTierBye] {
			keep[id] = true
		}
		bottom = make([]uuid.UUID, 0, len(field)-cup.FirstTierBye)
		for _, id := range field {
			if !keep[id] {
				bottom = append(bottom, id)
			}
		}
	}
	if len(bottom) < cup.SurvivorThreshold {
		return nil, ErrStagingInvalid
	}

	ladder, err := cupLadder(len(bottom), cup.SurvivorThreshold, cup.FirstTierBye)
	if err != nil {
		return nil, err
	}

	// Membership cap (migration 0037): at most 3 role='cup' memberships per
	// club across the world.
	var capped uuid.UUID
	cappedErr := tx.QueryRow(ctx, `
		SELECT cc.club_id
		FROM competition.club_competitions cc
		WHERE cc.world_id = $1 AND cc.role = 'cup' AND cc.competition_id <> $2
		  AND cc.club_id = ANY($3::uuid[])
		GROUP BY cc.club_id
		HAVING COUNT(*) >= $4
		LIMIT 1`, worldID, cupID, field, cupMaxMemberships).Scan(&capped)
	switch {
	case cappedErr == nil:
		return nil, fmt.Errorf("%w (club %s)", ErrCupLimit, capped)
	case errors.Is(cappedErr, pgx.ErrNoRows):
		// no club at the cap
	default:
		return nil, fmt.Errorf("cup membership cap: %w", cappedErr)
	}

	for _, clubID := range field {
		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
			VALUES ($1, $2, $3, 'cup') ON CONFLICT (club_id, competition_id) DO NOTHING`,
			worldID, clubID, cupID); err != nil {
			return nil, fmt.Errorf("cup membership %s: %w", clubID, err)
		}
	}

	season, err := s.createSeason(ctx, tx, worldID, cupID, worldRef, field)
	if err != nil {
		return nil, err
	}

	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return nil, fmt.Errorf("load world seed: %w", err)
	}
	ladder, err = s.planCupCalendar(ctx, tx, worldID, countryID, cupID, seed, ladder)
	if err != nil {
		return nil, err
	}

	// Round-1 participants: the bottom pool, or — when the pool is already
	// exactly X strong (F == X) — the late entrants join at Round 1 itself.
	participants := bottom
	if cup.FirstTierBye > 0 && len(bottom) == cup.SurvivorThreshold {
		participants = append(append([]uuid.UUID(nil), bottom...), topN[:cup.FirstTierBye]...)
	}
	planDoc := cupPlan{
		Total:  len(ladder),
		Ladder: ladder,
	}
	if cup.FirstTierBye > 0 {
		planDoc.LateEntry = 1
		for _, rp := range ladder {
			if rp.N == cup.FirstTierBye+cup.SurvivorThreshold {
				planDoc.LateEntry = rp.Round
				break
			}
		}
		planDoc.TopNClubIDs = append([]uuid.UUID(nil), topN[:cup.FirstTierBye]...)
		// F == X (the bottom pool is already exactly X strong): the late
		// entrants are in from Round 1, so the plan must not join them again
		// when applyKnockoutResult reaches LateEntry.
		if len(bottom) == cup.SurvivorThreshold {
			planDoc.Joined = true
		}
	}
	pb, err := json.Marshal(planDoc)
	if err != nil {
		return nil, fmt.Errorf("marshal campaign plan: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_rules
		SET qualification_rules = jsonb_set(
			COALESCE(qualification_rules, '{}'::jsonb), '{campaign}', $2::jsonb)
		WHERE competition_id = $1`, cupID, pb); err != nil {
		return nil, fmt.Errorf("persist campaign plan: %w", err)
	}

	if err := s.materializeRound(ctx, tx, worldID, cupID, season.ID, ladder[0], participants, worldRef, seed); err != nil {
		return nil, err
	}

	if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: "CUP_CAMPAIGN_STARTED",
		Payload: mustJSON(map[string]any{
			"competition_id":     cupID,
			"season_id":          season.ID,
			"country_id":         countryID,
			"team_count":         len(field),
			"late_entry_teams":   cup.FirstTierBye,
			"survivor_threshold": cup.SurvivorThreshold,
			"rounds":             len(ladder),
		}),
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cup campaign: %w", err)
	}
	return season, nil
}

// countryField loads every club with a role='league' membership in the country
// (all tiers), and separately the tier-1 memberships (club id → league id).
func countryField(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) ([]uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT cc.club_id
		FROM competition.club_competitions cc
		JOIN competition.competitions l ON l.id = cc.competition_id
		WHERE cc.world_id = $1 AND cc.role = 'league' AND l.country_id = $2
		ORDER BY cc.club_id`, worldID, countryID)
	if err != nil {
		return nil, nil, fmt.Errorf("country field: %w", err)
	}
	defer rows.Close()
	field := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, nil, fmt.Errorf("scan field: %w", err)
		}
		field = append(field, id)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	tRows, err := tx.Query(ctx, `
		SELECT cc.club_id, l.id FROM competition.club_competitions cc
		JOIN competition.competitions l ON l.id = cc.competition_id AND l.tier = 1
		WHERE cc.world_id = $1 AND l.country_id = $2`, worldID, countryID)
	if err != nil {
		return nil, nil, fmt.Errorf("tier one: %w", err)
	}
	defer tRows.Close()
	tierOne := map[uuid.UUID]uuid.UUID{}
	for tRows.Next() {
		var clubID, leagueID uuid.UUID
		if err := tRows.Scan(&clubID, &leagueID); err != nil {
			return nil, nil, fmt.Errorf("scan tier one: %w", err)
		}
		tierOne[clubID] = leagueID
	}
	return field, tierOne, tRows.Err()
}

// topTierOneClubs ranks the tier-1 clubs by most-recent standings
// (points, GD, GF, name); with no tier-1 results at all it falls back to the
// deterministic club order. Returns the full ordered list (the campaign caller
// takes the first N).
func topTierOneClubs(ctx context.Context, s *Service, tx pgx.Tx, worldID, countryID uuid.UUID, tierOne map[uuid.UUID]uuid.UUID) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(tierOne))
	if len(tierOne) == 0 {
		return out, nil
	}
	leagueIDs := make([]uuid.UUID, 0, len(tierOne))
	seen := map[uuid.UUID]bool{}
	for _, leagueID := range tierOne {
		if !seen[leagueID] {
			seen[leagueID] = true
			leagueIDs = append(leagueIDs, leagueID)
		}
	}

	type ranked struct {
		club        uuid.UUID
		pts, gd, gf int
		name        string
		has         bool
	}
	rows, err := tx.Query(ctx, `
		SELECT cc.club_id, COALESCE(st.points, 0), COALESCE(st.goals_for - st.goals_against, 0),
		       COALESCE(st.goals_for, 0), cl.name, (st.points IS NOT NULL)
		FROM competition.club_competitions cc
		JOIN competition.competitions l ON l.id = cc.competition_id AND l.tier = 1
		JOIN club.clubs cl ON cl.id = cc.club_id
		LEFT JOIN competition.seasons se ON se.competition_id = l.id AND se.world_id = $1 AND se.status = 'in_progress'
		LEFT JOIN competition.standings st ON st.season_id = se.id AND st.club_id = cc.club_id
		WHERE cc.world_id = $1 AND l.country_id = $2 AND cc.competition_id = ANY($3::uuid[])
		ORDER BY cc.club_id`, worldID, countryID, leagueIDs)
	if err != nil {
		return nil, fmt.Errorf("tier one standings: %w", err)
	}
	list := []ranked{}
	anyStandings := false
	for rows.Next() {
		var r ranked
		if err := rows.Scan(&r.club, &r.pts, &r.gd, &r.gf, &r.name, &r.has); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan tier one ranking: %w", err)
		}
		list = append(list, r)
		if r.has {
			anyStandings = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if !anyStandings {
		clubs := make([]uuid.UUID, 0, len(tierOne))
		for clubID := range tierOne {
			clubs = append(clubs, clubID)
		}
		sort.Slice(clubs, func(i, j int) bool { return clubs[i].String() < clubs[j].String() })
		return clubs, nil
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.has != b.has {
			return a.has
		}
		if a.pts != b.pts {
			return a.pts > b.pts
		}
		if a.gd != b.gd {
			return a.gd > b.gd
		}
		if a.gf != b.gf {
			return a.gf > b.gf
		}
		return a.name < b.name
	})
	for _, r := range list {
		out = append(out, r.club)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Knockout result application (the format branch of ApplyResult)
// ---------------------------------------------------------------------------

// applyKnockoutResult records a decided cup tie and advances the bracket. The
// fixture is already stamped 'completed' with its score (ApplyResult does that
// for every format); this step moves entries to qualified/eliminated/champion,
// closes the cup season on the final, and — when the round is the last
// unapplied one — materializes the next round in the same transaction. It
// never writes competition.standings and never triggers country rollover.
func (s *Service) applyKnockoutResult(ctx context.Context, tx pgx.Tx, fixtureID uuid.UUID,
	worldID, cupID uuid.UUID, homeClub, awayClub uuid.UUID, home, away int) error {
	if home == away {
		return ErrCupDraw
	}
	winnerClub, loserClub := homeClub, awayClub
	if away > home {
		winnerClub, loserClub = awayClub, homeClub
	}

	season, err := s.activeSeason(ctx, tx, cupID, worldID)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_entries SET status = 'qualified'
		WHERE season_id = $1 AND club_id = $2`, season.ID, winnerClub); err != nil {
		return fmt.Errorf("cup entry qualified: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_entries SET status = 'eliminated'
		WHERE season_id = $1 AND club_id = $2`, season.ID, loserClub); err != nil {
		return fmt.Errorf("cup entry eliminated: %w", err)
	}

	// The round's other ties may still be pending; advance only when this was
	// the last unapplied fixture of the round.
	var round int
	if err := tx.QueryRow(ctx,
		`SELECT matchday FROM match.fixtures WHERE id = $1`, fixtureID).Scan(&round); err != nil {
		return fmt.Errorf("cup fixture round: %w", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.fixtures
		WHERE competition_id = $1 AND matchday = $2 AND standings_applied_at IS NULL`,
		cupID, round).Scan(&remaining); err != nil {
		return fmt.Errorf("cup round remaining: %w", err)
	}
	if remaining > 0 {
		return nil
	}

	// The final is exactly two clubs (one tie, no bye). A 3-team round also has
	// a single tie but a bye too — a semifinal, not the final.
	var qual json.RawMessage
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(qualification_rules, '{}'::jsonb) FROM competition.competition_rules
		 WHERE competition_id = $1`, cupID).Scan(&qual); err != nil {
		return fmt.Errorf("cup plan: %w", err)
	}
	plan, ok := planFromQual(qual)
	if !ok || len(plan.Ladder) == 0 {
		return fmt.Errorf("cup campaign plan missing: %w", ErrStagingInvalid)
	}
	if round-1 >= len(plan.Ladder) {
		return fmt.Errorf("cup ladder exhausted at round %d: %w", round, ErrStagingInvalid)
	}
	if plan.Ladder[round-1].N == 2 {
		if _, err := tx.Exec(ctx, `
			UPDATE competition.competition_entries SET status = 'champion'
			WHERE season_id = $1 AND club_id = $2`, season.ID, winnerClub); err != nil {
			return fmt.Errorf("cup champion: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE competition.seasons SET status = 'completed', end_date = now()
			WHERE id = $1`, season.ID); err != nil {
			return fmt.Errorf("complete cup season: %w", err)
		}
		if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
			WorldID:   worldID,
			EventType: "CUP_COMPLETED",
			Payload: mustJSON(map[string]any{
				"competition_id":   cupID,
				"season_id":        season.ID,
				"champion_club_id": winnerClub,
			}),
		}); err != nil {
			return err
		}
		return nil
	}

	// Materialize the next round from the advancing set, driven by the stored
	// ladder so landing-round sizes and the join trigger stay exact.
	nextIdx := round - 1 + 1 // round is 1-based; ladder[0] is round 1
	if nextIdx >= len(plan.Ladder) {
		return fmt.Errorf("cup ladder exhausted at round %d: %w", round, ErrStagingInvalid)
	}
	next := plan.Ladder[nextIdx]

	advancing, err := cupAdvancing(ctx, tx, season.ID, cupID, round)
	if err != nil {
		return err
	}

	// Join: the top-N late entrants join exactly when X survivors remain and
	// the join has not yet materialized.
	n, x := stagingFromRules(qual)
	joined := plan.Joined
	if len(advancing) == x && n > 0 && !joined {
		advancing = append(advancing, plan.TopNClubIDs...)
		if err := s.setEntryQualified(ctx, tx, season.ID, plan.TopNClubIDs); err != nil {
			return err
		}
		joined = true
		if _, err := tx.Exec(ctx, `
			UPDATE competition.competition_rules
			SET qualification_rules = jsonb_set(
				COALESCE(qualification_rules, '{}'::jsonb),
				'{campaign,joined}', 'true'::jsonb)
			WHERE competition_id = $1`, cupID); err != nil {
			return fmt.Errorf("persist join flag: %w", err)
		}
	}

	if len(advancing) != next.N {
		return fmt.Errorf("cup round %d advancing set %d, ladder plans %d: %w",
			round, len(advancing), next.N, ErrStagingInvalid)
	}
	sort.Slice(advancing, func(i, j int) bool { return advancing[i].String() < advancing[j].String() })

	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return fmt.Errorf("load world seed: %w", err)
	}
	var worldRef time.Time
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1`, worldID).Scan(&worldRef); err != nil {
		return fmt.Errorf("load world ref: %w", err)
	}
	if err := s.materializeRound(ctx, tx, worldID, cupID, season.ID, next, advancing, worldRef, seed); err != nil {
		return err
	}
	return nil
}

// cupAdvancing returns the clubs that advance from a completed round: the
// winners of the round's ties plus the clubs that carried a bye (cup_bracket
// is_bye). Winners are read from the applied fixtures; for dedup with byes the
// set is unioned and canonically sorted by the caller.
func cupAdvancing(ctx context.Context, tx pgx.Tx, seasonID uuid.UUID, cupID uuid.UUID, round int) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT CASE WHEN ht_score > at_score THEN home_club_id ELSE away_club_id END
		FROM match.fixtures
		WHERE competition_id = $1 AND matchday = $2 AND standings_applied_at IS NOT NULL`, cupID, round)
	if err != nil {
		return nil, fmt.Errorf("cup winners: %w", err)
	}
	out := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan cup winner: %w", err)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byes, err := tx.Query(ctx, `
		SELECT club_id FROM competition.cup_bracket
		WHERE season_id = $1 AND round = $2 AND is_bye`, seasonID, round)
	if err != nil {
		return nil, fmt.Errorf("cup byes: %w", err)
	}
	defer byes.Close()
	for byes.Next() {
		var id uuid.UUID
		if err := byes.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan cup bye: %w", err)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, byes.Err()
}

// setEntryQualified flips a batch of entries to 'qualified' (the late entrants
// when they join the bracket).
func (s *Service) setEntryQualified(ctx context.Context, tx pgx.Tx, seasonID uuid.UUID, clubs []uuid.UUID) error {
	if len(clubs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE competition.competition_entries SET status = 'qualified'
		WHERE season_id = $1 AND club_id = ANY($2::uuid[])`, seasonID, clubs); err != nil {
		return fmt.Errorf("cup entrants qualified: %w", err)
	}
	return nil
}
