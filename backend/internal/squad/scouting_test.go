package squad

import (
	"testing"
)

func TestScoutingTagsThresholds(t *testing.T) {
	tags := TraitTagThresholds2018

	// No tags when nothing is at a threshold.
	if got := ScoutingTags(PlayerHiddenTraitsSnapshot{PressureHandling: 50, Adaptability: 50, Temperament: 60, Consistency: 70}, tags); len(got) != 0 {
		t.Fatalf("expected no tags, got %v", got)
	}

	// Thrives only at >= 80 pressure handling.
	if got := ScoutingTags(PlayerHiddenTraitsSnapshot{PressureHandling: 80, Adaptability: 50, Temperament: 60, Consistency: 70}, tags); len(got) != 1 || got[0] != "Thrives Under Pressure" {
		t.Fatalf("pressure 80 => %v", got)
	}

	// Set in their ways only at adaptability <= 10.
	if got := ScoutingTags(PlayerHiddenTraitsSnapshot{PressureHandling: 50, Adaptability: 1, Temperament: 60, Consistency: 70}, tags); len(got) != 1 || got[0] != "Set In Their Ways" {
		t.Fatalf("adaptability 1 => %v", got)
	}

	// Volatile at temperament <= 30.
	if got := ScoutingTags(PlayerHiddenTraitsSnapshot{PressureHandling: 50, Adaptability: 50, Temperament: 30, Consistency: 70}, tags); len(got) != 1 || got[0] != "Volatile Temperament" {
		t.Fatalf("temperament 30 => %v", got)
	}

	// Inconsistent at consistency <= 30.
	if got := ScoutingTags(PlayerHiddenTraitsSnapshot{PressureHandling: 50, Adaptability: 50, Temperament: 60, Consistency: 29}, tags); len(got) != 1 || got[0] != "Inconsistent" {
		t.Fatalf("consistency 29 => %v", got)
	}
}

func TestScoutingTagsOrderStable(t *testing.T) {
	tags := TraitTagThresholds2018
	h := PlayerHiddenTraitsSnapshot{PressureHandling: 95, Adaptability: 1, Temperament: 10, Consistency: 5}
	want := []string{"Thrives Under Pressure", "Set In Their Ways", "Volatile Temperament", "Inconsistent"}
	got := ScoutingTags(h, tags)
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("tag order %v, want %v", got, want)
		}
	}
}

func TestLineupWarningForTriggersAtAndBelowThreshold(t *testing.T) {
	tuning := ProposedTuning
	in := PlayerPerformanceInput{
		PlayerID: mustID(7), Consistency: 1, Temperament: 5, PressureHandling: 5, CurrentSentiment: -100,
	}
	f := PlayerPerformanceFactor{PlayerID: in.PlayerID, Factor: LineupWarningThreshold}
	w, ok := LineupWarningFor(in, FixtureContext{IsDerby: true, DerbyIntensity: 90}, f, 1, tuning, 0)
	if !ok {
		t.Fatal("factor exactly at threshold must warn")
	}
	if w.PlayerID != in.PlayerID {
		t.Fatalf("warning player %v, want %v", w.PlayerID, in.PlayerID)
	}
	if w.PolicyDecision == nil || w.PolicyDecision.Subject != LineupWarningSubject {
		t.Fatalf("explanation subject %+v", w.PolicyDecision)
	}
	if len(w.PolicyDecision.Factors) == 0 {
		t.Fatal("warning must carry at least the sentiment factor")
	}
}

func TestLineupWarningForSilentAboveThreshold(t *testing.T) {
	tuning := ProposedTuning
	in := PlayerPerformanceInput{PlayerID: mustID(8), Consistency: 100, CurrentSentiment: 0}
	f := PlayerPerformanceFactor{PlayerID: in.PlayerID, Factor: 1.06}
	if _, ok := LineupWarningFor(in, FixtureContext{IsCupTie: true}, f, 2, tuning, 0); ok {
		t.Fatal("no warning expected at factor 1.06")
	}
}

func TestLineupWarningFactorsDecomposeStack(t *testing.T) {
	tuning := ProposedTuning
	// Derby, glacier-calm, consistent, happy: no warning (factor well above).
	in := PlayerPerformanceInput{
		PlayerID: mustID(9), Consistency: 95, Temperament: 85, PressureHandling: 90, CurrentSentiment: 30,
	}
	f := PlayerPerformanceFactor{PlayerID: in.PlayerID, Factor: 1.12}
	if _, ok := LineupWarningFor(in, FixtureContext{IsDerby: true, DerbyIntensity: 80}, f, 3, tuning, 0); ok {
		t.Fatal("high-flying player must not warn")
	}
}
