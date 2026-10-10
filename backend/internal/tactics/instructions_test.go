package tactics

import (
	"testing"

	"github.com/touchline/backend/pkg/matchsim"
)

func TestInstructionsRoundTrip(t *testing.T) {
	in := matchsim.Instructions{Mentality: 1, Pressing: -1, Width: 0, Tempo: 1}
	got, err := ParseInstructions(ViewInstructions(in))
	if err != nil || got != in {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if v := ViewInstructions(matchsim.Instructions{}); v != (InstructionsView{"balanced", "mid", "normal", "normal"}) {
		t.Fatalf("neutral labels: %+v", v)
	}
}

func TestParseInstructionsRejectsUnknownOrMissing(t *testing.T) {
	for _, v := range []InstructionsView{
		{"positive", "high", "wide", "fast"},
		{"positive", "", "wide", "direct"},
	} {
		if _, err := ParseInstructions(v); err != ErrInvalidInstruction {
			t.Fatalf("%+v: want ErrInvalidInstruction, got %v", v, err)
		}
	}
}
