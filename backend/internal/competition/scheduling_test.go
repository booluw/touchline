package competition

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func parseDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return daysTruncate(d)
}

func iso(t *testing.T, s string) string {
	return parseDay(t, s).Format("2006-01-02")
}

func daysFrom(t *testing.T, anchor string, offset int) time.Time {
	t.Helper()
	return parseDay(t, anchor).AddDate(0, 0, offset)
}

// TestNormalizeWeekdays drives the weekday parser's normalizing behavior:
// out-of-range and duplicate entries are dropped, order is preserved.
func TestNormalizeWeekdays(t *testing.T) {
	cases := []struct {
		in   []int
		want []int
	}{
		{[]int{7, 1, 3, 3, 0, 8, 1}, []int{7, 1, 3}},
		{[]int{}, []int{}},
		{[]int{2, 4, 6}, []int{2, 4, 6}},
	}
	for _, tc := range cases {
		got := normalizeWeekdays(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("normalize(%v) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("normalize(%v) = %v, want %v", tc.in, got, tc.want)
			}
		}
	}
}

// TestNextAllowedWeekday checks the forward weekday walk.
func TestNextAllowedWeekday(t *testing.T) {
	cases := []struct {
		name     string
		start    int
		weekdays []int
		wantISO  string
	}{
		{"mon->wed allowed", 0, []int{3, 5}, "2026-09-09"}, // Wed 09
		{"mon->sun", 0, []int{7}, "2026-09-13"},
		{"mon->next mon (no extra)", 0, []int{1}, "2026-09-07"},
		{"on a fri, fri allowed same day", 4, []int{5}, "2026-09-11"},
	}
	for _, tc := range cases {
		got := nextAllowedWeekday(daysFrom(t, "2026-09-07T00:00:00Z", tc.start), tc.weekdays)
		if got.Format("2006-01-02") != tc.wantISO {
			t.Fatalf("%s: %v, want %s", tc.name, got.Format("2006-01-02"), tc.wantISO)
		}
	}
}

// TestPaceWeekdayMatchday locks the IM05 league weekday rule: matchday 1 is the
// earliest allowed weekday the day after the anchor; each later matchday is the
// earliest allowed weekday at least two game-days after its predecessor, so the
// two-day rest floor holds no matter how sparse the weekday set.
func TestPaceWeekdayMatchday(t *testing.T) {
	anchor := parseDay(t, "2026-06-01T00:00:00Z")
	cases := []struct {
		name     string
		weekdays []int
		want     []string // ISO dates for matchdays 1..n
	}{
		{"mon/wed/fri", []int{1, 3, 5}, []string{
			"2026-06-03", "2026-06-05", "2026-06-08", "2026-06-10", "2026-06-12",
		}},
		{"only saturday", []int{6}, []string{
			"2026-06-06", "2026-06-13", "2026-06-20",
		}},
		{"tue/thu", []int{2, 4}, []string{
			"2026-06-02", "2026-06-04", "2026-06-09", "2026-06-11",
		}},
	}
	for _, tc := range cases {
		got := []string{}
		for k := 1; k <= len(tc.want); k++ {
			got = append(got, paceWeekdayMatchday(anchor, k, tc.weekdays, 15).Format("2006-01-02"))
		}
		prev := time.Time{}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%s matchday %d = %s, want %s", tc.name, i+1, got[i], tc.want[i])
			}
			day, _ := time.Parse("2006-01-02", got[i])
			if !isAllowedWeekday(day, tc.weekdays) {
				t.Fatalf("%s: matchday %d (%s) not on an allowed weekday", tc.name, i+1, got[i])
			}
			if i > 0 && day.Sub(prev) < 2*24*time.Hour {
				t.Fatalf("%s: matchdays %d/%d too close (%s vs %s)", tc.name, i, i+1, got[i-1], got[i])
			}
			prev = day
		}
	}
}

// TestCupGapDeterminismAndBias checks cupGap's seeded determinism: the same
// inputs always pick the same gap (so anchored calendars replay), and the gap
// converges on the flat 2-day minimum away from the final while the final's
// two feeders are biased toward 3 close to the trophy.
func TestCupGapDeterminismAndBias(t *testing.T) {
	seed := int64(7)
	cupID := uuid.New()

	// Determinism.
	for total := 3; total <= 8; total++ {
		for round := 1; round <= total; round++ {
			a := cupGap(seed, cupID, round, total)
			b := cupGap(seed, cupID, round, total)
			if a != b {
				t.Fatalf("cupGap not deterministic (round %d/%d): %d vs %d", round, total, a, b)
			}
		}
	}

	// Distribution across the seeded sample: rounds far from the final (dist
	// >= 3) are always 2; the two feeders close to the trophy (dist 1 and
	// dist 2) can draw 3, with the nearer final feeder the more likely — and
	// the final round itself (dist 0) is never asked for a gap.
	samples := 20000
	count3 := map[int]int{}
	for across := 0; across < 50; across++ {
		s := int64(across)
		for i := 0; i < samples/50; i++ {
			for round := 1; round <= 4; round++ {
				if cupGap(s, cupID, round, 4) == 3 {
					count3[round]++
				}
			}
		}
	}
	if count3[2] == 0 || count3[3] == 0 {
		t.Fatalf("expected some 3-day gaps among the two feeders, got dist1=%d dist2=%d", count3[3], count3[2])
	}
	if count3[3] <= count3[2] {
		t.Fatalf("near-final feeder (dist 1) should draw 3 more often than dist 2: %d vs %d", count3[3], count3[2])
	}
	if count3[1] != 0 || count3[4] != 0 {
		t.Fatalf("dist>=3 rounds and the final must always be 2: rounds %d", count3)
	}
}

// TestDayClearance checks the league-day avoidance metric.
func TestDayClearance(t *testing.T) {
	leagueDays := []time.Time{
		parseDay(t, "2026-09-10T00:00:00Z"),
		parseDay(t, "2026-09-12T00:00:00Z"),
	}
	cases := []struct {
		day  string
		want int
	}{
		{"2026-09-10T00:00:00Z", 0}, // collision
		{"2026-09-09T00:00:00Z", 1},
		{"2026-09-08T00:00:00Z", 2},
		{"2026-09-11T00:00:00Z", 1},
		{"2026-09-15T00:00:00Z", 3},
	}
	for _, tc := range cases {
		got := dayClearance(parseDay(t, tc.day), leagueDays)
		if got != tc.want {
			t.Fatalf("clearance(%s) = %d, want %d", tc.day, got, tc.want)
		}
	}
}

// TestFindCupRoundDayChecks the round-placing heuristic: it never picks a
// league day while a free day fits in the window, keeps two days of rest from
// its successor, and honors the allowed weekday set.
func TestFindCupRoundDay(t *testing.T) {
	leagueDays := []time.Time{
		parseDay(t, "2026-09-10T00:00:00Z"),
		parseDay(t, "2026-09-12T00:00:00Z"),
		parseDay(t, "2026-09-14T00:00:00Z"),
	}
	next := parseDay(t, "2026-09-17T00:00:00Z")
	got := findCupRoundDay(parseDay(t, "2026-09-11T00:00:00Z"), next, leagueDays, nil)
	if len(got.Format("2006-01-02")) == 0 {
		t.Fatal("findCupRoundDay returned the zero time")
	}
	for _, ld := range leagueDays {
		if got.Equal(ld) {
			t.Fatalf("findCupRoundDay picked a league day: %s", got.Format("2006-01-02"))
		}
	}
	if got.After(parseDay(t, "2026-09-15T00:00:00Z")) {
		t.Fatalf("findCupRoundDay broke two-day rest from successor: %s", got.Format("2006-01-02"))
	}

	// Weekday-enforced: with only Wednesdays allowed, the pick must be a
	// Wednesday even though the nearest free day is another weekday.
	wed := findCupRoundDay(parseDay(t, "2026-09-08T00:00:00Z"), parseDay(t, "2026-09-21T00:00:00Z"), leagueDays, []int{3})
	if !isAllowedWeekday(wed, []int{3}) {
		t.Fatalf("weekday-enforced findCupRoundDay picked non-Wednesday: %s", wed.Format("2006-01-02"))
	}
}

// TestCupLadderDatesLocked exercises the pure round-planning inputs so IM05
// cup calendars stay anchored and internally consistent when the ladder is
// empty (no fixtures) — the legacy path.
func TestCupLadderDatesLocked(t *testing.T) {
	// cupLadder is untouched by IM05: plans carry nil dates until
	// planCupCalendar stamps them. This guards the struct-equality used by the
	// ladder tests (the Date pointer is omitted when nil).
	ladder, err := cupLadder(16, 8, 0)
	if err != nil {
		t.Fatalf("cupLadder: %v", err)
	}
	for _, rp := range ladder {
		if rp.Date != nil {
			t.Fatalf("round %d unexpectedly carries a date: %v", rp.Round, *rp.Date)
		}
	}
}

// daysFromDate returns the ISO string n days after the given ISO date,
// normalized to midnight UTC. Kept local so tests read like prose.
func daysFromDate(d string, n int) string {
	t, _ := time.Parse("2006-01-02", d)
	return t.AddDate(0, 0, n).Format("2006-01-02")
}