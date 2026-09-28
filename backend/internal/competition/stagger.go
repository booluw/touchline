package competition

import (
	"bytes"
	"sort"
	"time"

	"github.com/google/uuid"
)

// IM22 staggered kickoff scheduling.
//
// A staggered round distributes its fixtures across the competition's allowed
// weekdays (when it has at least two) instead of sharing one kickoff. Fixtures
// involving a human-managed club (home or away is_ai_controlled = false) land
// on the evening pool; AI-only fixtures scatter through the day pool. A slot
// (one allowed weekday × one hour) hosts at most one fixture, so a round
// stretches onto another allowed weekday when it runs out of same-day hours;
// the runner caps how many of the competition's fixtures are live at once
// (max_simultaneous_matches). Single-day sets and "staggered": false opt-outs
// never call the assigner; they keep one kickoff per round.
//
// The whole walk is deterministic: fixture packing order is (home, away), the
// slot list is a fixed weekday × hour expansion, so identical inputs reproduce
// identical calendars whether the caller is season materialization or an admin
// re-pace.

// roundFixture is one tie of a round plus the human flag that picks its kickoff
// pool (evening for human-involving ties, day otherwise).
type roundFixture struct {
	home  uuid.UUID
	away  uuid.UUID
	human bool
}

// roundAnchorFirst returns the earliest allowed weekday on/after the day after
// the season anchor, mirroring paceWeekdayMatchday's matchday-1 start.
func roundAnchorFirst(anchor time.Time, weekdays []int) time.Time {
	return nextAllowedWeekday(daysTruncate(anchor).AddDate(0, 0, 1), weekdays)
}

// roundAnchorNext returns the earliest allowed weekday at least two game-days
// after lastRoundEnd (the last day any fixture of the previous round played).
func roundAnchorNext(lastRoundEnd time.Time, weekdays []int) time.Time {
	return nextAllowedWeekday(daysTruncate(lastRoundEnd).AddDate(0, 0, 2), weekdays)
}

// roundLastDay returns the latest game-day among a round's assigned kickoffs
// (zero if the round has no fixtures).
func roundLastDay(kicks []time.Time) time.Time {
	var last time.Time
	for _, k := range kicks {
		if d := daysTruncate(k); d.After(last) {
			last = d
		}
	}
	return last
}

// assembleRoundKickoffs packs a round's fixtures onto allowed-weekday × hour
// slots starting at anchor and returns one kickoff time per input fixture (same
// order). A slot (one allowed weekday × one hour) holds at most one fixture, so
// rounds spread across the allowed weekdays — a 10-tie round plays 6 slots on
// its first day and spills the remaining 4 to the next allowed weekday, like
// European leagues. Human-involving fixtures fill the evening pool first,
// AI-only the day pool. At least one fixture always lands on the anchor day, so
// a round's first kickoff date is its anchor. Pure and deterministic (see
// package comment). The runner separately caps how many of the competition's
// fixtures may be *live* at once (max_simultaneous_matches); this assigner only
// distributes kickoff times.
func assembleRoundKickoffs(p *scheduleParamsResolved, fixtures []roundFixture, anchor time.Time) []time.Time {
	out := make([]time.Time, len(fixtures))
	if len(fixtures) == 0 {
		return out
	}

	// Deterministic packing order independent of how the caller ordered the
	// round's ties (creation vs re-pace).
	order := make([]int, 0, len(fixtures))
	for i := range fixtures {
		order = append(order, i)
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := fixtures[order[a]], fixtures[order[b]]
		if c := bytes.Compare(x.home[:], y.home[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(x.away[:], y.away[:]) < 0
	})

	// Slot list: for each allowed weekday in the round, one slot per distinct
	// hour across both pools (an hour present in both pools counts as human).
	// Slots are expanded lazily one day at a time until every fixture is placed.
	type slot struct {
		day  time.Time
		hour int
		pool string // "human" | "ai"
	}
	fills := []int{}
	var slots []slot
	nextDay := daysTruncate(anchor)
	addDay := func() {
		d := nextAllowedWeekday(nextDay, p.allowedWeekdays)
		seen := map[int]bool{}
		add := func(hours []int, pool string) {
			for _, h := range hours {
				if seen[h] {
					continue
				}
				seen[h] = true
				slots = append(slots, slot{d, h, pool})
				fills = append(fills, 0)
			}
		}
		add(p.humanHours, "human")
		add(p.aiHours, "ai")
		nextDay = d.AddDate(0, 0, 1)
	}

	prefer := func(f *roundFixture) string {
		if f.human {
			return "human"
		}
		return "ai"
	}
	place := func(idx int, pref string) {
		// Pools never mix: a human-involving tie only ever lands on an evening
		// slot, an AI-only tie only on a day slot. When the preferred pool's
		// slots for the current day(s) are full, the round extends onto the
		// next allowed weekday rather than borrowing the other pool's hour.
		for placed := false; !placed; {
			for i, s := range slots {
				if s.pool == pref && fills[i] < 1 {
					out[idx] = time.Date(s.day.Year(), s.day.Month(), s.day.Day(), s.hour, 0, 0, 0, time.UTC)
					fills[i]++
					placed = true
					break
				}
			}
			if !placed {
				addDay()
			}
		}
	}

	for _, idx := range order {
		place(idx, prefer(&fixtures[idx]))
	}
	return out
}
