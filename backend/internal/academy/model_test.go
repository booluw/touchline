package academy

import (
	"testing"

	"github.com/touchline/backend/internal/board"
)

func TestProspectCountForTier(t *testing.T) {
	cases := map[int]int{1: 5, 2: 7, 3: 9, 4: 11, 5: 13}
	for tier, want := range cases {
		if got := ProspectCountForTier(tier); got != want {
			t.Fatalf("tier %d count = %d, want %d", tier, got, want)
		}
	}
	// Out-of-band tiers clamp rather than panic.
	if got := ProspectCountForTier(0); got != 5 {
		t.Fatalf("tier 0 should clamp to 1: %d", got)
	}
	if got := ProspectCountForTier(9); got != 13 {
		t.Fatalf("tier 9 should clamp to 5: %d", got)
	}
}

func TestQualityOffsetCapped(t *testing.T) {
	if got := QualityOffsetForTier(1, 10); got != 4 { // 3 + 1
		t.Fatalf("tier1 rep10 offset = %d, want 4", got)
	}
	if got := QualityOffsetForTier(5, 100); got != 20 { // 15 + 10 -> cap
		t.Fatalf("tier5 rep100 offset = %d, want 20", got)
	}
}

func TestTalentOddsProfilesSum(t *testing.T) {
	for tier := MinTier; tier <= MaxTier; tier++ {
		o := TalentOddsForTier(tier)
		sum := o.Journeyman + o.TopProspect + o.Wonderkid + o.Generational
		if sum != 1000 {
			t.Fatalf("tier %d odds sum = %d, want 1000", tier, sum)
		}
	}
	// Higher tiers must never be worse: the rare-or-better band
	// (wonderkid + generational) is monotonically non-decreasing.
	prevW := -1
	for tier := MinTier; tier <= MaxTier; tier++ {
		o := TalentOddsForTier(tier)
		if o.Wonderkid+o.Generational < prevW {
			t.Fatalf("tier %d wonderkid+generational regressed", tier)
		}
		prevW = o.Wonderkid + o.Generational
	}
}

func TestSeasonForDay(t *testing.T) {
	if got := SeasonForDay(0); got != 0 {
		t.Fatalf("day 0 season = %d", got)
	}
	if got := SeasonForDay(DaysPerSeason - 1); got != 0 {
		t.Fatalf("last day of season 0 = %d", got)
	}
	if got := SeasonForDay(DaysPerSeason); got != 1 {
		t.Fatalf("first day of season 1 = %d", got)
	}
	if got := SeasonForDay(-5); got != 0 {
		t.Fatalf("negative day should clamp: %d", got)
	}
}

func TestAnnualAndUpgradeCostTables(t *testing.T) {
	if AnnualCostByTier[1] >= AnnualCostByTier[5] {
		t.Fatal("annual cost must rise with tier")
	}
	if UpgradeCostByTier[5] <= UpgradeCostByTier[2] {
		t.Fatal("upgrade cost must rise with tier")
	}
}

func TestSentimentAfterShutdown(t *testing.T) {
	cases := []struct {
		name    string
		current int
		want    int
	}{
		{"full penalty above the floor", 100, 85},
		{"mid-range takes the full hit", 50, 35},
		{"clamp binds just above the floor", 20, board.SupporterSentimentMin},
		{"already at the floor is a no-op", board.SupporterSentimentMin, board.SupporterSentimentMin},
		{"legacy sub-floor value is lifted to the floor", 10, board.SupporterSentimentMin},
		{"legacy zero is lifted to the floor", 0, board.SupporterSentimentMin},
	}
	for _, c := range cases {
		if got := sentimentAfterShutdown(c.current); got != c.want {
			t.Errorf("%s: sentimentAfterShutdown(%d) = %d, want %d", c.name, c.current, got, c.want)
		}
	}
}

// The shutdown must never leave sentiment below the board EWMA's own minimum,
// otherwise the next board review blends the value back up and the hit is not
// the one the explanation claims.
func TestSentimentAfterShutdownRespectsEWMAFloor(t *testing.T) {
	for cur := -20; cur <= 100; cur++ {
		got := sentimentAfterShutdown(cur)
		if got < board.SupporterSentimentMin {
			t.Fatalf("sentimentAfterShutdown(%d) = %d, below EWMA floor %d", cur, got, board.SupporterSentimentMin)
		}
		if got > board.SupporterSentimentMax {
			t.Fatalf("sentimentAfterShutdown(%d) = %d, above EWMA ceiling %d", cur, got, board.SupporterSentimentMax)
		}
	}
}
