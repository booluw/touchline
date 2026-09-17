// Package training implements Simple-Mode weekly training (S05-01 design §2):
// five archetypes, attribute-key deltas, the player condition subsystem, and a
// per-club weekly processor driven by the worker's WORLD_TICK weekly branch.
// All numbers are lifted from docs/design/tactics-training-numerics.md §2
// (proposal until PM tuning sign-off) and held here as data, never in engine
// code.
package training

import (
	"math"
	"math/rand"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/injury"
	"github.com/touchline/backend/internal/squad"
)

// Archetype keys — the five S05-01 training plans (club.club_training_plans
// CHECK and design §2.1).
const (
	ArchetypeTechnical = "technical"
	ArchetypePhysical  = "physical"
	ArchetypeDefensive = "defensive"
	ArchetypeAttacking = "attacking"
	ArchetypeRecovery  = "recovery"
)

// Archetypes lists the canonical keys for validation/iteration.
var Archetypes = []string{
	ArchetypeTechnical, ArchetypePhysical, ArchetypeDefensive, ArchetypeAttacking, ArchetypeRecovery,
}

// IsArchetype reports whether key is one of the five plans.
func IsArchetype(key string) bool {
	for _, k := range Archetypes {
		if k == key {
			return true
		}
	}
	return false
}

// Archetype is one row of the weekly training matrix (design §2.1): attribute
// deltas, the fatigue/injury/sharpness conditioning, and the recovery
// special-case. SharpnessRoles restricts the sharpness bonus to those primary
// positions (nil = all squad members).
type Archetype struct {
	Key            string
	Growth         map[string]float64 // attribute_key → +delta per week
	Decay          map[string]float64 // attribute_key → −delta per week
	FatigueStep    float64            // multiplies the +0.05 weekly fatigue step
	InjuryMult     float64            // punches injury_risk toward 0/1
	SharpnessBonus float64            // added to the +0.02 weekly sharpness step
	SharpnessRoles []string           // primary positions that receive the bonus
	Recovery       bool               // recovery is condition/removal-led (§2.2)
}

// archetypes is the design §2.1 table, verbatim.
var archetypes = map[string]Archetype{
	ArchetypeTechnical: {
		Key: ArchetypeTechnical,
		Growth: map[string]float64{
			"passing": 0.3, "vision": 0.2, "first_touch": 0.3, "composure": 0.2,
		},
		Decay: map[string]float64{
			"strength": -0.1, "tackling": -0.1,
		},
		FatigueStep:    0.80,
		InjuryMult:     0.70,
		SharpnessBonus: 0.05,
		SharpnessRoles: []string{"CM", "AM", "LM", "RM", "LW", "RW"},
	},
	ArchetypePhysical: {
		Key: ArchetypePhysical,
		Growth: map[string]float64{
			"stamina": 0.4, "natural_fitness": 0.3, "strength": 0.3, "work_rate": 0.2,
		},
		Decay: map[string]float64{
			"composure": -0.1,
		},
		FatigueStep:    1.40,
		InjuryMult:     1.35,
		SharpnessBonus: 0.02,
	},
	ArchetypeDefensive: {
		Key: ArchetypeDefensive,
		Growth: map[string]float64{
			"positioning": 0.4, "tackling": 0.3, "marking": 0.3,
			"concentration": 0.2, "teamwork": 0.2,
		},
		Decay: map[string]float64{
			"off_the_ball": -0.1,
		},
		FatigueStep:    1.00,
		InjuryMult:     0.85,
		SharpnessBonus: 0.03,
	},
	ArchetypeAttacking: {
		Key: ArchetypeAttacking,
		Growth: map[string]float64{
			"finishing": 0.4, "off_the_ball": 0.3, "pace": 0.2, "anticipation": 0.2,
		},
		Decay: map[string]float64{
			"marking": -0.1, "positioning": -0.1,
		},
		FatigueStep:    1.20,
		InjuryMult:     1.10,
		SharpnessBonus: 0.06,
		SharpnessRoles: []string{"ST"},
	},
	ArchetypeRecovery: {
		Key: ArchetypeRecovery,
		Growth: map[string]float64{
			"decision_making": 0.1,
		},
		FatigueStep:    -0.20,
		InjuryMult:     0.20,
		SharpnessBonus: -0.02,
		Recovery:       true,
	},
}

// archetypeFor resolves a plan key to its data row.
func archetypeFor(key string) (Archetype, bool) {
	a, ok := archetypes[key]
	return a, ok
}

// mentalGrowthKeys are the forecastgrowth mental keys eligible for the 30+
// ×1.2 age bonus (design §2.3). Decay is never age-scaled.
var mentalGrowthKeys = map[string]bool{
	"composure": true, "anticipation": true, "vision": true, "work_rate": true,
	"concentration": true, "decision_making": true, "positioning": true,
	"off_the_ball": true, "teamwork": true,
}

// growthScale returns the age multiplier for positive attribute deltas (§2.3):
// 16–21 ×1.8, 22–29 ×1.0, 30+ mental keys ×1.2 (others ×1.0). Decay is applied
// separately at ×1.0.
func growthScale(age int, key string) float64 {
	switch {
	case age >= 16 && age <= 21:
		return 1.8
	case age >= 30 && mentalGrowthKeys[key]:
		return 1.2
	default:
		return 1.0
	}
}

// veteranDecay is the mandatory 30+ deceleration: pace/stamina −0.05 per week
// unless the plan is physical (§2.3).
func veteranDecay(age int, plan string) map[string]float64 {
	if age < 30 || plan == ArchetypePhysical {
		return nil
	}
	return map[string]float64{"pace": -0.05, "stamina": -0.05}
}

// attrDelta is a key's weekly attribute delta for a player, age-scaled. Growth
// uses growthScale; decay stays ×1.0.
func attrDelta(a Archetype, age int, key string) float64 {
	if v, ok := a.Growth[key]; ok {
		return v * growthScale(age, key)
	}
	if v, ok := a.Decay[key]; ok {
		return v
	}
	return 0
}

// applyDelta folds a fractional weekly delta into an integer attribute value
// with a seeded binary rounding that preserves the expectation exactly
// (+0.3/week ≈ +1 point 30% of weeks, and +2.6/week ≈ +2 points plus a 60%
// third), so small deltas still move a player over a season and the
// development engine's compounded deltas keep their mean. Deterministic for a
// fixed rng.
func applyDelta(v int, d float64, rng *rand.Rand) int {
	if d > 0 {
		v += int(d)
		if rng.Float64() < d-float64(int(d)) {
			v++
		}
	} else if d < 0 {
		neg := -d
		v -= int(neg)
		if rng.Float64() < neg-float64(int(neg)) {
			v--
		}
	}
	if v < 1 {
		return 1
	}
	if v > 100 {
		return 100
	}
	return v
}

// planIntensity is the normalized weekly workload a plan applies to a player of
// a given age (0..1): the sum of the week's positive growth deltas against the
// documented saturation volume. Heavier plans raise the weekly training-injury
// probability (S08-03).
func planIntensity(a Archetype, age int) float64 {
	total := 0.0
	for _, key := range distinctKeys(a) {
		if d := attrDelta(a, age, key); d > 0 {
			total += d
		}
	}
	return clamp01(total / injury.TrainingVolumeSaturation)
}

// defaultsCondition is the lazy baseline (design §2.2: seeded at squad
// materialization) for a player with no player.player_condition row yet.
func defaultCondition(pid uuid.UUID, injurySusceptibility int) squad.PlayerCondition {
	risk := float64(injurySusceptibility) / 200
	if risk < 0 {
		risk = 0
	}
	if risk > 1 {
		risk = 1
	}
	return squad.PlayerCondition{
		PlayerID:            pid,
		Fatigue:             0,
		Fitness:             1,
		Sharpness:           0.5,
		InjuryRisk:          risk,
		TacticalFamiliarity: 0.5,
		Morale:              0.5,
	}
}

// applyCondition advances one player's condition by one weekly step (§2.2).
func applyCondition(c squad.PlayerCondition, a Archetype, sport string) squad.PlayerCondition {
	// fatigue: recovery removes 0.20 (its −0.40 removal over the baseline
	// +0.05 step); every other plan accumulates 0.05 × FatigueStep.
	if a.Recovery {
		c.Fatigue = clamp01(c.Fatigue - 0.20)
	} else {
		c.Fatigue = clamp01(c.Fatigue + 0.05*a.FatigueStep)
	}
	// fitness: 0.05 weekly recovery offset by the plan's fatigue step
	// (recovery's negative step therefore recovers more).
	c.Fitness = clamp01(c.Fitness + 0.05 - 0.05*a.FatigueStep)
	// sharpness: +0.02 baseline plus the archetype bonus, which may be
	// role-gated (technical midfielders/wingers, attacking strikers).
	bonus := 0.0
	if a.SharpnessRoles == nil || containsRole(a.SharpnessRoles, sport) {
		bonus = a.SharpnessBonus
	}
	c.Sharpness = clamp01(c.Sharpness + 0.02 + bonus)
	// injury risk: punched toward 0/1 by the archetype multiplier.
	c.InjuryRisk = clamp01(c.InjuryRisk + 0.05*(a.InjuryMult-1)*(1-c.InjuryRisk))
	// tactical familiarity: recovery speeds learning.
	if a.Recovery {
		c.TacticalFamiliarity = clamp01(c.TacticalFamiliarity + 0.05)
	} else {
		c.TacticalFamiliarity = clamp01(c.TacticalFamiliarity + 0.02)
	}
	return c
}

func containsRole(roles []string, sport string) bool {
	for _, r := range roles {
		if r == sport {
			return true
		}
	}
	return false
}

func clamp01(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
