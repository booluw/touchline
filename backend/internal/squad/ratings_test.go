package squad

import (
	"testing"

	"github.com/google/uuid"
)

func TestWeightedScoreBalanced(t *testing.T) {
	a := AttributeSnapshot{Technical: 70, Physical: 60, Mental: 50, Tactical: 80, Goalkeeping: 0, Positional: 40}
	s := weightedScore(a, balancedWeights)
	// balancedWeights total = 2.5 + weighted mean across six categories.
	want := (70*0.5 + 60*0.5 + 50*0.5 + 80*0.5 + 0*0.1 + 40*0.5) / 2.6
	if !approx(s, want) {
		t.Fatalf("weighted mean %v, want %v", s, want)
	}
}

func TestWeightedScoreZeroRecipeDegenerates(t *testing.T) {
	a := AttributeSnapshot{Technical: 90}
	if s := weightedScore(a, CategoryWeights{}); !approx(s, defaultAttackDefense) {
		t.Fatalf("zero recipe must give neutral 50, got %v", s)
	}
}

func TestBuildSquadRatingsSinglePlayer(t *testing.T) {
	m := SquadMember{
		PlayerID: mustID(1), Position: "ST",
		Attributes: AttributeSnapshot{Technical: 90, Physical: 80, Mental: 60, Tactical: 40, Goalkeeping: 0, Positional: 50},
	}
	att, def := BuildSquadRatings([]SquadMember{m}, DefaultPositionWeights, nil)
	wantAtt := weightedScore(m.Attributes, DefaultPositionWeights["ST"].Attack)
	if mathAbs(float64(att)-wantAtt) > 0.5 {
		t.Fatalf("ST attack %d, want ~%v", att, wantAtt)
	}
	if att <= def {
		t.Fatalf("striker must rate higher in attack (%d) than defence (%d)", att, def)
	}
}

func TestBuildSquadRatingsAppliesPerPlayerFactor(t *testing.T) {
	xi := []SquadMember{
		{PlayerID: mustID(1), Position: "ST", Attributes: AttributeSnapshot{Technical: 90, Physical: 80, Mental: 60, Tactical: 40, Positional: 50}},
		{PlayerID: mustID(2), Position: "ST", Attributes: AttributeSnapshot{Technical: 90, Physical: 80, Mental: 60, Tactical: 40, Positional: 50}},
	}
	neutral, ndef := BuildSquadRatings(xi, DefaultPositionWeights, nil)
	// A feeble stint for the first striker drags the attack rating down.
	factors := map[uuid.UUID]float64{mustID(1): 0.85}
	depressed, _ := BuildSquadRatings(xi, DefaultPositionWeights, factors)
	if depressed >= neutral {
		t.Fatalf("factor 0.85 must depress attack; neutral %d depressed %d", neutral, depressed)
	}
	if ndef == 0 {
		t.Fatal("defensive rating must still be computed")
	}
}

func TestBuildSquadRatingsUnknownPositionFallsBack(t *testing.T) {
	xi := []SquadMember{
		{PlayerID: mustID(3), Position: "Sweeper", Attributes: AttributeSnapshot{Technical: 50, Physical: 50, Mental: 50, Tactical: 50}},
	}
	att, def := BuildSquadRatings(xi, DefaultPositionWeights, nil)
	if att < 1 || att > 100 || def < 1 || def > 100 {
		t.Fatalf("fallback ratings out of range: att %d def %d", att, def)
	}
}

func TestPlayerOverallRanges(t *testing.T) {
	a := AttributeSnapshot{Technical: 80, Physical: 70, Mental: 60, Tactical: 50, Positional: 60}
	if got := PlayerOverall(a, "ST"); got < 1 || got > 100 {
		t.Fatalf("ST overall %d out of range", got)
	}
	if known, missing := PlayerOverall(a, "ST"), PlayerOverall(a, "Sweeper"); missing < 1 || missing > 100 {
		t.Fatalf("unknown-position overall %d out of range", missing)
	} else if mathAbs(float64(known)-float64(missing)) > 100 {
		t.Fatalf("positions must both rate within [1,100], got %d/%d", known, missing)
	}
}

func TestPlayerOverallWithinSquadRatingBand(t *testing.T) {
	m := SquadMember{
		PlayerID: mustID(1), Position: "ST",
		Attributes: AttributeSnapshot{Technical: 90, Physical: 80, Mental: 60, Tactical: 40, Positional: 50},
	}
	att, def := BuildSquadRatings([]SquadMember{m}, DefaultPositionWeights, nil)
	overall := PlayerOverall(m.Attributes, m.Position)
	if overall > att || overall < def {
		t.Fatalf("overall %d should sit between the ST defence %d and attack %d", overall, def, att)
	}
}

func TestTakerPenaltyConversionRateBaseline(t *testing.T) {
	c := TakerPenaltyConversionRate(PlayerHiddenTraitsSnapshot{PressureHandling: 50, Consistency: 50}, 0)
	if !approx(c, 0.78) {
		t.Fatalf("neutral taker %v, want 0.78", c)
	}
}

func TestTakerPenaltyConversionRateClamps(t *testing.T) {
	worst := TakerPenaltyConversionRate(PlayerHiddenTraitsSnapshot{PressureHandling: 1, Consistency: 1}, -100)
	if worst < 0.65-1e-9 {
		t.Fatalf("worst-case taker %v below 0.65 floor", worst)
	}
	best := TakerPenaltyConversionRate(PlayerHiddenTraitsSnapshot{PressureHandling: 100, Consistency: 100}, 100)
	if best > 0.88+1e-9 {
		t.Fatalf("elite taker %v above 0.88 ceiling", best)
	}
	if mathAbs(best-0.88) > 1e-9 {
		t.Fatalf("elite taker should clamp to 0.88 exactly, got %v", best)
	}
}

func TestTakerPenaltyConversionRateSentimentDragsDown(t *testing.T) {
	base := PlayerHiddenTraitsSnapshot{PressureHandling: 60, Consistency: 60}
	happy := TakerPenaltyConversionRate(base, 50)
	nervous := TakerPenaltyConversionRate(base, -50)
	if nervous >= happy {
		t.Fatalf("nervous taker %v must rate below happy taker %v", nervous, happy)
	}
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
