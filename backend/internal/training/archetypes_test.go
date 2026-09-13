package training

import (
	"math"
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

func feq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestGrowthScale(t *testing.T) {
	cases := []struct {
		age int
		key string
		exp float64
	}{
		{16, "passing", 1.8},
		{21, "passing", 1.8},
		{22, "passing", 1.0},
		{29, "passing", 1.0},
		{34, "composure", 1.2}, // 30+ mental
		{34, "passing", 1.0},   // 30+ non-mental
		{34, "decision_making", 1.2},
		{35, "pace", 1.0}, // 30+ physical-key pace stays 1.0
	}
	for _, tc := range cases {
		if got := growthScale(tc.age, tc.key); got != tc.exp {
			t.Errorf("growthScale(%d, %q) = %v, want %v", tc.age, tc.key, got, tc.exp)
		}
	}
}

func TestVeteranDecay(t *testing.T) {
	if vd := veteranDecay(29, ArchetypeTechnical); vd != nil {
		t.Errorf("veteranDecay(29) = %v, want nil", vd)
	}
	if vd := veteranDecay(30, ArchetypePhysical); vd != nil {
		t.Errorf("veteranDecay(30, physical) = %v, want nil", vd)
	}
	vd := veteranDecay(31, ArchetypeTechnical)
	if vd["pace"] != -0.05 || vd["stamina"] != -0.05 {
		t.Errorf("veteranDecay(31) = %v, want pace/stamina -0.05", vd)
	}
}

func TestAttrDeltaScaling(t *testing.T) {
	a, ok := archetypeFor(ArchetypeTechnical)
	if !ok {
		t.Fatal("technical archetype missing")
	}
	if got := attrDelta(a, 18, "passing"); got != 0.54 { // +0.3 × 1.8 youth
		t.Errorf("passing@18 = %v, want 0.54", got)
	}
	if got := attrDelta(a, 34, "composure"); got != 0.24 { // +0.2 × 1.2 veteran mental
		t.Errorf("composure@34 = %v, want 0.24", got)
	}
	if got := attrDelta(a, 25, "strength"); got != -0.1 { // decay never age-scaled
		t.Errorf("strength@25 = %v, want -0.1", got)
	}
	if got := attrDelta(a, 25, "pace"); got != 0 {
		t.Errorf("untouched pace = %v, want 0", got)
	}
}

func TestApplyDeltaClampsAndRounds(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	if got := applyDelta(5, 0.3, rng); got != 5 && got != 6 {
		t.Errorf("applyDelta = %d, want 5 or 6", got)
	}
	if got := applyDelta(1, -1, rng); got != 1 {
		t.Errorf("lower clamp = %d, want 1", got)
	}
	if got := applyDelta(100, 1, rng); got != 100 {
		t.Errorf("upper clamp = %d, want 100", got)
	}
	if got := applyDelta(50, 0, rng); got != 50 {
		t.Errorf("zero delta = %d, want 50", got)
	}
}

// TestApplyDeltaPreservesExpectation checks the seeded binary rounding keeps
// the long-run mean equal to the fractional delta.
func TestApplyDeltaPreservesExpectation(t *testing.T) {
	const n = 200000
	rng := rand.New(rand.NewSource(42))
	start := 10
	hits := 0
	for i := 0; i < n; i++ {
		if applyDelta(start, 0.3, rng) == start+1 {
			hits++
		}
	}
	got := float64(hits) / n
	if got < 0.29 || got > 0.31 {
		t.Errorf("empirical p = %v, want ~0.3", got)
	}
}

func TestApplyConditionTechnical(t *testing.T) {
	a, _ := archetypeFor(ArchetypeTechnical)
	base := defaultCondition(uuid.Nil, 50)

	// Midfielder/winger gets the sharpness bonus (+0.05).
	cm := applyCondition(base, a, "CM")
	if !feq(cm.Fatigue, 0.04) || !feq(cm.Sharpness, 0.57) || !feq(cm.TacticalFamiliarity, 0.52) {
		t.Errorf("CM = %+v, want fatigue 0.04 sharpness 0.57 fam 0.52 (bonus)", cm)
	}
	if !feq(cm.Fitness, 1) {
		t.Errorf("CM fitness = %v, want 1 (baseline 1 + 0.01 clamped)", cm.Fitness)
	}

	// Goalkeeper does not get the bonus: sharpness stays +0.02.
	gk := applyCondition(base, a, "GK")
	if !feq(gk.Sharpness, 0.52) {
		t.Errorf("GK sharpness = %v, want 0.52 (no bonus)", gk.Sharpness)
	}
}

func TestApplyConditionRecoveryAndPhysical(t *testing.T) {
	rec, _ := archetypeFor(ArchetypeRecovery)
	base := defaultCondition(uuid.Nil, 50)
	c := applyCondition(base, rec, "ST")
	if c.Fatigue != 0 || !feq(c.Fitness, 1) || !feq(c.Sharpness, 0.5) {
		t.Errorf("recovery = %+v, want fatigue 0 fitness 1 sharpness 0.5", c)
	}
	if c.InjuryRisk > 0.25 || c.InjuryRisk <= 0.21 {
		t.Errorf("recovery injury_risk = %v, want ~0.22", c.InjuryRisk)
	}
	if !feq(c.TacticalFamiliarity, 0.55) {
		t.Errorf("recovery familiarity = %v, want 0.55", c.TacticalFamiliarity)
	}

	// Physical: +0.05 × 1.40 fatigue, fitness −0.02/wk, sharpness +0.04/wk.
	phys, _ := archetypeFor(ArchetypePhysical)
	p := applyCondition(applyCondition(base, phys, "GK"), phys, "GK")
	if !feq(p.Fatigue, 0.14) || !feq(p.Fitness, 0.96) || !feq(p.Sharpness, 0.58) {
		t.Errorf("physical 2 weeks = %+v, want fatigue 0.14 fitness 0.96 sharpness 0.58", p)
	}
	if !feq(p.TacticalFamiliarity, 0.54) {
		t.Errorf("physical familiarity = %v, want 0.54", p.TacticalFamiliarity)
	}
}

func TestDefaultConditionBaseline(t *testing.T) {
	c := defaultCondition(uuid.Nil, 50)
	if c.Fatigue != 0 || c.Fitness != 1 || c.Sharpness != 0.5 || c.TacticalFamiliarity != 0.5 {
		t.Errorf("baseline = %+v", c)
	}
	if c.InjuryRisk != 0.25 {
		t.Errorf("injury_risk = %v, want 0.25 (50/200)", c.InjuryRisk)
	}
	if high := defaultCondition(uuid.Nil, 300).InjuryRisk; high != 1 {
		t.Errorf("clamped high injury_risk = %v, want 1", high)
	}
	if low := defaultCondition(uuid.Nil, 0).InjuryRisk; low != 0 {
		t.Errorf("clamped low injury_risk = %v, want 0", low)
	}
}

func TestDistinctKeysSpansGrowthAndDecay(t *testing.T) {
	for _, key := range Archetypes {
		a, ok := archetypeFor(key)
		if !ok {
			t.Fatalf("archetype %q missing", key)
		}
		keys := distinctKeys(a)
		seen := make(map[string]bool)
		for _, k := range keys {
			seen[k] = true
		}
		for k := range a.Growth {
			if !seen[k] {
				t.Errorf("%s: growth key %s missing from distinctKeys", key, k)
			}
		}
		for k := range a.Decay {
			if !seen[k] {
				t.Errorf("%s: decay key %s missing from distinctKeys", key, k)
			}
		}
	}
}

// TestConditionRounding sanity-checks numerical expectations stay within the
// documented [0,1] band across a long-run of weeks for every archetype.
func TestConditionRounding(t *testing.T) {
	for _, key := range Archetypes {
		a, _ := archetypeFor(key)
		c := defaultCondition(uuid.Nil, 80)
		for i := 0; i < 52; i++ {
			c = applyCondition(c, a, "AM")
			if c.Fatigue < 0 || c.Fatigue > 1 || c.Fitness < 0 || c.Fitness > 1 ||
				c.Sharpness < 0 || c.Sharpness > 1 || c.InjuryRisk < 0 || c.InjuryRisk > 1 ||
				c.TacticalFamiliarity < 0 || c.TacticalFamiliarity > 1 {
				t.Fatalf("%s: condition escaped [0,1] at week %d: %+v", key, i, c)
			}
		}
	}
}
