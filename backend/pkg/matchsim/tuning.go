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
	GoalWeightBase    float64
	GoalAbilityScale  float64
	GoalMultiplierMin float64
	GoalMultiplierMax float64

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

	// Styles maps S05-01 style keys to their numeric block (matchsim_addendum
	// v1.5). DefaultStyle is the identity fallback when a team's key is missing
	// or unknown; "balanced" is the identity block, so a tactics-less match is
	// numerically identical to the v1.4 block (golden digest pins).
	Styles       map[string]StyleSpec
	DefaultStyle string

	// Stamina model (addendum v1.5): a side's tank seeds from Team.Fitness
	// ([0,1], default 1.0) and drains every minute by (1/90) × the current
	// style's StaminaDecay. Substitutions restore it to SubFitness. From
	// FatigueStartMinute onward a side whose stamina lags the healthy norm
	// suffers an effectiveness penalty:
	//
	//	deficit = max((90 - minute) / 90 - stamina, 0)
	//	eff     = 1 - clamp(deficit * FatiguePenaltyScale, 0, FatiguePenaltyMax)
	//
	// At Fitness 1.0 with the balanced style the deficit is identically zero,
	// so legacy calls replicate the v1.4 outcome byte-for-byte. Lower squad
	// conditioning and faster-decay styles (gegenpress +35%) burn the tank and
	// pay late; low_block (−15%) preserves it.
	SubFitness          float64
	FatigueStartMinute  int
	FatiguePenaltyScale float64
	FatiguePenaltyMax   float64
}

// StyleSpec is one style's numeric block (v1.5). Every field re-weights an
// existing probability boundary or scales a rating value — none consumes RNG —
// so within an engine version replays stay byte-exact.
type StyleSpec struct {
	PossessionShift    float64 // additive on the side's possession share (Δ of p_home)
	ChanceVolume       float64 // per-minute chance-arrival multiplier
	GoalConversion     float64 // own shot-to-goal multiplier
	ConcededConversion float64 // how dangerous this side's concessions are
	CardRate           float64 // card thresholds multiplier when this side fouls
	StaminaDecay       float64 // per-minute tank drain, relative to (1/90)
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
const EngineVersion = "1.5-proposal"

// RefereeBiasSource values.
const (
	RefereeSourceNone       = "none"
	RefereeSourceHomeCrowd  = "home_crowd"
	RefereeSourceReputation = "reputation_gap"
)

// Style keys — the product-approved Simple-Mode tactical styles (S05-01).
const (
	StyleBalanced   = "balanced"
	StylePossession = "possession"
	StyleGegenpress = "gegenpress"
	StyleLowBlock   = "low_block"
	StyleDirect     = "direct"
)

// StyleKeys are the five style keys, for validation/iteration by orchestration
// layers (internal/tactics, HTTP handlers, the engine itself).
var StyleKeys = []string{StyleBalanced, StylePossession, StyleGegenpress, StyleLowBlock, StyleDirect}

// IsStyle reports whether key is one of the product-approved style keys.
func IsStyle(key string) bool {
	for _, k := range StyleKeys {
		if k == key {
			return true
		}
	}
	return false
}

// identityStyle is the fallback block when Styles is empty: every lever is
// neutral AND the tank drains at the healthy (1/90) rate.
var identityStyle = StyleSpec{
	PossessionShift: 0, ChanceVolume: 1, GoalConversion: 1,
	ConcededConversion: 1, CardRate: 1, StaminaDecay: 1,
}

// StyleSpecs is the v1.5 style block — midpoints of the S05-01 matrix
// (matchsim_addendum_v1.5.md §"Style keys"; numbers marked proposal, tuning
// data swapped by a PM pass).
func StyleSpecs() map[string]StyleSpec {
	return map[string]StyleSpec{
		StyleBalanced:   identityStyle,
		StylePossession: {PossessionShift: 0.20, ChanceVolume: 0.90, GoalConversion: 1.20, ConcededConversion: 1.20, CardRate: 0.80, StaminaDecay: 1.00},
		StyleGegenpress: {PossessionShift: 0.125, ChanceVolume: 1.25, GoalConversion: 1.40, ConcededConversion: 1.80, CardRate: 1.40, StaminaDecay: 1.35},
		StyleLowBlock:   {PossessionShift: -0.175, ChanceVolume: 0.70, GoalConversion: 2.20, ConcededConversion: 0.60, CardRate: 1.15, StaminaDecay: 0.85},
		StyleDirect:     {PossessionShift: -0.075, ChanceVolume: 1.15, GoalConversion: 0.80, ConcededConversion: 1.00, CardRate: 1.10, StaminaDecay: 1.05},
	}
}

// styleSpec resolves a team's style key to its active numeric block, falling
// back through DefaultStyle to the identity block.
func (t Tuning) styleSpec(key string) StyleSpec {
	if key == "" {
		key = t.DefaultStyle
	}
	if key == "" {
		key = StyleBalanced
	}
	if s, ok := t.Styles[key]; ok {
		return s
	}
	if s, ok := t.Styles[t.DefaultStyle]; ok {
		return s
	}
	return identityStyle
}

// clampFitness normalises Team.Fitness: <=0 means "not computed" and plays a
// fresh squad (1.0); values above 1 clamp to 1 (the domain is [0,1]).
func clampFitness(f float64) float64 {
	if f <= 0 || f > 1 {
		return 1
	}
	return f
}

// clampShare keeps the style-shifted possession share inside the documented
// band (addendum v1.5).
func clampShare(p float64) float64 {
	if p < 0.05 {
		return 0.05
	}
	if p > 0.95 {
		return 0.95
	}
	return p
}

// clampPenalty bounds the stamina-deficit penalty to [0, FatiguePenaltyMax].
func clampPenalty(x, max float64) float64 {
	if x < 0 {
		return 0
	}
	if x > max {
		return max
	}
	return x
}

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
	Styles:                     StyleSpecs(),
	DefaultStyle:               StyleBalanced,
	SubFitness:                 0.5,
	FatigueStartMinute:         76,
	FatiguePenaltyScale:        0.5,
	FatiguePenaltyMax:          0.15,
}

// DefaultTuning returns a copy of the proposed tuning so callers can mutate
// their own instance without corrupting the shared proposal (slices AND style
// maps are copied).
func DefaultTuning() Tuning {
	t := ProposedTuning
	t.SubWindows = append([]int{}, ProposedTuning.SubWindows...)
	t.Styles = make(map[string]StyleSpec, len(ProposedTuning.Styles))
	for k, v := range ProposedTuning.Styles {
		t.Styles[k] = v
	}
	return t
}
