package lifecycle

import (
	"math"
	"testing"
)

func TestRetireProbabilityCurve(t *testing.T) {
	cases := []struct {
		name     string
		age      int
		ability  int
		injury   int
		ambition int
		wantLo   float64
		wantHi   float64
	}{
		{"age 29 baseline zero", 29, 60, 40, 50, 0, 0},
		{"age 30 prime neutral 12.5%", 30, 60, 40, 50, 0.125, 0.125},
		{"age 31 prime neutral 25%", 31, 60, 40, 50, 0.25, 0.25},
		{"age 33 prime neutral 50%", 33, 60, 40, 50, 0.5, 0.5},
		{"age 36 prime neutral 87.5%", 36, 60, 40, 50, 0.875, 0.875},
		{"age 37 forced retire", 37, 60, 40, 50, 1.0, 1.0},
		{"age 40 forced retire", 40, 70, 50, 30, 1.0, 1.0},
		{"elite ability halves never forced under 37", 36, 80, 40, 50, 0.875 * 0.6, 0.875*0.6 + 1e-9},
		{"low ability jacks curve", 30, 40, 40, 50, 0.125 * 1.5, 0.125*1.5 + 1e-9},
		{"injury prone jacks curve", 30, 60, 60, 50, 0.125 * 1.3, 0.125*1.3 + 1e-9},
		{"unambitious jacks curve", 30, 60, 40, 20, 0.125 * 1.3, 0.125*1.3 + 1e-9},
		{"high ambition damps curve", 30, 60, 40, 90, 0.125 * 0.7, 0.125*0.7 + 1e-9},
		{"age 30 all jacks product", 30, 40, 90, 10, 0.125 * 1.5 * 1.3 * 1.3, 0.125*1.5*1.3*1.3 + 1e-9},
		{"age 36 all jacks clamps to 1", 36, 40, 90, 10, 1.0, 1.0},
	}
	for _, c := range cases {
		got := RetireProbability(c.age, c.ability, c.injury, c.ambition)
		if got < c.wantLo || got > c.wantHi {
			t.Errorf("%s: RetireProbability(%d, %d, %d, %d) = %v, want in [%v, %v]",
				c.name, c.age, c.ability, c.injury, c.ambition, got, c.wantLo, c.wantHi)
		}
		if got < 0 || got > 1 {
			t.Errorf("%s: out of [0,1]: %v", c.name, got)
		}
	}
}

func TestRetireProbabilityMonotoneWithAge(t *testing.T) {
	prev := -1.0
	for age := 29; age <= 40; age++ {
		p := RetireProbability(age, 60, 40, 50)
		if p+1e-9 < prev {
			t.Fatalf("curve not monotone at age %d: %v then %v", age, prev, p)
		}
		prev = p
	}
}

func TestRetireProbabilityMitigatingMadsStayBounded(t *testing.T) {
	// The absolute worst case — age 36 with every jack — must clamp to 1.0,
	// never exceed it, and a fleet elder already at 1.0 stays 1.0.
	if got := RetireProbability(36, 20, 100, 1); math.Abs(got-1.0) > 1e-12 {
		t.Fatalf("worst-case 36-year-old = %v, want clamped 1.0", got)
	}
}
