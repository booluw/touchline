package development

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func seed(field string, skills map[string]int) Input {
	return Input{
		PlayerID:          uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Age:               25,
		Position:          "ST",
		FacilityLevel:     5,
		Skills:            skills,
		Potential:         0,
		ExpansionsLeft:    ExpansionBudget,
		Professionalism:   75,
		PlayingTimePct:    0.8,
		SeasonAvgRating:    7.5,
		ConsecutiveStagnant: 0,
		WeekTick:           120,
	}
}

func TestAgeCurveOrdering(t *testing.T) {
	skills := map[string]int{"pace": 70, "passing": 70}
	jr := seed("youth", skills)
	jr.Age = 19
	pr := seed("prime", skills)
	vet := seed("veteran", skills)
	vet.Age = 32

	jrO, prO, vetO := Evaluate(jr), Evaluate(pr), Evaluate(vet)
	if jrO.Multiplier("pace") <= prO.Multiplier("pace") {
		t.Fatalf("youth pace growth must exceed prime: %.3f <= %.3f", jrO.Multiplier("pace"), prO.Multiplier("pace"))
	}
	if vetO.Multiplier("pace") >= prO.Multiplier("pace") {
		t.Fatalf("veteran physical growth must be below prime: %.3f >= %.3f", vetO.Multiplier("pace"), prO.Multiplier("pace"))
	}
	// Veterans keep growing mentally faster than they grow technically.
	// "composure" is a mental key; "pace" is physical.
	if !(vetO.Multiplier("composure") > vetO.Multiplier("pace")) {
		t.Fatalf("veteran mental growth must exceed their physical growth: %.3f <= %.3f", vetO.Multiplier("composure"), vetO.Multiplier("pace"))
	}
}

func TestMinutesAndStagnation(t *testing.T) {
	skills := map[string]int{"passing": 70}
	starter := seed("starter", skills)
	starter.PlayingTimePct = 1.0

	bench := seed("bench", skills)
	bench.PlayingTimePct = 0.05
	bench.ConsecutiveStagnant = 6

	benchO := Evaluate(bench)
	starterO := Evaluate(starter)
	if benchO.Multiplier("passing") >= starterO.Multiplier("passing") {
		t.Fatalf("stagnating bench player must grow slower than a starter: %.3f >= %.3f", benchO.Multiplier("passing"), starterO.Multiplier("passing"))
	}
	if got := Evaluate(bench).Stagnating; !got {
		t.Fatal("bench player with 6 stagnant weeks must be flagged stagnating")
	}
}

func TestStagnationCounterChains(t *testing.T) {
	in := seed("stagnant", map[string]int{"passing": 70})
	in.PlayingTimePct = 0.05
	in.ConsecutiveStagnant = 2

	o1 := Evaluate(in)
	if o1.State.ConsecutiveStagnant != 3 {
		t.Fatalf("week 1 stagnant count = %d, want 3", o1.State.ConsecutiveStagnant)
	}

	in.ConsecutiveStagnant = o1.State.ConsecutiveStagnant
	o2 := Evaluate(in)
	if o2.State.ConsecutiveStagnant != 4 {
		t.Fatalf("week 2 stagnant count = %d, want 4", o2.State.ConsecutiveStagnant)
	}

	if !o2.Stagnating {
		t.Fatal("week 2 must be stagnating")
	}
	// Escaping stagnation resets the counter.
	in2 := seed("back", map[string]int{"passing": 70})
	in2.ConsecutiveStagnant = o2.State.ConsecutiveStagnant
	in2.PlayingTimePct = 0.9
	if got := Evaluate(in2).State.ConsecutiveStagnant; got != 0 {
		t.Fatalf("counter must reset to 0 when minutes return, got %d", got)
	}
}

func TestPotentialFactorAtCeiling(t *testing.T) {
	skills := map[string]int{"passing": 70}
	capped := seed("capped", skills)
	capped.Potential = 71 // overall 70 → headroom 1
	capped.SeasonAvgRating = 5.5 // not elite: no flex, growth trickles

	open := seed("open", skills)
	open.Potential = 90 // headroom 20

	cappedO, openO := Evaluate(capped), Evaluate(open)
	for _, key := range []string{"passing", "pace"} {
		if cappedO.Multiplier(key) > openO.Multiplier(key) {
			t.Fatalf("capped growth must be below open growth for %s: %.3f > %.3f", key, cappedO.Multiplier(key), openO.Multiplier(key))
		}
	}
	if cappedO.PotentialChanged {
		t.Fatal("non-elite capped player must not expand")
	}
}

func TestPotentialExpansion(t *testing.T) {
	skills := map[string]int{"passing": 90, "pace": 90, "composure": 90}
	in := seed("wonderkid", skills)
	in.Age = 19
	in.Potential = 92              // overall ~90 → headroom ~2, at ceiling
	in.ExpansionsLeft = 3
	in.SeasonAvgRating = 8.4       // bump 1 + youth extra 1 = +2

	out := Evaluate(in)
	if !out.PotentialChanged {
		t.Fatal("elite youth at ceiling must flex potential")
	}
	if out.Potential != 94 {
		t.Fatalf("potential = %d, want 94", out.Potential)
	}
	if out.State.ExpansionsLeft != 2 {
		t.Fatalf("expansions left = %d, want 2", out.State.ExpansionsLeft)
	}
	if out.Locked {
		t.Fatal("budget remains: ceiling must NOT lock")
	}
	// The fresh headroom reopens growth for the week.
	if out.Multiplier("pace") <= cappedMultiplier(in) {
		t.Fatalf("post-expansion growth must exceed trickle: %.3f", out.Multiplier("pace"))
	}
}

func TestPotentialLockBudgetExhausted(t *testing.T) {
	skills := map[string]int{"pace": 90, "passing": 90, "composure": 90}
	in := seed("maxed", skills)
	in.Age = 20
	in.Potential = 91
	in.ExpansionsLeft = 1
	in.SeasonAvgRating = 9.2 // bump 2 + youth 1 = +3

	out := Evaluate(in)
	if !out.Locked {
		t.Fatal("last expansion must lock the ceiling")
	}
	if out.LockedWeek != 120 {
		t.Fatalf("locked week = %d, want 120", out.LockedWeek)
	}
	if out.State.LockedWeek != 120 || !out.State.Locked {
		t.Fatal("state must carry the lock")
	}
	if out.State.ExpansionsLeft != 0 {
		t.Fatalf("expansions left = %d, want 0", out.State.ExpansionsLeft)
	}
}

func TestPotentialLockedByAge(t *testing.T) {
	in := seed("senior", map[string]int{"passing": 80})
	in.Age = 28
	in.Potential = 84

	out := Evaluate(in)
	if !out.Locked {
		t.Fatal("age past the flex window must lock the ceiling")
	}
	// No expansion happens for an old player regardless of form.
	if out.PotentialChanged {
		t.Fatal("age-locked player must not expand")
	}
}

func TestLegacyNoCeiling(t *testing.T) {
	in := seed("legacy", map[string]int{"passing": 70})
	in.Potential = 0

	out := Evaluate(in)
	if out.PotentialChanged || out.Locked {
		t.Fatal("legacy player without ceiling data must not change or lock")
	}
	if got := out.Multiplier("passing"); got <= 0 {
		t.Fatalf("legacy growth multiplier must stay positive, got %v", got)
	}
}

func TestDeterministic(t *testing.T) {
	in := seed("deterministic", map[string]int{"passing": 80, "pace": 70, "composure": 75})
	in.Age = 18
	in.Potential = 79
	in.ConsecutiveStagnant = 3
	in.PlayingTimePct = 0.35

	a := Evaluate(in)
	b := Evaluate(in)
	if a.Potential != b.Potential || a.Locked != b.Locked ||
		a.State.ConsecutiveStagnant != b.State.ConsecutiveStagnant {
		t.Fatal("deterministic outcome violated")
	}
	for _, key := range []string{"passing", "pace", "finishing"} {
		if a.Multiplier(key) != b.Multiplier(key) {
			t.Fatalf("multiplier for %s differs across equal inputs", key)
		}
	}
}

func TestOverallBlend(t *testing.T) {
	skills := map[string]int{"passing": 100, "pace": 100, "composure": 100}
	field := Overall(skills, "ST")
	if math.Abs(field-100) > 1e-9 {
		t.Fatalf("uniform 100 field overall = %.2f, want 100", field)
	}
	// A goalkeeper's blend ignores outfield ball-skill keys entirely; a mixed
	// set reads only the goalkeeping category.
	mixed := map[string]int{"passing": 100, "reflexes": 50}
	if got := Overall(mixed, "GK"); math.Abs(got-50) > 1e-9 {
		t.Fatalf("GK overall must read only goalkeeping keys, got %.2f", got)
	}
	if got := Overall(nil, "ST"); got != 0 {
		t.Fatalf("empty skills overall = %.2f, want 0", got)
	}
}

func TestExplanationAuditable(t *testing.T) {
	in := seed("explain", map[string]int{"passing": 88})
	in.Age = 19
	in.Potential = 90
	in.PlayingTimePct = 0.72
	in.SeasonAvgRating = 7.2
	in.ConsecutiveStagnant = 5

	out := Evaluate(in)
	exp := out.Explanation
	if exp == nil || exp.Subject != "player_development" {
		t.Fatalf("explanation subject = %v", exp)
	}
	if len(exp.Factors) == 0 {
		t.Fatal("explanation must carry factors")
	}
	// The explanation should mention the deciding context.
	joined := ""
	for _, f := range exp.Factors {
		joined += f.Label + " "
	}
	for _, want := range []string{"Youth", "Minutes share 72%", "Potential 92", "Potential expanded"} {
		if !contains(joined, want) {
			t.Fatalf("explanation missing %q: %s", want, joined)
		}
	}

	// A visibly underplayed player surfaces the stagnation factor instead.
	bench := seed("bench-explained", map[string]int{"pace": 70})
	bench.PlayingTimePct = 0.10
	bench.ConsecutiveStagnant = 5
	bench.Potential = 0
	benchExp := Evaluate(bench).Explanation
	found := false
	for _, f := range benchExp.Factors {
		if contains(f.Label, "Stagnating") {
			found = true
		}
	}
	if !found {
		t.Fatal("stagnating explanation must carry the stagnation factor")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// cappedMultiplier reproduces the trickle path (headroom ≤ 2, no expansion).
func cappedMultiplier(in Input) float64 {
	return ageFactor(in.Age, "pace") * minutesFactor(in.PlayingTimePct, 0) *
		disciplineFactor(in.Professionalism) * facilityFactor(in.FacilityLevel) * 0.25
}