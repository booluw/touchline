package lifecycle

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

func cand(id byte, pos string, overall int) autoFillCandidate {
	return autoFillCandidate{ID: uuid.New(), Position: pos, Overall: overall}
}

// squadCounts converts a squad's position list into per-group counts.
func squadCounts(positions ...string) map[string]int {
	counts := map[string]int{}
	for _, p := range positions {
		counts[positionGroup(p)]++
	}
	return counts
}

func TestOrderAutoFillFillsPositionGapsFirst(t *testing.T) {
	// A squad that is only below its 2-GK quota (every other group stocked)
	// gets goalkeepers before outfielders, even though better-rated forwards
	// exist in the pool.
	squad := squadCounts(
		"CB", "CB", "LB", "RB", "CB", "CB", "LB",
		"CM", "CM", "CM", "AM", "RM", "CM", "LM",
		"ST", "LW", "RW", "ST", "ST",
	) // GK=0, DEF=7, MID=7, FWD=6 => only GK gap
	pool := []autoFillCandidate{
		cand(1, "ST", 85),
		cand(2, "GK", 70),
		cand(3, "GK", 65),
		cand(4, "CB", 60),
	}
	allNeeded := 24 - 19 // 5
	ordered := orderAutoFillCandidates(pool, squad, allNeeded, rand.New(rand.NewSource(1)))
	if len(ordered) < 2 {
		t.Fatalf("expected the GK fills, got %d: %+v", len(ordered), ordered)
	}
	if ordered[0].Position != "GK" || ordered[1].Position != "GK" {
		t.Fatalf("GK gap not filled first: %+v", ordered)
	}
}

func TestOrderAutoFillGapDepthOrdersGroups(t *testing.T) {
	// Squad has 0 GK, 0 MID but many defenders and forwards: the MID quota
	// deficit (7) is deeper than GK (2), so the first pick fills MID.
	squad := squadCounts("CB", "CB", "CB", "CB", "ST", "ST") // DEF=4, FWD=2, GK=0, MID=0
	pool := []autoFillCandidate{
		cand(1, "ST", 90),
		cand(2, "CM", 60),
		cand(3, "GK", 80),
	}
	ordered := orderAutoFillCandidates(pool, squad, 3, rand.New(rand.NewSource(5)))
	if got := ordered[0].Position; got != "CM" {
		t.Fatalf("deepest gap (MID) must fill first, got %s: %+v", got, ordered)
	}
}

func TestOrderAutoFillDoesNotOverfill(t *testing.T) {
	need := 18
	pool := make([]autoFillCandidate, 40)
	for i := range pool {
		pool[i] = cand(byte(i%251), []string{"CB", "CM", "LW", "ST"}[i%4], 50+i%10)
	}
	ordered := orderAutoFillCandidates(pool, squadCounts("GK", "GK", "CB", "CB", "CM", "ST"), need, rand.New(rand.NewSource(2)))
	if len(ordered) > need {
		t.Fatalf("overfilled: got %d picks for need %d", len(ordered), need)
	}
	if len(ordered) != need {
		t.Fatalf("expected exactly %d picks when pool is plentiful, got %d", need, len(ordered))
	}
}

func TestOrderAutoFillPoolExhaustion(t *testing.T) {
	pool := []autoFillCandidate{cand(1, "GK", 60), cand(2, "ST", 55)}
	ordered := orderAutoFillCandidates(pool, squadCounts("GK"), 24, rand.New(rand.NewSource(3)))
	if len(ordered) != len(pool) {
		t.Fatalf("pool exhaustion: got %d picks, want %d", len(ordered), len(pool))
	}
}

func TestOrderAutoFillDeterministicUnderSeed(t *testing.T) {
	pool := make([]autoFillCandidate, 30)
	for i := range pool {
		pool[i] = cand(byte(i%251), []string{"CB", "LB", "DM", "AM", "RW"}[i%5], 45+i%15)
	}
	squad := squadCounts("GK", "CM", "ST")
	a := orderAutoFillCandidates(pool, squad, 21, rand.New(rand.NewSource(11)))
	b := orderAutoFillCandidates(pool, squad, 21, rand.New(rand.NewSource(11)))
	if len(a) != len(b) {
		t.Fatalf("determinism: length differs %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("determinism broken at index %d", i)
		}
	}
}

func TestTemplatePositionCoverage(t *testing.T) {
	// A club that already has one GK (quota 2) but no forwards gets the best
	// FWD first (FWD quota 6 is the deepest zero-count gap), then remaining
	// signings top up by overall.
	pool := []autoFillCandidate{
		cand(1, "GK", 90),
		cand(2, "GK", 89),
		cand(3, "GK", 88),
		cand(4, "ST", 70),
	}
	ordered := orderAutoFillCandidates(pool, squadCounts("GK"), 4, rand.New(rand.NewSource(4)))
	if len(ordered) != 4 {
		t.Fatalf("want 4 picks, got %d", len(ordered))
	}
	if ordered[0].Position != "ST" {
		t.Fatalf("open FWD group must be filled first: %+v", ordered)
	}
}

func TestPositionGroupMapping(t *testing.T) {
	cases := map[string]string{
		"GK": "GK",
		"CB": "DEF", "LB": "DEF", "RB": "DEF",
		"DM": "MID", "CM": "MID", "AM": "MID", "LM": "MID", "RM": "MID",
		"LW": "FWD", "RW": "FWD", "ST": "FWD",
	}
	for pos, want := range cases {
		if got := positionGroup(pos); got != want {
			t.Errorf("positionGroup(%q) = %q, want %q", pos, got, want)
		}
	}
}
