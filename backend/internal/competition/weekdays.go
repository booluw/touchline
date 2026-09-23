package competition

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// IM05 weekday-aware scheduling. Weekdays are ISO-8601 numbers (1 = Monday …
// 7 = Sunday). A competition spells out which weekdays it may play on in
// scheduling_rules->'allowed_weekdays'; when it does, fixtures land only on
// those days. The two-day rest floor is structural: a club never plays two
// matches closer than two game-days apart (IM05), so the walk that lays out a
// competition's matchdays steps at least one empty day between slots.

// isoWeekday returns t's ISO-8601 weekday (1 = Monday … 7 = Sunday).
func isoWeekday(t time.Time) int {
	w := int(t.Weekday())
	if w == 0 {
		return 7
	}
	return w
}

// normalizeWeekdays keeps valid ISO weekdays (1-7) in declared order, dropping
// duplicates and out-of-range values so the set is safe to persist and to walk.
func normalizeWeekdays(wd []int) []int {
	seen := map[int]bool{}
	out := []int{}
	for _, d := range wd {
		if d < 1 || d > 7 || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// isAllowedWeekday reports whether t's ISO weekday is in the configured set.
func isAllowedWeekday(t time.Time, weekdays []int) bool {
	w := isoWeekday(t)
	for _, d := range weekdays {
		if d == w {
			return true
		}
	}
	return false
}

// nextAllowedWeekday returns the earliest day on or after start whose ISO
// weekday is in the set. weekdays must be non-empty.
func nextAllowedWeekday(start time.Time, weekdays []int) time.Time {
	for day := daysTruncate(start); ; day = day.AddDate(0, 0, 1) {
		if isAllowedWeekday(day, weekdays) {
			return day
		}
	}
}

// kickOff lifts a calendar day onto the given kickoff hour (UTC), as the
// pacing helpers return midnight days and the matchday runner needs a time.
func kickOff(day time.Time, hour int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, hour, 0, 0, 0, time.UTC)
}

// paceWeekdayMatchday returns the kickoff time of the k-th (1-based) matchday
// of a competition paced onto a weekday set. Matchday 1 is the earliest
// allowed weekday on/after the day after the anchor (the season begins the day
// after its reference date, mirroring scheduledAtFromDay); each later matchday
// is the earliest allowed weekday at least two days after its predecessor, so
// the two-day rest floor holds by construction.
func paceWeekdayMatchday(anchor time.Time, matchday int, weekdays []int, kickoffHour int) time.Time {
	prev := time.Time{}
	for k := 1; k <= matchday; k++ {
		start := daysTruncate(anchor).AddDate(0, 0, 1)
		if k > 1 {
			start = prev.AddDate(0, 0, 2)
		}
		prev = nextAllowedWeekday(start, weekdays)
	}
	return kickOff(prev, kickoffHour)
}

// weekdaysJSON renders an allowed_weekdays document for storage, normalizing
// the set. Empty weekdays render as a null document so clearing an override
// restores the fallback chain.
func weekdaysJSON(wd []int) []byte {
	norm := normalizeWeekdays(wd)
	if len(norm) == 0 {
		return []byte("null")
	}
	b, err := json.Marshal(map[string]any{"allowed_weekdays": norm})
	if err != nil {
		panic(fmt.Sprintf("competition: marshal weekdays: %v", err))
	}
	return b
}

// decodeWeekdays unmarshals an allowed_weekdays value from JSONB (may be null
// or absent — the caller resolves the fallback chain).
func decodeWeekdays(raw json.RawMessage) []int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var declared []int
	if err := json.Unmarshal(raw, &declared); err != nil {
		return nil
	}
	return normalizeWeekdays(declared)
}

// resolveAllowedWeekdays resolves a competition's weekday set (IM05): its own
// scheduling_rules->'allowed_weekdays', else its country's
// default_scheduling_rules->'allowed_weekdays', else nil (legacy pacing).
func (s *Service) resolveAllowedWeekdays(ctx context.Context, q rowQueryer, competitionID uuid.UUID, countryID *uuid.UUID) ([]int, error) {
	var raw json.RawMessage
	if err := q.QueryRow(ctx, `
		SELECT scheduling_rules->'allowed_weekdays'
		FROM competition.competition_rules
		WHERE competition_id = $1`, competitionID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("scheduling rules: allowed weekdays: %w", err)
	}
	if wd := decodeWeekdays(raw); len(wd) > 0 {
		return wd, nil
	}
	if countryID == nil {
		return nil, nil
	}
	if err := q.QueryRow(ctx, `
		SELECT default_scheduling_rules->'allowed_weekdays'
		FROM world.countries WHERE id = $1`, *countryID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("country scheduling: allowed weekdays: %w", err)
	}
	return decodeWeekdays(raw), nil
}

// competitionCountry returns the competition's country id (nil for a
// country-less competition) and errors on a missing row.
func competitionCountry(ctx context.Context, q rowQueryer, competitionID uuid.UUID) (*uuid.UUID, error) {
	var countryID *uuid.UUID
	if err := q.QueryRow(ctx, `
		SELECT country_id FROM competition.competitions WHERE id = $1`, competitionID).
		Scan(&countryID); err != nil {
		return nil, fmt.Errorf("load competition country: %w", err)
	}
	return countryID, nil
}