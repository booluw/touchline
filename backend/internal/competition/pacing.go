package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// scheduleParamsResolved is the fixture-pacing configuration a season's
// calendar is built from. All values resolve from
// competition_rules.scheduling_rules or compiled defaults (see scheduleParams).
type scheduleParamsResolved struct {
	matchdaysPerWeek int   // how many matchdays pack into one game-week (legacy pacing)
	daysPerWeek      int   // length of one game-week in world game-days (legacy pacing)
	kickoffHours     []int // kickoff-hour rotation (UTC); matchday k uses kickoffHours[(base+k-1)%len]
	// allowedWeekdays is the IM05 ISO-weekday set (1=Mon..7=Sun) the season
	// may play on. Empty with a non-staggered season means weekday-aware
	// scheduling is OFF and the exact IM03 calendar applies.
	allowedWeekdays []int
	// humanHours / aiHours are the kickoff-hour pools (UTC) staggered rounds
	// use: fixtures involving a human-managed club (is_ai_controlled = false)
	// take evening slots, AI-only fixtures scatter through the day.
	humanHours []int
	aiHours    []int
	// maxSimultaneous caps how many fixtures of one staggered competition may
	// be live at the same time; kickoff slots never exceed it.
	maxSimultaneous int
	// finalKickoffHour is the one evening slot the season's final matchday
	// kicks every fixture at (same day, same time).
	finalKickoffHour int
	// staggered is true (the default) when a round with >= 2 allowed weekdays
	// spreads its fixtures across those days instead of one kickoff per round.
	// An explicit "staggered": false opt-out restores single-day, single-kickoff
	// rounds (and empty allowedWeekdays keeps the exact IM03 calendar).
	staggered bool
}

// rowQueryer is the subset of *pgxpool.Pool / pgx.Tx the pacing reads need.
type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// scheduleParams resolves the pacing for a league. Read at season materialization
// (inside createFixtures, so values are stamped onto the fixtures) and again at
// calendar-read time to reproduce the same grouping. Precedence mirrors
// offSeasonTicks: per-league scheduling_rules override → world calendar config
// (IM02) → compiled defaults (IM03). kickoff_hours is a JSONB array. The
// allowed_weekdays set (IM05) resolves per-league → country default → the IM22
// built-in weekday default when staggering is on (see DefaultWeekdays); a
// "staggered": false opt-out keeps empty weekdays (exact legacy pacing).
func (s *Service) scheduleParams(ctx context.Context, q rowQueryer, leagueID, worldID uuid.UUID) (*scheduleParamsResolved, error) {
	p := &scheduleParamsResolved{
		matchdaysPerWeek: DefaultMatchdaysPerWeek,
		daysPerWeek:      DefaultDaysPerWeek,
		kickoffHours:     cloneHours(DefaultKickoffHours),
		humanHours:       cloneHours(DefaultHumanKickoffHours),
		aiHours:          cloneHours(DefaultAIKickoffHours),
		maxSimultaneous:  DefaultMaxSimultaneous,
		finalKickoffHour: DefaultFinalKickoffHour,
		staggered:        true,
	}

	var matchdaysText, daysText, staggeredText, capText, finalHourText string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(scheduling_rules->>'matchdays_per_week', ''),
		       COALESCE(scheduling_rules->>'days_per_week', ''),
		       COALESCE(scheduling_rules->>'staggered', ''),
		       COALESCE(scheduling_rules->>'max_simultaneous_matches', ''),
		       COALESCE(scheduling_rules->>'final_kickoff_hour', '')
		FROM competition.competition_rules
		WHERE competition_id = $1`, leagueID).Scan(&matchdaysText, &daysText, &staggeredText, &capText, &finalHourText)
	if err != nil {
		return nil, fmt.Errorf("scheduling rules: %w", err)
	}

	// matchdays_per_week: per-league override only.
	if v, parseErr := strconv.Atoi(strings.TrimSpace(matchdaysText)); parseErr == nil && v > 0 {
		p.matchdaysPerWeek = v
	}

	// days_per_week: per-league override → world calendar config → default.
	if v, parseErr := strconv.Atoi(strings.TrimSpace(daysText)); parseErr == nil && v > 0 {
		p.daysPerWeek = v
	} else if days, err := s.worldDaysPerWeek(ctx, q, worldID); err != nil {
		return nil, err
	} else if days > 0 {
		p.daysPerWeek = days
	}

	// staggered: an explicit "false" opts out of multi-day rounds. Any other
	// value (or absence) keeps the default on.
	if v, parseErr := strconv.ParseBool(strings.TrimSpace(staggeredText)); parseErr == nil {
		p.staggered = v
	}

	// max_simultaneous_matches: cap of concurrently live fixtures of this
	// competition while it plays staggered rounds; clamped to >= 1.
	if v, parseErr := strconv.Atoi(strings.TrimSpace(capText)); parseErr == nil && v > 0 {
		p.maxSimultaneous = v
	}

	// final_kickoff_hour: single evening slot for the season-final matchday
	// (validated 0-23; invalid falls back to the default).
	if v, parseErr := strconv.Atoi(strings.TrimSpace(finalHourText)); parseErr == nil && v >= 0 && v <= 23 {
		p.finalKickoffHour = v
	}

	// kickoff_hours: JSONB array of valid UTC hours; the league may also clear
	// it with an explicit empty array or null to fall back to the defaults.
	var hoursRaw json.RawMessage
	if err := q.QueryRow(ctx, `
		SELECT scheduling_rules->'kickoff_hours'
		FROM competition.competition_rules
		WHERE competition_id = $1`, leagueID).Scan(&hoursRaw); err != nil {
		return nil, fmt.Errorf("scheduling rules: kickoff hours: %w", err)
	}
	var declared []int
	if len(hoursRaw) > 0 && string(hoursRaw) != "null" {
		if err := json.Unmarshal(hoursRaw, &declared); err != nil {
			return nil, fmt.Errorf("scheduling rules: kickoff hours: %w", err)
		}
	}
	if hours := normalizeHours(declared); len(hours) > 0 {
		p.kickoffHours = hours
	}

	// human/ai kickoff-hour pools (IM22): JSONB arrays; empty or null falls
	// back to the compiled defaults.
	if err := s.resolveHourPool(ctx, q, leagueID, "human_kickoff_hours", &p.humanHours, DefaultHumanKickoffHours); err != nil {
		return nil, err
	}
	if err := s.resolveHourPool(ctx, q, leagueID, "ai_kickoff_hours", &p.aiHours, DefaultAIKickoffHours); err != nil {
		return nil, err
	}

	// allowed_weekdays (IM05): per-league → country default → IM22 built-in
	// default when staggering is on. The country id is nil whenever the
	// competition is country-less, which the resolver treats as "no weekday
	// set".
	countryID, err := competitionCountry(ctx, q, leagueID)
	if err != nil {
		return nil, err
	}
	if p.allowedWeekdays, err = s.resolveAllowedWeekdays(ctx, q, leagueID, countryID); err != nil {
		return nil, err
	}
	if p.staggered && len(p.allowedWeekdays) == 0 {
		p.allowedWeekdays = cloneWeekdays(DefaultWeekdays)
	}
	return p, nil
}

// resolveHourPool reads one kickoff-hour pool ('human_kickoff_hours' or
// 'ai_kickoff_hours') from scheduling_rules, applying it when it holds at least
// one valid hour, otherwise falling back to the default pool.
func (s *Service) resolveHourPool(ctx context.Context, q rowQueryer, competitionID uuid.UUID, key string, target *[]int, fallback []int) error {
	var raw json.RawMessage
	if err := q.QueryRow(ctx, `
		SELECT scheduling_rules->'`+key+`'
		FROM competition.competition_rules
		WHERE competition_id = $1`, competitionID).Scan(&raw); err != nil {
		return fmt.Errorf("scheduling rules: %s: %w", key, err)
	}
	var declared []int
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &declared); err != nil {
			return fmt.Errorf("scheduling rules: %s: %w", key, err)
		}
	}
	if hours := normalizeHours(declared); len(hours) > 0 {
		*target = hours
	} else {
		*target = cloneHours(fallback)
	}
	return nil
}

func cloneWeekdays(wd []int) []int { return append([]int(nil), wd...) }

// StaggeredCap resolves the simultaneous-live-fixture cap for a competition at
// its open matchday, plus that season's greatest matchday. Cap semantics
// (IM22):
//
//   - staggered competition (>= 2 allowed weekdays, not opted out) with an open
//     matchday before the season-final: max_simultaneous_matches (default 3);
//   - anything else (single-day rounds, legacy pacing, the season-final
//     matchday): 0, meaning the whole open round may kick at once.
//
// The season-final matchday is the greatest matchday among the competition's
// fixtures (leagues materialize every round at season start, so MAX(matchday)
// is stable for a season).
func (s *Service) StaggeredCap(ctx context.Context, q rowQueryer, worldID, competitionID uuid.UUID, openMatchday int) (cap int, maxMatchday int, err error) {
	p, err := s.scheduleParams(ctx, q, competitionID, worldID)
	if err != nil {
		return 0, 0, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(MAX(matchday), 0)
		FROM match.fixtures
		WHERE competition_id = $1 AND world_id = $2 AND status <> 'cancelled'`,
		competitionID, worldID).Scan(&maxMatchday); err != nil {
		return 0, 0, fmt.Errorf("staggered cap: max matchday: %w", err)
	}
	if p.staggered && len(p.allowedWeekdays) >= 2 && openMatchday < maxMatchday {
		return p.maxSimultaneous, maxMatchday, nil
	}
	return 0, maxMatchday, nil
}

// worldDaysPerWeek reads the world's calendar.days_per_week config (IM02).
// It returns -1 when the key is absent or non-numeric so callers fall back to
// DefaultDaysPerWeek.
func (s *Service) worldDaysPerWeek(ctx context.Context, q rowQueryer, worldID uuid.UUID) (int, error) {
	var raw json.RawMessage
	err := q.QueryRow(ctx, `
		SELECT config_value FROM world.world_config
		WHERE world_id = $1 AND config_key = 'calendar.days_per_week'`, worldID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return -1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("days per week: world config: %w", err)
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return -1, nil
	}
	return v, nil
}

// scheduledAtFromDay places the k-th (1-based) matchday of a paced calendar.
// The season begins the day after the anchor and each week's matchdays spread
// evenly across the game-week:
//
//	day(anchor, k) = anchor + floor((k-1) * daysPerWeek / matchdaysPerWeek) + 1
//
// With the default 3 matchdays in a 7-game-day week the season plays game-days
// 1,3,5, then 8,10,12, … — two playing days, one rest day.
func scheduledAtFromDay(anchor time.Time, matchday, daysPerWeek, matchdaysPerWeek, kickoffHour int) time.Time {
	day := daysTruncate(anchor).AddDate(0, 0, (matchday-1)*daysPerWeek/matchdaysPerWeek+1)
	return time.Date(day.Year(), day.Month(), day.Day(), kickoffHour, 0, 0, 0, time.UTC)
}

// kickoffHour returns the matchday's kickoff hour (UTC) from the league's
// rotation, cycling deterministically from a seed-derived offset so a season's
// slots vary while replay stays stable. Degenerates to KickoffHourUTC when the
// rotation is exhausted (defensive; normalizeHours never emits an empty list).
func kickoffHour(seed int64, leagueID uuid.UUID, hours []int, matchday int) int {
	n := len(hours)
	if n == 0 {
		return KickoffHourUTC
	}
	// hashMix is an int64 xor that can go negative; fold through uint64 so the
	// modulo stays non-negative (Go's % keeps the dividend's sign).
	base := int(uint64(hashMix(seed, leagueID)) % uint64(n))
	return hours[(base+matchday-1)%n]
}

// normalizeHours keeps valid UTC hours (0-23) in declared order, dropping
// duplicates so the rotation never repeats within its own span.
func normalizeHours(hours []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, h := range hours {
		if h < 0 || h > 23 || seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

func cloneHours(hours []int) []int {
	return append([]int(nil), hours...)
}
