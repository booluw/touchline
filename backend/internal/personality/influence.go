package personality

import "fmt"

// ---- Training & recovery influence seam (S09-01, acceptance criterion 9) ----
//
// This file is the SINGLE documented seam where personality reaches the player
// developmental surfaces (training programme + injury treatment/recovery). It is
// deliberately a seam, NOT a rewrite:
//
//   - The legacy training/injury engines in internal/training and internal/injury
//     are NOT edited here. Their legitimacy as the authoritative programme/
//     recovery numerics is untouched (see docs/design/player-personality-and-
//     hidden-traits.md, "What this does NOT change").
//   - Personality influences those surfaces only where a caller EXPLICITLY
//     asks the engine to quantify it: ManagerTrainingInfluenceFor hatches a
//     deterministic, inbound-only modifier computed from the pure TraitSet.
//
// The interface below is deliberately small and pure so that a V2 that wants
// personality to feed the training planner (intensity absorption, learning-
// speed s-curves) or the medical desk (recovery discipline, rush-return risk)
// can swap in a richer implementation WITHOUT touching this contract or the
// reaction matrix.

// TrainingInfluenceModifier is the deterministic, personality-derived adjustment
// to how a player absorbs a training programme or a recovery period. All values
// are pure functions of the TraitSet (no RNG, no DB, no time). A caller (the
// training/recovery seam consumer) multiplies its own base numerics by these
// factors — direction and existence are owned here, magnitude is a data-only
// tuning slider (OPD-30 training-direction table in docs/product_manager.md).
type TrainingInfluenceModifier struct {
	// IntensityHash is an opaque, deterministic digest so downstream logs can
	// fingerprint which seam revision produced a factor. Not a security digest.
	IntensityHash string `json:"intensity_hash"`
	// TrainingAbsorption lies around 1.0: >1 means the player rides a given
	// workload harder (professionalism), <1 means the load bites deep and the
	// player sours (low professionalism + volatility).
	TrainingAbsorption float64 `json:"training_absorption"`

	// LearningSpeedBias is the hidden learning-speed influence on how much of
	// a completed drill converts into skill growth. 1.0 is the neutral seam
	// default; LearningSpeed >= 9 pushes >1.0.
	LearningSpeedBias float64 `json:"learning_speed_bias"`

	// RecoveryDiscipline: how cleanly the player executes a treatment/recovery
	// programme (professionalism + patience). >1 speeds healthy recovery;
	// <1 drags it and raises the chance a rushed return opens (see injury seam).
	RecoveryDiscipline float64 `json:"recovery_discipline"`
}

// InfluenceAdapter is the seam contract the developmental surfaces consume.
type InfluenceAdapter interface {
	// TrainingInfluenceFor computes the modifiers for one player. Managers may
	// see the absorption/bias numbers ("why did my drill land so flat") — the
	// hidden learning_speed base is NEVER surfaced, only its derived bias.
	TrainingInfluenceFor(t TraitSet) TrainingInfluenceModifier
}

// DefaultInfluence is the shipping, deterministic adapter. It is the only
// concrete implementation in this package and is safe for concurrent use.
type DefaultInfluence struct{}

// NewDefaultInfluence returns the seam adapter used by callers that opt in.
func NewDefaultInfluence() *DefaultInfluence { return &DefaultInfluence{} }

// Numerics — personality → developmental-surface coefficients. These are
// TUNING-DATA honored as the documented OPD-30 defaults (docs/product_manager.md
// row OPD-30). Recalibration is a one-line, data-only change; the engine's
// determinism and the seam contract never move.
const (
	// Professionalism drives workload absorption. A professional player at 10
	// rides ~20% more intensity before morale bends; a low-professional, high-
	// volatility player absorbs ~25% less (the "reads the extra load as
	// punishment" branch in reaction.go).
	absorptionProFloor       = 0.85
	absorptionProPerPoint    = 0.04
	absorptionVolPenaltyHigh = 0.20

	// LearningSpeed: 1.0 neutral at the median (5); +4% per point above 5,
	// -4% per point below, clamped to [0.5, 1.5].
	learningBiasPerPoint = 0.04
	learningBiasFloor    = 0.50
	learningBiasCeil     = 1.50

	// Recovery discipline: baseline 1.0, +5% per point of professionalism above
	// 2 and +3% per point of patience above 2, clamped to [0.5, 1.6]. This is
	// the seam that would let a professional recover faster without touching
	// internal/injury's authoritative healing clock.
	recoveryProPerPoint = 0.05
	recoveryPatPerPoint = 0.03
	recoveryFloor       = 0.50
	recoveryCeil        = 1.60
)

// TrainingInfluenceFor implements InfluenceAdapter.
func (d *DefaultInfluence) TrainingInfluenceFor(t TraitSet) TrainingInfluenceModifier {
	// Training absorption: start from the professional baseline, then penalise
	// a volatile player who also reads the load as punishment.
	absorption := absorptionProFloor + float64(t.Professionalism-1)*absorptionProPerPoint
	if t.Volatility >= 8 {
		absorption -= absorptionVolPenaltyHigh
	}
	absorption = clamp(absorption, 0.5, 1.6)

	// Learning-speed bias: hidden trait, only its derived bias ever leaves the
	// engine. Median 5 → neutral.
	learningBias := 1.0 + float64(t.LearningSpeed-5)*learningBiasPerPoint
	learningBias = clamp(learningBias, learningBiasFloor, learningBiasCeil)

	// Recovery discipline: professionalism + patience.
	var rec float64 = 1.0
	if t.Professionalism > 2 {
		rec += float64(t.Professionalism-2) * recoveryProPerPoint
	}
	if t.Patience > 2 {
		rec += float64(t.Patience-2) * recoveryPatPerPoint
	}
	rec = clamp(rec, recoveryFloor, recoveryCeil)

	// IntensityHash: deterministic fingerprint of (svc, trait, professionalism,
	// volatility, learning_speed, patience) so consumers can audit which seam
	// shape produced a factor without decoding the trait surface.
	payload := t.Professionalism*1000000 + t.Volatility*10000 + t.LearningSpeed*100 + t.Patience
	hash := uint64(14695981039346656037) // FNV-1a 64 offset basis
	for _, b := range []byte("training-influence:" + fmt.Sprintf("%d", payload)) {
		hash ^= uint64(b)
		hash *= 1099511628211
	}
	return TrainingInfluenceModifier{
		IntensityHash:      fmt.Sprintf("%016x", hash),
		TrainingAbsorption: absorption,
		LearningSpeedBias:  learningBias,
		RecoveryDiscipline: rec,
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
