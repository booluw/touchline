package matchsim

import (
	"math"
	"testing"
)

// TestXGDeterministicAndCalibrated pins IM58: the same seed gives the same
// xG, and across many seeds expected goals track actual goals (xG is the
// exact per-chance goal probability, so the means converge).
func TestXGDeterministicAndCalibrated(t *testing.T) {
	a := Simulate(sampleOptions(goldenSeed))
	b := Simulate(sampleOptions(goldenSeed))
	if a.HomeXG != b.HomeXG || a.AwayXG != b.AwayXG {
		t.Fatalf("xG not deterministic: %v/%v vs %v/%v", a.HomeXG, a.AwayXG, b.HomeXG, b.AwayXG)
	}

	const n = 4000
	var goals, xg float64
	for seed := int64(1); seed <= n; seed++ {
		res := Simulate(sampleOptions(seed))
		if res.HomeXG < 0 || res.AwayXG < 0 {
			t.Fatalf("seed %d: negative xG", seed)
		}
		goals += float64(res.HomeGoals + res.AwayGoals)
		xg += res.HomeXG + res.AwayXG
	}
	meanGoals, meanXG := goals/n, xg/n
	if math.Abs(meanGoals-meanXG) > 0.1*meanGoals {
		t.Fatalf("xG miscalibrated: mean goals %.3f vs mean xG %.3f", meanGoals, meanXG)
	}
	t.Logf("mean goals %.3f, mean xG %.3f", meanGoals, meanXG)
}
