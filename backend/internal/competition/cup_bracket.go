package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
	return s.unionLeagueDays(ctx, tx, worldID, []uuid.UUID{countryID})
}

// unionLeagueDays returns the sorted, deduped set of calendar days any of the
// given countries' leagues has a non-cancelled fixture on. Regional cup rounds
// must avoid every participating country's league days (IM08); a country with
// no league fixtures contributes nothing.
func (s *Service) unionLeagueDays(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, countryIDs []uuid.UUID) ([]time.Time, error) {
	if len(countryIDs) == 0 {
		return []time.Time{}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT f.scheduled_at::date
		FROM match.fixtures f
		JOIN competition.competitions c ON c.id = f.competition_id
		WHERE c.country_id = ANY($1::uuid[]) AND f.world_id = $2
		  AND c.competition_type = 'league' AND f.status <> 'cancelled'`,
		countryIDs, worldID)
	if err != nil {
		return nil, fmt.Errorf("union league days: %w", err)
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

// cupFinalDatePolicy reads a cup's IM10 final-date declaration for the
// calendar planner: the mode ('calculated' defaulting otherwise), the fixed
// date (nil unless fixed mode pins one), and the normalized offset.
func (s *Service) cupFinalDatePolicy(ctx context.Context, q rowQueryer, cupID uuid.UUID) (mode string, fixed *time.Time, offset int, err error) {
	var fixedRaw *time.Time
	var off int
	if err := q.QueryRow(ctx, `
		SELECT final_date_mode, final_date, final_offset_days
		FROM competition.competitions WHERE id = $1`, cupID).Scan(&mode, &fixedRaw, &off); err != nil {
		return "", nil, 0, fmt.Errorf("load cup final-date policy: %w", err)
	}
	if mode != cupFinalModeFixed {
		mode = cupFinalModeCalculated
	}
	if mode == cupFinalModeFixed && fixedRaw == nil {
		return "", nil, 0, fmt.Errorf("%w: fixed cup is missing a final_date", ErrCupFinalDateInvalid)
	}
	return mode, fixedRaw, off, nil
}

// resolveCupFinalDate derives a cup's final-round date from its IM10 policy
// (pure). 'fixed' returns the declared date; 'calculated' returns the first
// allowed weekday at least offset days after the scope's latest league fixture
// (no weekday snap when the set is empty). ok is false in 'calculated' mode
// with no league fixtures to anchor against — the ladder then stays unstamped
// and materialization keeps the legacy weekly pace.
func resolveCupFinalDate(mode string, fixed *time.Time, offset int, leagueDays []time.Time, weekdays []int) (final time.Time, ok bool) {
	if mode == cupFinalModeFixed {
		if fixed == nil {
			return time.Time{}, false
		}
		return daysTruncate(*fixed), true
	}
	if len(leagueDays) == 0 {
		return time.Time{}, false
	}
	final = leagueDays[len(leagueDays)-1].AddDate(0, 0, offset)
	if len(weekdays) > 0 {
		final = nextAllowedWeekday(final, weekdays)
	}
	return final, true
}

// planCupCalendar derives the anchored calendar slot of every cup round
// (IM05/IM10). The final lands the first allowed weekday at least the policy
// offset after the latest league fixture in the anchor days — a country's
// league days for domestic cups; the union over participating countries'
// league days for regional cups (IM08) — or, for a fixed cup, exactly on its
// declared date. Each earlier round then walks backward on a seeded 2-3-day
// gap (cupGap) and snaps onto the best league-free day (findCupRoundDay).
// Rounds are stamped onto the ladder, which the campaign caller persists so
// lazy materialization reproduces them. Empty anchor days with no fixed date
// stamp nothing and materializeRound keeps the legacy weekly placement; the
// region caller surfaces that as a warning.
func (s *Service) planCupCalendar(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, leagueDays []time.Time, cupID uuid.UUID, seed int64, ladder []roundPlan) ([]roundPlan, error) {
	k := len(ladder)
	if k == 0 {
		return ladder, nil
	}
	mode, fixed, offset, err := s.cupFinalDatePolicy(ctx, tx, cupID)
	if err != nil {
		return nil, err
	}

	p, err := s.scheduleParams(ctx, tx, cupID, worldID)
	if err != nil {
		return nil, err
	}

	if finalDate, ok := resolveCupFinalDate(mode, fixed, offset, leagueDays, p.allowedWeekdays); ok {
		ladder[k-1].Date = &finalDate
		for idx := k - 2; idx >= 0; idx-- {
			next := *ladder[idx+1].Date
			round := idx + 1 // 1-based round
			target := next.AddDate(0, 0, -cupGap(seed, cupID, round, k))
			date := findCupRoundDay(target, next, leagueDays, p.allowedWeekdays)
			ladder[idx].Date = &date
		}
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
	// weekly formula applies. Staggered cups (IM22, default with 2+ allowed
	// weekdays) spread the round's ties across those weekdays around the same
	// anchor day; the final rule (whole round on one evening) is leagues only.
	kickoff := kickoffHour(seed, cupID, p.kickoffHours, plan.Round)
	day := scheduledAtFromDay(worldRef, plan.Round, p.daysPerWeek, p.matchdaysPerWeek, kickoff)
	if plan.Date != nil {
		day = kickOff(daysTruncate(*plan.Date), kickoff)
	}

	staggered := p.staggered && len(p.allowedWeekdays) >= 2
	if staggered && !isAllowedWeekday(daysTruncate(day), p.allowedWeekdays) {
		day = kickOff(nextAllowedWeekday(daysTruncate(day), p.allowedWeekdays), kickoff)
	}

	var kicks []time.Time
	if staggered {
		human := map[uuid.UUID]bool{}
		rows, err := tx.Query(ctx,
			`SELECT id, is_ai_controlled FROM club.clubs WHERE id = ANY($1::uuid[])`, participants)
		if err != nil {
			return fmt.Errorf("load cup club control flags: %w", err)
		}
		for rows.Next() {
			var id uuid.UUID
			var ai bool
			if err := rows.Scan(&id, &ai); err != nil {
				rows.Close()
				return fmt.Errorf("scan cup club control flags: %w", err)
			}
			human[id] = !ai
		}
		rows.Close()
		ties := make([]roundFixture, 0, len(pairs))
		for _, pair := range pairs {
			ties = append(ties, roundFixture{
				home: pair[0], away: pair[1],
				human: human[pair[0]] || human[pair[1]],
			})
		}
		kicks = assembleRoundKickoffs(p, ties, daysTruncate(day))
	}
	for i, pair := range pairs {
		fixture := day
		if staggered {
			fixture = kicks[i]
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO match.fixtures
				(world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
			VALUES ($1, $2, $3, $4, $5, $6, 'scheduled')`,
			worldID, cupID, pair[0], pair[1], plan.Round, fixture); err != nil {
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
