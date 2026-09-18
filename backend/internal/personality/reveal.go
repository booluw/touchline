package personality

import "fmt"

// --- Hidden-trait reveal (S09-01 acceptance: "gradual reveal of hidden
// traits over time; never a one-shot dump"). -------------------------------------------------

// RevealSource is where evidence for a hidden trait may come from. Each source
// the engine can observe deterministically, and the engine never invents
// evidence — a hidden trait is only exposed when an interaction naturally
// leaks it.
type RevealSource string

const (
	// SrcPressureLeak — high-stakes fixtures (pressure_handling, consistency).
	SrcPressureLeak RevealSource = "high_stakes_fixture"
	// SrcTrainingLoadLeak — sustained overload (learning_speed, consistency).
	SrcTrainingLoadLeak RevealSource = "training_load_overload"
	// SrcRecoveryLeak — a treatment/recovery window (professionalism → recovery
	// discipline, injury_susceptibility is revealed by the club's own record).
	SrcRecoveryLeak RevealSource = "recovery_programme"
	// SrcTransferWindow — transfer bids (ambition/loyalty are VISIBLE already;
	// ego decides whether a block leaks adaptability/consistency).
	SrcTransferWindow RevealSource = "transfer_window"
	// SrcTeamTalkLeak — team talks (temperament/volatility under the lights).
	SrcTeamTalkLeak RevealSource = "team_talk_squad"
)

// baseConfidence is the starting observation confidence for a reveal source;
// the engine never starts at 100 — the acceptance contract demands gradual
// hardening from one interaction to the next.
// baseConfidence is the starting observation confidence, per source, for a
// fresh (prior=0) reveal. The engine never starts a surface at 100 — the
// acceptance contract demands gradual, observation-backed hardening.
var baseConfidence = map[RevealSource]int{ // proposal tuning constants
	SrcPressureLeak:     60,
	SrcTrainingLoadLeak: 45,
	SrcRecoveryLeak:     55,
	SrcTransferWindow:   50,
	SrcTeamTalkLeak:     50,
}

// revealBump is how much a second, consistent observation hardens a trait that
// has already been leaked by the same source (still under 100 until the club
// has genuinely consistent evidence).
const revealBump = 15

// maybeReveal inspects one source's worth of deterministic observation and
// returns the hidden trait surface it supports (nil when there is no evidence
// to surface). Hidden, non-observable traits never leak from a source they
// have nothing to do with.
func (e *Engine) maybeReveal(src RevealSource, t TraitSet, priorConfidence int) *TraitReveal {
	if priorConfidence >= 100 {
		return nil // already fully surfaced; nothing gradual left to add
	}
	trait, _, text, ok := evidenceFor(t, src)
	if !ok {
		return nil
	}
	conf := baseConfidence[src]
	if priorConfidence > 0 {
		conf = priorConfidence + revealBump
		if conf > 95 {
			conf = 95
		}
	}
	return &TraitReveal{
		PlayerID:   t.PlayerID,
		Trait:      "hidden:" + trait,
		Source:     string(src),
		Confidence: conf,
		Text:       text,
	}
}

// evidenceFor maps an observation source to the single hidden trait it can
// credibly leak plus the human-readable 1..10 value and explanation. Pure:
// deterministic on (source, trait set).
func evidenceFor(t TraitSet, src RevealSource) (trait string, value int, text string, ok bool) {
	switch src {
	case SrcPressureLeak:
		// A high-stakes fixture with a composed player performs; a brittle one
		// cracks. The manager sees "how they hold up" — the raw ceiling never
		// surfaces, only the composure/pressure axis.
		if t.PressureHandling >= 8 {
			return "pressure_handling", t.PressureHandling,
				"Held the high-stakes run in with visibly more composure than a normal squad-mate — pressure handling reads high.", true
		}
		if t.PressureHandling <= 4 {
			return "pressure_handling", t.PressureHandling,
				"The nerves were visible in the decisive window — pressure handling reads low.", true
		}
		return "consistency", t.Consistency,
			"Repeated identical output across a brace of high-pressure fixtures — consistency is starting to show.", true

	case SrcTrainingLoadLeak:
		if t.LearningSpeed >= 8 {
			return "learning_speed", t.LearningSpeed,
				"Under a raised load the player's drills sharpened faster than the squad — learning speed reads high.", true
		}
		if t.LearningSpeed <= 3 {
			return "learning_speed", t.LearningSpeed,
				"Even under an easy condition the new pattern never stuck — learning speed reads low.", true
		}
		return "consistency", t.Consistency,
			"An even response to an uneven schedule — consistency is holding under load.", true

	case SrcRecoveryLeak:
		// Recovery discipline is dominated by professionalism, which is VISIBLE;
		// but injury history is genuinely hidden, so a clean recovery window is
		// the club's honest signal for injury susceptibility.
		if t.InjurySusceptibility >= 7 {
			return "injury_susceptibility", t.InjurySusceptibility,
				"A minor knock took far longer to settle than the medical desk expected — susceptibility reads high.", true
		}
		if t.InjurySusceptibility <= 3 {
			return "injury_susceptibility", t.InjurySusceptibility,
				"A full recovery programme with no setbacks and no follow-on complaints — susceptibility reads low.", true
		}
		return "adaptability", t.Adaptability,
			"Recovery discipline holds steady with the programme — a low-drama, adaptable profile.", true

	case SrcTransferWindow:
		if t.Ego >= 8 && t.Adaptability >= 7 {
			return "adaptability", t.Adaptability,
				"Handled a block on a transfer with visible adaptability — adapting to the club refusing the move.", true
		}
		if t.Ego >= 8 {
			return "consistency", t.Consistency,
				"A volatile ego and a blocked move, yet output stayed flat — consistency underneath the noise.", true
		}
		return "pressure_handling", t.PressureHandling,
			"The transfer saga never touched performance — pressure handling held.", true

	case SrcTeamTalkLeak:
		if t.Volatility >= 6 {
			return "volatility:temperament", t.Volatility,
				"An even technical point landed as an emotional spike in the dressing room — temperament reads hot.", true
		}
		if t.Volatility <= 3 {
			return "volatility:temperament", t.Volatility,
				"Managers could pump the room and the player stayed level-headed — temperament reads cold.", true
		}
		return "leadership", t.Leadership,
			"A steady presence in a heated squad meeting — leadership is surfacing.", true
	}
	return "", 0, "", false
}

// mustReveal is a test-only convenience: it runs the reveal for a source and
// panics if the trait set produced no evidence, keeping tests terse.
func (e *Engine) mustReveal(src RevealSource, t TraitSet) *TraitReveal {
	r := e.maybeReveal(src, t, 0)
	if r == nil {
		panic(fmt.Sprintf("personality: no reveal evidence for source %q", src))
	}
	return r
}
