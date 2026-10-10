package tactics

import "github.com/touchline/backend/pkg/matchsim"

// Instruction option labels per dial, indexed by dial value + 1 (IM61).
var instructionLabels = struct{ Mentality, Pressing, Width, Tempo [3]string }{
	Mentality: [3]string{"cautious", "balanced", "positive"},
	Pressing:  [3]string{"low", "mid", "high"},
	Width:     [3]string{"narrow", "normal", "wide"},
	Tempo:     [3]string{"patient", "normal", "direct"},
}

// InstructionsView is the wire shape of the four team-instruction dials.
type InstructionsView struct {
	Mentality string `json:"mentality"`
	Pressing  string `json:"pressing"`
	Width     string `json:"width"`
	Tempo     string `json:"tempo"`
}

func label(opts [3]string, dial int) string { return opts[dial+1] }

func dial(opts [3]string, v string) (int, bool) {
	for i, o := range opts {
		if o == v {
			return i - 1, true
		}
	}
	return 0, false
}

// ViewInstructions renders engine dials as labels.
func ViewInstructions(in matchsim.Instructions) InstructionsView {
	l := instructionLabels
	return InstructionsView{
		Mentality: label(l.Mentality, in.Mentality),
		Pressing:  label(l.Pressing, in.Pressing),
		Width:     label(l.Width, in.Width),
		Tempo:     label(l.Tempo, in.Tempo),
	}
}

// ParseInstructions validates labels into engine dials; every field is required.
func ParseInstructions(v InstructionsView) (matchsim.Instructions, error) {
	l := instructionLabels
	var in matchsim.Instructions
	var ok [4]bool
	in.Mentality, ok[0] = dial(l.Mentality, v.Mentality)
	in.Pressing, ok[1] = dial(l.Pressing, v.Pressing)
	in.Width, ok[2] = dial(l.Width, v.Width)
	in.Tempo, ok[3] = dial(l.Tempo, v.Tempo)
	for _, k := range ok {
		if !k {
			return matchsim.Instructions{}, ErrInvalidInstruction
		}
	}
	return in, nil
}
