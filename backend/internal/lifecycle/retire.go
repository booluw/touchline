// The retirement model of the player lifecycle (A06): a discrete, rolled
// each season. Age is derived from date_of_birth vs the world reference date,
// so "aging" is implicit; retirement is the probe that fires when the curve,
// ability, injury and ambition mods push the probability over the roll.
package lifecycle

// RetireProbability is the A06 retirement curve:
//
//	base    = (age - 29) / 8      // 0 at 29, 12.5% at 30, 100% at 37+
//	ability = <50 → 1.5x, 50–70 → 1.0x, >70 → 0.6x
//	injury  = injury_susceptibility > 40 → 1.3x, else 1.0x
//	ambition= <30 → 1.3x, >70 → 0.7x, else 1.0x
//	result  = clamp(base × ability × injury × ambition, 0, 1)
//
// Age 37+ forces retirement regardless of the mods (the curve already reads
// 1.0 there, but the clamp keeps badly-modded elders honest).
func RetireProbability(age int, abilityAvg, injurySusceptibility, ambition int) float64 {
	var base float64
	if age >= 37 {
		base = 1.0
	} else if age <= 29 {
		base = 0
	} else {
		base = float64(age-29) / 8
	}
	return clamp01(base * abilityMod(abilityAvg) * injuryMod(injurySusceptibility) * ambitionMod(ambition))
}

func abilityMod(abilityAvg int) float64 {
	switch {
	case abilityAvg < 50:
		return 1.5
	case abilityAvg > 70:
		return 0.6
	default:
		return 1.0
	}
}

func injuryMod(injurySusceptibility int) float64 {
	if injurySusceptibility > 40 {
		return 1.3
	}
	return 1.0
}

func ambitionMod(ambition int) float64 {
	switch {
	case ambition < 30:
		return 1.3
	case ambition > 70:
		return 0.7
	default:
		return 1.0
	}
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
