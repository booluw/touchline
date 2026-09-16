package playergen

import "math/rand"

// Hidden-trait, personality, and initial emotional-state generation.
//
// These numbers are the starting variance of a fresh world. Shared dimensions
// (professionalism, ambition, loyalty, adaptability) are drawn ONCE and
// mirrored into both player.player_hidden_traits and player.player_personality
// so the two rows can never disagree. Leaving a dimension at the pure random
// baseline produces the believable spread the engine modifiers expect: most
// players sit mid-band, a handful tip the scouting thresholds
// (e.g. temperament <= 30 → "Volatile Temperament"), and captains emerge from
// the leadership tail.

// generateTraitsAndPersonality draws the hidden-trait row and the personality
// row coherently: the four shared dimensions are drawn once and mirrored.
// potentialBonus is the talent class's potential-FLOOR lift (0 for the default
// tier); it raises the bottom of the roll before clamping to [1,100].
func generateTraitsAndPersonality(rng *rand.Rand, age int, potentialBonus int) (HiddenTraits, Personality) {
	professionalism := rng.Intn(56) + 35 // 35..90
	ambition := rng.Intn(66) + 25        // 25..90
	loyalty := rng.Intn(61) + 30         // 30..90
	adaptability := rng.Intn(61) + 30    // 30..91

	ht := HiddenTraits{
		// In the [1,100] bands below, each trait's baseline lives mid-range so
		// a generated squad is neither a dressing room of 90s nor of 10s.
		Potential:            clampInt(70+rng.Intn(21)+potentialBonus+(maxAge-age), 1, 100),
		Consistency:          rng.Intn(61) + 30, // 30..90
		InjurySusceptibility: rng.Intn(46) + 5,  // 5..50, low = robust
		Adaptability:         adaptability,
		Professionalism:      professionalism,
		Ambition:             ambition,
		Loyalty:              loyalty,
		Temperament:          25 + rng.Intn(66), // 25..90
		PressureHandling:     30 + rng.Intn(61), // 30..91
		LearningSpeed:        40 + rng.Intn(51), // 40..90
	}
	p := Personality{
		Professionalism:     professionalism,
		Ambition:            ambition,
		Loyalty:             loyalty,
		Adaptability:        adaptability,
		Ego:                 30 + rng.Intn(61),
		Sociability:         30 + rng.Intn(61),
		Patience:            35 + rng.Intn(56),
		Leadership:          20 + rng.Intn(71),
		EmotionalVolatility: 15 + rng.Intn(71),
	}
	return ht, p
}

// neutralEmotionalState seeds a fresh player content at a believable raw
// intensity, which is the "nothing has happened yet" precondition the first
// match day reads.
func neutralEmotionalState(rng *rand.Rand) EmotionalState {
	return EmotionalState{
		State:     "content",
		Cause:     "preseason",
		Intensity: 40 + rng.Intn(31), // 40..70
	}
}
