package matchsim

import "time"

// Tuning is the versioned, product-owned tuning block for the match engine
// (spec §5). The simulation structure is the approved product model; the
// numbers below are a v1.0 PROPOSAL awaiting PM sign-off (OPD-03). They are
// consumed as configuration and recorded on match.matches.engine_version —
// never hardcoded at call sites.
type Tuning struct {
	// Version identifies the exact tuning block that produced a result. Any
	// change to structure OR numbers must ship a new version.
	Version string

	// PossessionExponent skews possession toward the stronger side:
	// share = ability^e / (ability_a^e + ability_b^e); e<=0 ⇒ 50/50.
	PossessionExponent float64

	// ChancesPerMatchMin is the baseline attacking chances per team per 90
	// minutes against an equal (ability 75) opponent.
	ChancesPerMatchMin float64

	// GoalWeightBase is the goal bucket's weight for two equal abilities
	// (yields the spec's 0.10 goal probability after normalisation).
	GoalWeightBase float64
	// GoalAbilityScale is the exponent on (attAbility/defAbility); the
	// resulting multiplier is clamped to [GoalMultiplierMin, GoalMultiplierMax]
	// before it scales GoalWeightBase (spec §6).
	GoalAbilityScale  float64
	GoalMultiplierMin float64
	GoalMultiplierMax float64

	// OutcomeWeights are the chance-table weights before ability scaling:
	// goal, on-target, off-target, blocked, foul. They reproduce the spec's
	// cumulative thresholds for equal abilities (0.10 / 0.30 / 0.75 / 0.95 /
	// 1.00) after normalisation.
	OutcomeWeights OutcomeWeights

	// ShotFeedFraction is the share of on-target shots surfaced as
	// chance_created feed events (keeps the S04-03 feed readable).
	ShotFeedFraction float64

	// YellowPerMatchMin and RedPerMatchMin are baseline card rates per full
	// match (distributed by the per-minute card draw).
	YellowPerMatchMin float64
	RedPerMatchMin    float64

	// SubWindows are the canonical minutes at which substitution draws happen,
	// once per window per team.
	SubWindows []int

	// LivePacingSecondsPerMinute is the default real-time pacing used by the
	// S04-02 live goroutines. The world clock never drives matches.
	LivePacingSecondsPerMinute time.Duration
}

// OutcomeWeights are the unscaled chance-table weights.
type OutcomeWeights struct {
	Goal      float64
	OnTarget  float64
	OffTarget float64
	Blocked   float64
	Foul      float64
}

// EngineVersion is the engine_version recorded on completed matches today.
const EngineVersion = "1.0-proposed"

// ProposedTuning is the current v1.0-proposed tuning block (spec §5). It is
// intentionally a package-level value: callers copy it via DefaultTuning and
// the service layer persists its Version as engine_version.
var ProposedTuning = Tuning{
	Version:                    EngineVersion,
	PossessionExponent:         3.0,
	ChancesPerMatchMin:         13,
	GoalWeightBase:             1.0,
	GoalAbilityScale:           0.6,
	GoalMultiplierMin:          0.2,
	GoalMultiplierMax:          3.0,
	OutcomeWeights:             OutcomeWeights{Goal: 1, OnTarget: 2, OffTarget: 4.5, Blocked: 2, Foul: 0.5},
	ShotFeedFraction:           0.5,
	YellowPerMatchMin:          2.6,
	RedPerMatchMin:             0.10,
	SubWindows:                 []int{60, 75},
	LivePacingSecondsPerMinute: 20 * time.Second,
}

// DefaultTuning returns a copy of the proposed tuning so callers can mutate
// their own instance without corrupting the shared proposal.
func DefaultTuning() Tuning {
	t := ProposedTuning
	t.SubWindows = append([]int{}, ProposedTuning.SubWindows...)
	return t
}
