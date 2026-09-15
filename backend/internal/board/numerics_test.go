package board

import (
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestWeightsSumToOne(t *testing.T) {
	personas := []Persona{
		PersonaPatientOwner, PersonaDemandingOwner, PersonaFinancialConservative,
		PersonaAcademyOwner, PersonaPrestigeOwner, PersonaPoliticalBoard,
	}
	for _, p := range personas {
		w := weights(p)
		sum := 0.0
		for _, wi := range w {
			sum += wi
		}
		if sum < 0.9999 || sum > 1.0001 {
			t.Errorf("persona %s weights sum to %v, want 1.0", p, sum)
		}
	}
	if !personaIsKnown(PersonaPoliticalBoard) {
		t.Error("known persona not recognized")
	}
}

func TestTotalScoreSumsRounding(t *testing.T) {
	s := FactorScores{Performance: 80, Expectations: 60, Financial: 70,
		BoardRelationship: 55, ClubDNAAlignment: 50, SupporterSentiment: 65, Alternatives: 40}
	for _, p := range []Persona{PersonaPatientOwner, PersonaDemandingOwner, PersonaFinancialConservative,
		PersonaAcademyOwner, PersonaPrestigeOwner, PersonaPoliticalBoard} {
		total, deltas := totalScore(p, s)
		sum := 0
		for _, d := range deltas {
			sum += d
		}
		if total != sum {
			t.Errorf("persona %s total %d != sum of deltas %d", p, total, sum)
		}
	}
}

func TestExpectedFinish(t *testing.T) {
	for _, tc := range []struct {
		ambition, patience, want int
	}{
		{90, 40, 3},
		{70, 50, 5},
		{50, 80, 9}, // patient board grants ~1-2 places
		{30, 60, 10},
		{100, 90, 3}, // (110-100)/8 = 1.25 + 1.5 patient bonus = 2.75 → 3
		{10, 10, 13},
	} {
		if got := expectedFinish(tc.ambition, tc.patience); got != tc.want {
			t.Errorf("expectedFinish(%d,%d) = %d, want %d", tc.ambition, tc.patience, got, tc.want)
		}
	}
	if fn, pp := expectedFinish(0, 0), expectedFinish(0, 0); fn < 1 || pp < 1 {
		t.Errorf("finish out of band: %d", fn)
	}
}

func TestExpectedPoints(t *testing.T) {
	for finish, want := range map[int]int{1: 85, 3: 78, 5: 71, 9: 57, 13: 43} {
		got := expectedPoints(finish)
		if got < want-2 || got > want+2 {
			t.Errorf("expectedPoints(%d) = %d, want ~%d", finish, got, want)
		}
	}
}

func TestEvaluateFinish(t *testing.T) {
	finish := 8
	// Early season: 3 places of slack.
	if met, broken := evaluateFinish(11, finish, 0.2, false); !met || broken {
		t.Errorf("early slack should be met (pos 11 finish 8)")
	}
	// Mid season: 2 places of slack.
	if met, broken := evaluateFinish(10, finish, 0.6, false); !met || broken {
		t.Errorf("mid slack should be met (pos 10 finish 8)")
	}
	// Late season: 1 place of slack.
	if met, broken := evaluateFinish(9, finish, 0.9, false); !met || broken {
		t.Errorf("late slack should be met (pos 9 finish 8)")
	}
	// Clearly adrift breaks it any time.
	if met, broken := evaluateFinish(15, finish, 0.5, false); met || !broken {
		t.Errorf("position 15 should be broken, got met=%v broken=%v", met, broken)
	}
	// Season complete: strict.
	if met, broken := evaluateFinish(8, finish, 1, true); !met || broken {
		t.Errorf("finishing on target should be met")
	}
	if met, broken := evaluateFinish(9, finish, 1, true); met || !broken {
		t.Errorf("season-complete miss should be broken")
	}
}

func TestEvaluatePoints(t *testing.T) {
	target := 80
	// At 50% the scaled target is 40; 38 points is inside the window (unresolved).
	if met, broken := evaluatePoints(38, target, 0.5, false); met || broken {
		t.Errorf("38 vs scaled 40 should be unresolved")
	}
	// 30 points is >10 under → broken.
	if met, broken := evaluatePoints(30, target, 0.5, false); met || !broken {
		t.Errorf("30 vs scaled 40 should be broken")
	}
	// 45 points beats the scaled target → met.
	if met, broken := evaluatePoints(45, target, 0.5, false); !met || broken {
		t.Errorf("45 vs scaled 40 should be met")
	}
	// Season complete grades against the full target.
	if met, broken := evaluatePoints(79, target, 1, true); met || !broken {
		t.Errorf("79/80 at season end should be broken")
	}
}

func TestEvaluateWageStructure(t *testing.T) {
	budget := int64(25_000_000)
	// At target "0" allowance is budget * 1.05.
	if met, broken := evaluateWageStructure(budget, budget, 0); !met || broken {
		t.Errorf("at-budget should be met")
	}
	if met, broken := evaluateWageStructure(int64(float64(budget)*1.04), budget, 0); !met || broken {
		t.Errorf("+4%% inside tolerance should be met")
	}
	if met, broken := evaluateWageStructure(int64(float64(budget)*1.10), budget, 0); met || !broken {
		t.Errorf("+10%% should be broken")
	}
	// A negotiated tolerance of 30% makes +10% fine.
	if met, broken := evaluateWageStructure(int64(float64(budget)*1.10), budget, 30); !met || broken {
		t.Errorf("+10%% under 30%% tolerance should be met")
	}
	// No budget → nothing to grade.
	if met, broken := evaluateWageStructure(1_000_000, 0, 0); met || broken {
		t.Errorf("no budget should be unresolved")
	}
}

func TestEvaluateOperatingBalance(t *testing.T) {
	budget := int64(25_000_000)
	if met, broken := evaluateOperatingBalance(0, budget); !met || broken {
		t.Errorf("breakeven should be met")
	}
	if met, broken := evaluateOperatingBalance(int64(-float64(budget)*0.1), budget); met || broken {
		t.Errorf("small shortfall should be unresolved")
	}
	if met, broken := evaluateOperatingBalance(int64(-float64(budget)*0.3), budget); met || !broken {
		t.Errorf("loss beyond 25%% of wage budget should be broken")
	}
}

func TestSupporterBlend(t *testing.T) {
	// Clamped to [15,95].
	if got := supporterBlend(10, 90); got < SupporterSentimentMin {
		t.Errorf("blend below floor: %d", got)
	}
	if got := supporterBlend(95, 100); got > SupporterSentimentMax {
		t.Errorf("blend above ceiling: %d", got)
	}
	// EWMA moves toward the performance factor.
	low, hi := supporterBlend(50, 90), supporterBlend(50, 10)
	if low <= 50 || hi >= 50 {
		t.Errorf("EWMA direction wrong: low=%d hi=%d", low, hi)
	}
}

func TestNegotiation(t *testing.T) {
	// league_finish: smaller is better; proposal way off the window → invalid.
	if withinNegotiationWindow(TargetLeagueFinish, 8, 20) {
		t.Error("off-window finish proposal should be invalid")
	}
	if !withinNegotiationWindow(TargetLeagueFinish, 8, 10) {
		t.Error("±3 finish proposal should be in-window")
	}
	if withinNegotiationWindow(TargetLeagueFinish, 8, 3) {
		t.Error("out-of-band finish (<1) should be invalid")
	}
	// points: bigger is better; ±8 in-window.
	if !withinNegotiationWindow(TargetPointsTarget, 70, 75) {
		t.Error("±8 points proposal should be in-window")
	}
	if withinNegotiationWindow(TargetPointsTarget, 70, 55) {
		t.Error(">8 points proposal should be invalid")
	}
	// negotiationDelta: finish worsening is positive; points worsening negative.
	if d := negotiationDelta(TargetLeagueFinish, 8, 11); d != 3 {
		t.Errorf("finish demotion delta = %d, want 3", d)
	}
	if d := negotiationDelta(TargetPointsTarget, 70, 60); d != 10 {
		t.Errorf("points demotion delta = %d, want 10", d)
	}
	// patient owner tolerates 3; demanding owner tolerates 0.
	if t0, t3 := negotiationTolerance(PersonaDemandingOwner), negotiationTolerance(PersonaPatientOwner); t0 != 0 || t3 != 3 {
		t.Errorf("tolerances = %d/%d, want 0/3", t0, t3)
	}
}

func TestMandateSet(t *testing.T) {
	seeds := mandateSet(6, 67)
	if len(seeds) != 4 {
		t.Fatalf("mandate set = %d rows, want 4", len(seeds))
	}
	if seeds[0].Category != CatPrimary || seeds[0].TargetType != TargetLeagueFinish {
		t.Errorf("primary mandate wrong: %+v", seeds[0])
	}
	if seeds[1].Category != CatSecondary || seeds[1].TargetType != TargetPointsTarget {
		t.Errorf("secondary mandate wrong: %+v", seeds[1])
	}
}

func TestFactorScores(t *testing.T) {
	if got := performanceScore(nil, 5); got != 50 {
		t.Errorf("no-league performance = %d, want 50", got)
	}
	if got := performanceScore(ptr(3), 6); got != 74 { // 3 above target by
		t.Errorf("above-target finish performance = %d, want 74", got)
	}
	if got := expectationsScore(2, 1); got != 55 {
		t.Errorf("expectations = %d, want 55", got)
	}
	if got := financialScore(10_000, 25_000_000, 20_000_000); got != 68 { // 50+40*0.2+10
		t.Errorf("financial = %d, want 78", got)
	}
	if got := relationshipScore(1, 0, 80); got != 81 { // 50 + (80-50)/5 + 25
		t.Errorf("relationship = %d, want 79", got)
	}
	if got := dnaAlignmentScore(1, 0, 0, 1); got != 40 { // 50+15-25
		t.Errorf("dna alignment = %d, want 40", got)
	}
	if got := alternativesScore(5); got != 75 {
		t.Errorf("alternatives = %d, want 75", got)
	}
}
