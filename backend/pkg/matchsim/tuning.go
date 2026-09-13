package matchsim

import "time"

// Tuning is the versioned, product-owned tuning block for the match engine
// (spec §5, PM Part 5 + Part 7 sign-offs). The simulation structure is the
// approved product model; numbers marked "approved" are PM-signed, numbers
// marked "proposal" are implemented with documented values awaiting PM tuning
// sign-off (same v1.0→v1.2 discipline). They are consumed as configuration and
// recorded on match.matches.engine_version — never hardcoded at call sites.
type Tuning struct {
	// Version identifies the exact tuning block that produced a result. Any
	// change to structure OR numbers must ship a new version.
	Version string

	// PossessionExponent skews possession toward the stronger side:
	// share = p_home^e / (p_home^e + p_away^e); e<=0 ⇒ 50/50. The combined
	// figure per team is (Attack+Defense)/2 after form/morale/motivation/
	// variance (spec §2.1).
	PossessionExponent float64

	// ChancesPerMatchMin is the baseline attacking chances per team per 90
	// minutes against an equal opponent (approved).
	ChancesPerMatchMin float64

	// GoalWeightBase is the goal bucket's weight for two equal ratings.
	// GoalAbilityScale is the exponent on (attAttack/defDefense); the result
	// is clamped to [GoalMultiplierMin, GoalMultiplierMax] (spec §6, §2.1).
	GoalWeightBase     float64
	GoalAbilityScale   float64
	GoalMultiplierMin  float64
	GoalMultiplierMax  float64

	// OutcomeWeights are the chance-table weights before ability scaling:
	// goal, on-target, off-target, blocked, foul. The goal bucket scales with
	// ability; the rest stay fixed (approved baseline, v1.1 Concern 5).
	OutcomeWeights OutcomeWeights

	// ShotFeedFraction is the share of on-target shots surfaced as
	// chance_created feed events (keeps the S04-03 feed readable).
	ShotFeedFraction float64

	// HomeAdvantageFactor is a multiplier on the HOME side's effective
	// attack+defense ratings, feeding possession AND goal scaling
	// (v1.1 Concern 1; default 1.08 approved).
	HomeAdvantageFactor float64

	// VarianceLow/VarianceHigh bound the pre-match "on the day" roll — one
	// triangular draw per team centred at 1.0 (spec §2.3; band proposal).
	VarianceLow  float64
	VarianceHigh float64

	// Referee disposition on marginal calls (PM Part 5 §1, APPROVED):
	// noise (decided the wrong way, both directions) dominates a small
	// directional bias; RefereeBiasSource drives which signal (if any) biases
	// the call. "home_crowd" favours the home club by RefereeBiasFactor.
	RefereeNoiseFactor float64
	RefereeBiasFactor  float64
	RefereeBiasSource  string // "none" | "home_crowd" | "reputation_gap"

	// PenaltyConversionBase is the population-level penalty conversion rate
	// used when a Team has no designated taker (PM Part 5 §5, APPROVED 78%).
	PenaltyConversionBase float64

	// PenaltyInBoxFraction (proposal) is the portion of chance-table fouls
	// that occur inside the box and so produce a penalty decision.
	PenaltyInBoxFraction float64

	// Card/foul model (v1.1 Concern 2 + Understanding 13, mechanism approved;
	// numbers proposal): fouls are a defensive per-minute rate; cards couple
	// to fouls and scale with the defender's Aggression and RivalryIntensity.
	FoulBasePerMatchMin float64 // defensive fouls per team per 90 min
	FoulYellowBase      float64 // P(yellow | foul) at baseline fixture
	FoulRedBase         float64 // P(red | foul) at baseline fixture
	AggressionCardScale float64 // per-point scale: (0.5 + Aggression*..)
	RivalryCardScale    float64 // per-point scale: (1 + Rivalry*..)

	// CardRefereeMargin (proposal): a card draw landing within this distance
	// of a decision boundary is "borderline" and resolved by the referee.
	CardRefereeMargin float64

	// RedCard/Substitution in-match effects (PM Part 5 §4, APPROVED):
	// a dismissed team loses Defense 15% and Attack 25% for the rest of the
	// match; a fresh substitute adds +5% to the team's ratings.
	RedDefenseDegrade float64
	RedAttackDegrade  float64
	SubStaminaBoost   float64

	// InjuryOnFoulFraction (proposal) is the chance a serious-injury draw
	// fires on a defensive foul, forcing an immediate substitution.
	InjuryOnFoulFraction float64

	// AssistFraction (proposal) is the share of OPEN-PLAY goals that carry an
	// assist event (penalties never do).
	AssistFraction float64

	// SubWindows are the canonical minutes at which substitution draws happen,
	// once per window per team.
	SubWindows []int

	// SubAutoFraction is the chance a team makes a random sub at a window
	// when no manager LiveInput substitution exists for that window (v1.0
	// behaviour, retained).
	SubAutoFraction float64

	// LivePacingSecondsPerMinute is the default real-time pacing used by the
	// S04-02 live goroutines (OPD-21, 20s proposal).
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

// EngineVersion is the engine_version recorded on completed matches. It bumps
// whenever the canonical draw order or tuning structure changes (spec §4).
const EngineVersion = "1.2-approved"

// RefereeBiasSource values.
const (
	RefereeSourceNone        = "none"
	RefereeSourceHomeCrowd   = "home_crowd"
	RefereeSourceReputation  = "reputation_gap"
)

// ProposedTuning is the approved v1.2 tuning block (numbers marked PROPOSAL
// await PM tuning sign-off; the rest are PM-signed).
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
	HomeAdvantageFactor:        1.08,
	VarianceLow:                0.85,
	VarianceHigh:               1.15,
	RefereeNoiseFactor:         0.03,
	RefereeBiasFactor:          0.01,
	RefereeBiasSource:          RefereeSourceHomeCrowd,
	PenaltyConversionBase:      0.78,
	PenaltyInBoxFraction:       0.10,
	FoulBasePerMatchMin:        12,
	FoulYellowBase:             0.15,
	FoulRedBase:                0.008,
	AggressionCardScale:        0.005,
	RivalryCardScale:           0.005,
	CardRefereeMargin:          0.02,
	RedDefenseDegrade:          0.15,
	RedAttackDegrade:           0.25,
	SubStaminaBoost:            0.05,
	InjuryOnFoulFraction:       0.02,
	AssistFraction:             0.7,
	SubWindows:                 []int{60, 75},
	SubAutoFraction:            0.85,
	LivePacingSecondsPerMinute: 20 * time.Second,
}

// DefaultTuning returns a copy of the proposed tuning so callers can mutate
// their own instance without corrupting the shared proposal.
func DefaultTuning() Tuning {
	t := ProposedTuning
	t.SubWindows = append([]int{}, ProposedTuning.SubWindows...)
	return t
}