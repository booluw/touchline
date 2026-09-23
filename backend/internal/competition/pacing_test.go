package competition

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestScheduledAtFromDayPacing locks the IM03 day-map formula: the k-th
// matchday sits at day floor((k-1) * daysPerWeek / matchdaysPerWeek) + 1 after
// the anchor, so a league never plays consecutive matchdays beyond its pace.
func TestScheduledAtFromDayPacing(t *testing.T) {
	anchor, err := time.Parse(time.RFC3339, "2026-06-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse anchor: %v", err)
	}
	offset := func(k, d, m int) int {
		return daysBetween(anchor, scheduledAtFromDay(anchor, k, d, m, 15))
	}
	assertOffsets := func(name string, d, m int, want []int) {
		t.Helper()
		got := make([]int, 0, len(want))
		for k := 1; k <= len(want); k++ {
			got = append(got, offset(k, d, m))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s (d=%d,m=%d) matchday %d day = %d, want %d", name, d, m, i+1, got[i], want[i])
			}
		}
	}

	// Default pacing: 3 matchdays in a 7-game-day week → 1,3,5,8,10,12.
	assertOffsets("default 3/7", 7, 3, []int{1, 3, 5, 8, 10, 12})
	// Denser: 4 matchdays in 7 days → 1,2,4,6,8,9,11,13 (floor formula).
	assertOffsets("dense 4/7", 7, 4, []int{1, 2, 4, 6, 8, 9, 11, 13})
	// Short week drawn from world calendar.days_per_week=5 → 1,2,4,6,7,9.
	assertOffsets("world week 5", 5, 3, []int{1, 2, 4, 6, 7, 9})
}

func TestKickoffHourRotation(t *testing.T) {
	leagueID := uuid.New()
	hours := []int{15, 18, 20}
	seed := int64(42)

	// The rotation is deterministic for a fixed seed/league, and cycles the full
	// list across consecutive matchdays, so a season varies kickoff times.
	var first int
	for k := 1; k <= len(hours); k++ {
		h := kickoffHour(seed, leagueID, hours, k)
		seen := map[int]bool{}
		for _, hh := range hours {
			seen[hh] = true
		}
		if !seen[h] {
			t.Fatalf("matchday %d kickoff = %d, outside rotation %v", k, h, hours)
		}
		if k == 1 {
			first = h
		} else if h == first {
			t.Fatalf("matchday %d repeated matchday-1 kickoff %d; want rotation to vary", k, h)
		}
	}
	if got := kickoffHour(seed, leagueID, hours, 1); got != first {
		t.Fatalf("same matchday kickoff changed across reads: %d vs %d (determinism)", got, first)
	}
	// Wrap-around: matchday 4 reuses matchday 1's slot.
	if got := kickoffHour(seed, leagueID, hours, 4); got != first {
		t.Fatalf("matchday 4 kickoff = %d, want wrap to %d", got, first)
	}
	// Empty rotation degenerates to the legacy fixed 19:00 slot.
	if got := kickoffHour(seed, leagueID, nil, 1); got != KickoffHourUTC {
		t.Fatalf("empty rotation kickoff = %d, want %d", got, KickoffHourUTC)
	}
}

func TestNormalizeHours(t *testing.T) {
	if got := normalizeHours([]int{25, 15, 17, -3, 15, 20}); len(got) != 3 || got[0] != 15 || got[1] != 17 || got[2] != 20 {
		t.Fatalf("normalize = %v, want [15 17 20] (dedupe 15, drop out-of-range)", got)
	}
	if got := normalizeHours(nil); len(got) != 0 {
		t.Fatalf("empty normalize = %v, want empty", got)
	}
}
