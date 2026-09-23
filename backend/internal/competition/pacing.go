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
	matchdaysPerWeek int   // how many matchdays pack into one game-week
	daysPerWeek      int   // length of one game-week in world game-days
	kickoffHours     []int // kickoff-hour rotation (UTC); matchday k uses kickoffHours[(base+k-1)%len]
	// allowedWeekdays is the IM05 ISO-weekday set (1=Mon..7=Sun) the season
	// may play on. Empty means weekday-aware scheduling is OFF and the legacy
	// day-formula pacing below applies unchanged (regression: an unconfigured
	// season reproduces the exact IM03 calendar).
	allowedWeekdays []int
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
// allowed_weekdays set (IM05) resolves per-league → country default → unset.
func (s *Service) scheduleParams(ctx context.Context, q rowQueryer, leagueID, worldID uuid.UUID) (*scheduleParamsResolved, error) {
	p := &scheduleParamsResolved{
		matchdaysPerWeek: DefaultMatchdaysPerWeek,
		daysPerWeek:      DefaultDaysPerWeek,
		kickoffHours:     cloneHours(DefaultKickoffHours),
	}

	var matchdaysText, daysText string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(scheduling_rules->>'matchdays_per_week', ''),
		       COALESCE(scheduling_rules->>'days_per_week', '')
		FROM competition.competition_rules
		WHERE competition_id = $1`, leagueID).Scan(&matchdaysText, &daysText)
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

	// allowed_weekdays (IM05): per-league → country default → unset. The
	// country id is nil whenever the league is country-less, which the
	// resolver treats as "no weekday set" (legacy pacing).
	countryID, err := competitionCountry(ctx, q, leagueID)
	if err != nil {
		return nil, err
	}
	if p.allowedWeekdays, err = s.resolveAllowedWeekdays(ctx, q, leagueID, countryID); err != nil {
		return nil, err
	}
	return p, nil
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
