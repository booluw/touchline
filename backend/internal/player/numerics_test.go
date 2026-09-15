package player

import (
	"math"
	"testing"
)

func TestPlayingTimeRatio(t *testing.T) {
	cases := []struct {
		name     string
		minutes  int
		matches  int
		expected float64
	}{
		{"no matches yet", 900, 0, 0},
		{"no minutes", 0, 10, 0},
		{"full season starter", 810, 10, 0.9},
		{"capped at 1", 9000, 10, 1},
		{"exact role entitlement", 675, 10, 0.75},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlayingTimeRatio(tc.minutes, tc.matches)
			if math.Abs(got-tc.expected) > 1e-6 {
				t.Fatalf("PlayingTimeRatio(%d, %d) = %v, want %v", tc.minutes, tc.matches, got, tc.expected)
			}
		})
	}
}

func TestExpectedShareForRole(t *testing.T) {
	cases := map[string]float64{
		SquadRoleKeyPlayer:   RoleExpectedShareKeyPlayer,
		SquadRoleRotation:    RoleExpectedShareRotation,
		SquadRoleSquadPlayer: RoleExpectedShareSquadPlayer,
		SquadRoleDevelopment: RoleExpectedShareDevelopment,
		"":                   RoleExpectedShareSquadPlayer,
		"unknown_role":       RoleExpectedShareSquadPlayer,
	}
	for role, want := range cases {
		if got := ExpectedShareForRole(role); got != want {
			t.Errorf("ExpectedShareForRole(%q) = %v, want %v", role, got, want)
		}
	}
}

func TestSquadRoleFromExpectation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"I expect to be a key player", SquadRoleKeyPlayer},
		{"First team regular", SquadRoleKeyPlayer},
		{"impact sub rotation", SquadRoleRotation},
		{"youth development path", SquadRoleDevelopment},
		{"squad depth", SquadRoleSquadPlayer},
		{"", ""},
	}
	for _, tc := range cases {
		if got := SquadRoleFromExpectation(tc.in); got != tc.want {
			t.Errorf("SquadRoleFromExpectation(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSatisfiedAndDeepShortfall(t *testing.T) {
	if !Satisfied(0.8, RoleExpectedShareKeyPlayer) {
		t.Error("share 0.8 / expected 0.75 should be satisfied")
	}
	if Satisfied(0.4, RoleExpectedShareKeyPlayer) {
		t.Error("share 0.4 / expected 0.75 should not be satisfied")
	}
	if !Satisfied(0, 0) {
		t.Error("development role (expected 0) is always satisfied")
	}
	if !DeepShortfall(0.3, RoleExpectedShareKeyPlayer) {
		t.Error("share 0.3 is below half of expected 0.75")
	}
	if DeepShortfall(0.5, RoleExpectedShareKeyPlayer) {
		t.Error("share 0.5 is exactly the deep-shortfall cutoff, not below")
	}
	if DeepShortfall(0.0, 0) {
		t.Error("development role has no shortfall")
	}
}

func TestMoraleTargetBands(t *testing.T) {
	avg := PlayerPersonality{Professionalism: 50, Ambition: 50, Loyalty: 50, Ego: 50, Patience: 50}
	if got := moraleTarget(0.8, RoleExpectedShareKeyPlayer, avg); got != MoraleTargetSatisfied {
		t.Errorf("met expected share: target = %v, want %v", got, MoraleTargetSatisfied)
	}
	if got := moraleTarget(0.55, RoleExpectedShareKeyPlayer, avg); got != MoraleTargetNeutral {
		t.Errorf("between half and full: target = %v, want %v", got, MoraleTargetNeutral)
	}
	if got := moraleTarget(0.2, RoleExpectedShareKeyPlayer, avg); got != MoraleTargetUnhappy {
		t.Errorf("deep shortfall: target = %v, want %v", got, MoraleTargetUnhappy)
	}
	if got := moraleTarget(0, 0, avg); got != MoraleTargetSatisfied {
		t.Errorf("development role no pressure: target = %v, want %v", got, MoraleTargetSatisfied)
	}
}

func TestMoraleTargetPersonalityInfluence(t *testing.T) {
	base := PlayerPersonality{Professionalism: 50, Ambition: 50, Loyalty: 50, Ego: 50, Patience: 50}
	patient := PlayerPersonality{Professionalism: 50, Ambition: 50, Loyalty: 50, Ego: 50, Patience: 100}
	if got := moraleTarget(0.2, RoleExpectedShareKeyPlayer, patient); got <= MoraleTargetUnhappy {
		t.Errorf("patient player should NOT drop below the unhappy baseline, got %v", got)
	}
	ambitious := PlayerPersonality{Professionalism: 50, Ambition: 100, Loyalty: 50, Ego: 50, Patience: 50}
	if got := moraleTarget(0.2, RoleExpectedShareKeyPlayer, ambitious); got > moraleTarget(0.2, RoleExpectedShareKeyPlayer, base) {
		t.Errorf("ambitious player should have a lower (harder) target, got %v vs %v",
			got, moraleTarget(0.2, RoleExpectedShareKeyPlayer, base))
	}
	if got := moraleTarget(0.2, RoleExpectedShareKeyPlayer, patient); got < 0.05 {
		t.Errorf("target must respect the 0.05 floor, got %v", got)
	}
}

func TestSwingAlphaBounded(t *testing.T) {
	cases := []struct {
		name string
		p    PlayerPersonality
	}{
		{"league average", PlayerPersonality{Professionalism: 50, EmotionalVolatility: 50}},
		{"max volatility", PlayerPersonality{Professionalism: 50, EmotionalVolatility: 100}},
		{"max professionalism", PlayerPersonality{Professionalism: 100, EmotionalVolatility: 50}},
		{"min both", PlayerPersonality{Professionalism: 0, EmotionalVolatility: 0}},
		{"out of range values", PlayerPersonality{Professionalism: 200, EmotionalVolatility: -10}},
	}
	for _, tc := range cases {
		a := swingAlpha(tc.p)
		if a < 0 || a > MoraleSwingAlpha*VolatilitySwingScale {
			t.Errorf("%s: swingAlpha out of bounds %v", tc.name, a)
		}
	}
}

func TestUpdateMorale(t *testing.T) {
	avg := PlayerPersonality{Professionalism: 50, Ambition: 50, Loyalty: 50, Ego: 50, Patience: 50, EmotionalVolatility: 50}
	// Deep shortfall: pulls morale down toward the unhappy target.
	got := UpdateMorale(0.9, 0.2, RoleExpectedShareKeyPlayer, avg)
	if got >= 0.9 {
		t.Fatalf("unhappy playing time should drag morale down, got %v", got)
	}
	// Satisfied: morale climbs toward the satisfied target.
	got = UpdateMorale(0.1, 0.8, RoleExpectedShareKeyPlayer, avg)
	if got > MoraleTargetSatisfied || got <= 0.1 {
		t.Fatalf("satisfied playing time should pull morale up toward 0.65 and stay bounded, got %v", got)
	}
	// Bounded at the top: a strong player plays the whole season, morale = satisfied target exactly.
	got = UpdateMorale(1.0, 1.0, RoleExpectedShareKeyPlayer, avg)
	if got > 1.0 || got < MoraleTargetSatisfied {
		t.Fatalf("should settle near the satisfied target and respect [0,1], got %v", got)
	}
	// Volatile temperaments move further each match.
	volatile := avg
	volatile.EmotionalVolatility = 100
	calm := avg
	calm.EmotionalVolatility = 0
	dv := math.Abs(UpdateMorale(0.5, 1.0, RoleExpectedShareKeyPlayer, volatile) - MoraleTargetSatisfied)
	dc := math.Abs(UpdateMorale(0.5, 1.0, RoleExpectedShareKeyPlayer, calm) - MoraleTargetSatisfied)
	if dv >= dc {
		t.Errorf("volatile player should close more per match (dv %v, dc %v)", dv, dc)
	}
}

func TestWeeklyRecovery(t *testing.T) {
	low := WeeklyRecovery(0.2, 50)
	if low <= 0.2 || low >= MoraleNeutralBaseline {
		t.Fatalf("low morale should recover toward 0.5, got %v", low)
	}
	high := WeeklyRecovery(0.9, 50)
	if high >= 0.9 || high <= MoraleNeutralBaseline {
		t.Fatalf("high morale should regress toward 0.5, got %v", high)
	}
	// Professional players recover faster.
	if WeeklyRecovery(0.2, 100) <= WeeklyRecovery(0.2, 0) {
		t.Error("professional players should recover faster")
	}
	// At exactly neutral, recovery is a fixed point.
	if got := WeeklyRecovery(MoraleNeutralBaseline, 100); got != MoraleNeutralBaseline {
		t.Errorf("neutral morale is a fixed point, got %v", got)
	}
	if WeeklyRecovery(1.1, 50) > 1 || WeeklyRecovery(-0.1, 50) < 0 {
		t.Error("recovery must stay within [0,1]")
	}
}

func TestSatisfiedTargetFor(t *testing.T) {
	if got := SatisfiedTargetFor(RoleExpectedShareKeyPlayer); got != MoraleTargetSatisfied {
		t.Errorf("SatisfiedTargetFor(key_player) = %v, want %v", got, MoraleTargetSatisfied)
	}
	if got := SatisfiedTargetFor(0); got != MoraleTargetSatisfied {
		t.Errorf("SatisfiedTargetFor(development) = %v, want %v", got, MoraleTargetSatisfied)
	}
}

func TestClamps(t *testing.T) {
	if got := clampInt(-5, 0, 100); got != 0 {
		t.Errorf("clampInt(-5) = %d", got)
	}
	if got := clampInt(150, 0, 100); got != 100 {
		t.Errorf("clampInt(150) = %d", got)
	}
	if got := clamp01(1.5, 0, 1); got != 1 {
		t.Errorf("clamp01(1.5) = %v", got)
	}
	if got := clamp01(-0.5, 0, 1); got != 0 {
		t.Errorf("clamp01(-0.5) = %v", got)
	}
}
