// Scouting and pre-match warning labels. PM sign-off (addendum v1.4 Part 7
// §2) requires the influence of hidden traits to be transparent: they are
// surfaced to managers as fixed, tunable scouting tags before they act, and
// as a ranked lineup warning at matchday. Both render through the shared
// package explanation pattern, so nothing is ever recomputed on render.
package squad

import (
	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/explanation"
)

// TraitTagThresholds2018 is the default threshold set meaning "a player at or
// beyond these values carries the corresponding tag". Each threshold is
// independently tunable — recalibration is a data change, not a code change.
var TraitTagThresholds2018 = TraitTagThresholds{
	ThrivesUnderPressureMin: 80, // pressure_handling >= 80
	CalcifiedDropbackMax:    90, // adaptability <= 10 → "Set In Their Ways" proposal
	VolatileTemperamentMax:  30, // temperament <= 30
	InconsistentMax:         30, // consistency <= 30
}

// TraitTagThresholds bounds the ScoutingTags classifier; all fields are
// additive proposals unless a value is explicitly marked APPROVED.
type TraitTagThresholds struct {
	ThrivesUnderPressureMin int
	CalcifiedDropbackMax    int
	VolatileTemperamentMax  int
	InconsistentMax         int
}

// ScoutingTags classifies a player's hidden traits into the manager-visible
// tag set (Part 7 §2), in a stable display order. Tags are pure descriptions:
// the matchday effect of the same traits is computed independently by
// BuildSquadRatings/ComputePlayerPerformanceFactor.
func ScoutingTags(h PlayerHiddenTraitsSnapshot, t TraitTagThresholds) []string {
	var tags []string
	if h.PressureHandling >= t.ThrivesUnderPressureMin {
		tags = append(tags, "Thrives Under Pressure")
	}
	if h.Adaptability <= 100-t.CalcifiedDropbackMax {
		tags = append(tags, "Set In Their Ways")
	}
	if h.Temperament <= t.VolatileTemperamentMax {
		tags = append(tags, "Volatile Temperament")
	}
	if h.Consistency <= t.InconsistentMax {
		tags = append(tags, "Inconsistent")
	}
	return tags
}

// LineupWarningThreshold is the default factor (rounded to integer percent)
// at or below which a key player's matchday projection is flagged for the
// manager: currently the flat 95% mark, no family penalty yet — CARRIED
// PROPOSAL, see the fix-queue in specs/dev/mapping-01.md.
const LineupWarningThreshold float64 = 0.95

// LineupWarningSubject identifies the warning in the world.events explanation
// column.
const LineupWarningSubject = "lineup_warning"

// LineupWarning is the pre-match objection for one key player whose projected
// performance factor is at or below LineupWarningThreshold.
type LineupWarning struct {
	PlayerID       uuid.UUID                `json:"player_id"`
	PolicyDecision *explanation.Explanation `json:"policy_decision"`
}

// LineupWarningFor builds the projection warning for one key player, or
// (warning, false) if the factor does not cross the threshold. The explanation
// decomposes the factor transparently — consistency variance, high-stakes
// temperament/pressure divergence, and the player's own sentiment — so the
// manager can see exactly which trait dragged it. STAKE NOTE: when clamping
// or rounding engaged, the individual delta integers on factors may not sum
// exactly to Score; policy_decision is narrative-first by design (§54).
func LineupWarningFor(in PlayerPerformanceInput, fc FixtureContext, f PlayerPerformanceFactor, seed int64, t SquadTuning, thr float64) (LineupWarning, bool) {
	if thr <= 0 {
		thr = LineupWarningThreshold
	}
	if f.Factor > thr {
		return LineupWarning{}, false
	}

	score := int((f.Factor - 1) * 100)
	d := newRNG(seed, hashPlayer(in.PlayerID)).nextFloat()
	width := t.ConsistencyMinWidth +
		(1-float64(clampScore(in.Consistency))/100)*(t.ConsistencyMaxWidth-t.ConsistencyMinWidth)
	variance := int(mathRound((2*d - 1) * width * 100))

	exp := explanation.New(LineupWarningSubject, score)
	if variance != 0 {
		exp.Add("Consistency variance", variance)
	}
	if fc.IsHighStakes() {
		pressure := int(mathRound((float64(clampScore(in.PressureHandling)) - 50) / 50 * t.StakesPressureSpan * 100))
		temper := int(mathRound((float64(clampScore(in.Temperament)) - 50) / 50 * t.StakesTemperamentSpan * 100))
		if pressure != 0 {
			exp.Add("Pressure handling", pressure)
		}
		if temper != 0 {
			exp.Add("Temperament", temper)
		}
	}
	if in.CurrentSentiment != 0 {
		exp.Add("Player sentiment", int(mathRound(float64(clampSentiment(in.CurrentSentiment))/100*t.SentimentSpan*100)))
	}

	return LineupWarning{PlayerID: in.PlayerID, PolicyDecision: exp}, true
}

func mathRound(v float64) float64 {
	if v >= 0 {
		return float64(int64(v + 0.5))
	}
	return float64(int64(v - 0.5))
}
