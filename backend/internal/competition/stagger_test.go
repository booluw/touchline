package competition

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func stagParams() *scheduleParamsResolved {
	return &scheduleParamsResolved{
		allowedWeekdays:  []int{5, 6, 7, 1}, // Fri Sat Sun Mon
		humanHours:       []int{18, 20},
		aiHours:          []int{12, 15, 17, 23},
		maxSimultaneous:  DefaultMaxSimultaneous,
		finalKickoffHour: DefaultFinalKickoffHour,
		staggered:        true,
	}
}

func mustDay(t *testing.T, rfc3339 string) time.Time {
	t.Helper()
	d, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatalf("parse %s: %v", rfc3339, err)
	}
	return d
}

// TestAssembleRoundKickoffsAnchorManyDays checks that a big round spills
// across the allowed weekdays starting on the anchor day, that every slot
// honors the cap, and that only allowed weekdays are used.
func TestAssembleRoundKickoffsAnchorManyDays(t *testing.T) {
	p := stagParams()
	anchor := mustDay(t, "2026-06-05T00:00:00Z") // Friday
	ties := make([]roundFixture, 0, 25)
	for i := 0; i < 25; i++ {
		ties = append(ties, roundFixture{home: uuid.New(), away: uuid.New(), human: i%2 == 0})
	}
	kicks := assembleRoundKickoffs(p, ties, anchor)
	if len(kicks) != 25 {
		t.Fatalf("kicks = %d, want 25", len(kicks))
	}

	days := map[string]int{}
	for _, k := range kicks {
		if !isAllowedWeekday(k, p.allowedWeekdays) {
			t.Fatalf("kickoff %v falls on a disallowed weekday", k)
		}
		days[k.Format("2006-01-02")]++
	}
	if days[anchor.Format("2006-01-02")] == 0 {
		t.Fatalf("no fixture on the anchor day %s", anchor.Format("2006-01-02"))
	}
	// One fixture per hour slot: the anchor day (Fri) holds its 6 slots and
	// the remaining 19 spill onto Sat/Sun/Mon/Fri.
	if days[anchor.Format("2006-01-02")] != 6 {
		t.Fatalf("anchor day fixtures = %d, want 6 (one per hour slot)", days[anchor.Format("2006-01-02")])
	}
	if len(days) < 2 {
		t.Fatalf("round played on %d day(s), want it to span multiple days", len(days))
	}

	// Per-slot cap: no (day, hour) pair hosts two fixtures.
	perSlot := map[string]int{}
	for _, k := range kicks {
		perSlot[k.Format("2006-01-02 15")]++
	}
	for slot, n := range perSlot {
		if n > 1 {
			t.Fatalf("slot %s holds %d fixtures, want at most 1", slot, n)
		}
	}
}

// TestAssembleRoundKickoffsPools locks the evening/day pool split: every
// human-involving tie lands on an evening hour, every AI-only tie on a day
// hour (the pools are disjoint in the defaults).
func TestAssembleRoundKickoffsPools(t *testing.T) {
	p := stagParams()
	anchor := mustDay(t, "2026-06-05T00:00:00Z")
	ties := []roundFixture{
		{home: uuid.New(), away: uuid.New(), human: false},
		{home: uuid.New(), away: uuid.New(), human: true},
		{home: uuid.New(), away: uuid.New(), human: true},
		{home: uuid.New(), away: uuid.New(), human: false},
		{home: uuid.New(), away: uuid.New(), human: true},
	}
	kicks := assembleRoundKickoffs(p, ties, anchor)
	isEvening := func(h int) bool {
		for _, hh := range p.humanHours {
			if hh == h {
				return true
			}
		}
		return false
	}
	for i, k := range kicks {
		h := k.Hour()
		if ties[i].human && !isEvening(h) {
			t.Fatalf("human tie %d kicked at %d, want an evening hour %v", i, h, p.humanHours)
		}
		if !ties[i].human && isEvening(h) {
			t.Fatalf("AI tie %d kicked at %d, want a day hour %v", i, h, p.aiHours)
		}
	}
}

// TestAssembleRoundKickoffsDeterminism verifies the assigner is pure: identical
// rounds reproduce identical kickoffs, and the same ties in a different input
// order map to the same per-pair slots (creation vs re-pace agreement).
func TestAssembleRoundKickoffsDeterminism(t *testing.T) {
	p := stagParams()
	anchor := mustDay(t, "2026-06-05T00:00:00Z")
	ties := make([]roundFixture, 0, 12)
	for i := 0; i < 12; i++ {
		ties = append(ties, roundFixture{home: uuid.New(), away: uuid.New(), human: i%3 == 0})
	}
	first := assembleRoundKickoffs(p, ties, anchor)
	again := assembleRoundKickoffs(p, ties, anchor)
	for i := range ties {
		if !first[i].Equal(again[i]) {
			t.Fatalf("kickoff %d differs across calls: %v vs %v", i, first[i], again[i])
		}
	}

	shuffled := make([]roundFixture, 0, len(ties))
	for _, i := range []int{6, 0, 11, 3, 7, 1, 10, 4, 8, 2, 9, 5} {
		shuffled = append(shuffled, ties[i])
	}
	byKey := func(src []roundFixture, kicks []time.Time) map[string]time.Time {
		m := map[string]time.Time{}
		for i, k := range kicks {
			m[fixtureKey(src[i].home, src[i].away)] = k
		}
		return m
	}
	want := byKey(ties, first)
	got := byKey(shuffled, assembleRoundKickoffs(p, shuffled, anchor))
	for key, k := range want {
		if !got[key].Equal(k) {
			t.Fatalf("tie %s drift across input order: %v vs %v", key, k, got[key])
		}
	}
}

// TestRoundAnchorWalk locks the multi-day round walk: round 1 begins the day
// after the season anchor, later rounds restart at least two game-days after
// the previous round's last kickoff.
func TestRoundAnchorWalk(t *testing.T) {
	wd := []int{5, 6, 7, 1}
	anchor := mustDay(t, "2026-06-03T00:00:00Z") // Wednesday

	first := roundAnchorFirst(anchor, wd)
	if got := first.Format("2006-01-02"); got != "2026-06-05" {
		t.Fatalf("round 1 anchor = %s, want 2026-06-05 (first allowed after Thu)", got)
	}

	// A round whose last kickoff is Sunday 2026-06-07 (or Mon 8) restarts no
	// earlier than the first allowed weekday >= 2 days later.
	lastSun, err := time.Parse(time.RFC3339, "2026-06-07T23:15:00Z")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	next := roundAnchorNext(lastSun, wd)
	if got := next.Format("2006-01-02"); got != "2026-06-12" {
		t.Fatalf("round after Sun 7 = %s, want 2026-06-12 (Sun+2 -> Fri)", got)
	}

	// roundLastDay picks the latest calendar day among a round's kickoffs.
	round := []time.Time{
		mustDay(t, "2026-06-05T18:00:00Z"),
		mustDay(t, "2026-06-06T15:00:00Z"),
		mustDay(t, "2026-06-05T20:00:00Z"),
	}
	if got := roundLastDay(round).Format("2006-01-02"); got != "2026-06-06" {
		t.Fatalf("roundLastDay = %s, want 2026-06-06", got)
	}
}
