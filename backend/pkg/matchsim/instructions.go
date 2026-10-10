package matchsim

// Team instructions (IM61) layer four three-way dials on top of a side's
// style block. Each dial is -1 / 0 / +1; 0 is the neutral middle option and
// leaves the style block untouched, so a side without instructions (every
// pre-IM61 snapshot) replays byte-identically. Like the style block, every
// lever re-weights an existing boundary and consumes no RNG.
//
//	Mentality  -1 cautious  · +1 positive
//	Pressing   -1 low       · +1 high
//	Width      -1 narrow    · +1 wide
//	Tempo      -1 patient   · +1 direct
type Instructions struct {
	Mentality int `json:"mentality,omitempty"`
	Pressing  int `json:"pressing,omitempty"`
	Width     int `json:"width,omitempty"`
	Tempo     int `json:"tempo,omitempty"`
}

// instructionLever is one dial's effect at +1; -1 applies the mirrored
// effect (multipliers inverted, shifts negated). Numbers are proposals,
// sized well inside the style spread so instructions refine a style rather
// than replace it.
type instructionLever struct {
	possessionShift    float64
	chanceVolume       float64
	goalConversion     float64
	concededConversion float64
	cardRate           float64
	staminaDecay       float64
}

var (
	// Positive: more bodies forward — more chances, a more open back line.
	leverMentality = instructionLever{chanceVolume: 1.12, goalConversion: 1, concededConversion: 1.15, cardRate: 1, staminaDecay: 1}
	// High press: win the ball higher, foul more, burn the tank faster.
	leverPressing = instructionLever{possessionShift: 0.04, chanceVolume: 1.05, goalConversion: 1, concededConversion: 1.06, cardRate: 1.15, staminaDecay: 1.15}
	// Wide: stretch the opponent — more chances, more space left centrally.
	leverWidth = instructionLever{chanceVolume: 1.07, goalConversion: 1, concededConversion: 1.05, cardRate: 1, staminaDecay: 1.03}
	// Direct tempo: fewer passes, more (lower quality) shots.
	leverTempo = instructionLever{possessionShift: -0.04, chanceVolume: 1.08, goalConversion: 0.92, concededConversion: 1, cardRate: 1, staminaDecay: 1.04}
)

// apply folds one dial setting into spec.
func (l instructionLever) apply(spec StyleSpec, dial int) StyleSpec {
	switch {
	case dial > 0:
		spec.PossessionShift += l.possessionShift
		spec.ChanceVolume *= l.chanceVolume
		spec.GoalConversion *= l.goalConversion
		spec.ConcededConversion *= l.concededConversion
		spec.CardRate *= l.cardRate
		spec.StaminaDecay *= l.staminaDecay
	case dial < 0:
		spec.PossessionShift -= l.possessionShift
		spec.ChanceVolume /= l.chanceVolume
		spec.GoalConversion /= l.goalConversion
		spec.ConcededConversion /= l.concededConversion
		spec.CardRate /= l.cardRate
		spec.StaminaDecay /= l.staminaDecay
	}
	return spec
}

// withInstructions returns spec adjusted by every non-neutral dial.
func withInstructions(spec StyleSpec, in Instructions) StyleSpec {
	spec = leverMentality.apply(spec, in.Mentality)
	spec = leverPressing.apply(spec, in.Pressing)
	spec = leverWidth.apply(spec, in.Width)
	return leverTempo.apply(spec, in.Tempo)
}
