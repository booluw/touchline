// Package form owns the club FormState subsystem (matchsim addendum v1.2
// §2.2, approved): a rolling, persistent EWMA of recent performance vs.
// expectation that produces the FormFactor multiplier consumed by the match
// engine via Team.FormFactor. It is orchestration-layer code — pkg/matchsim
// stays pure — and is deliberately small: one table, one EWMA, one read-model
// string.
//
// Model contract (interpretation pinned in the matchsim README):
//
//   - CurrentRating is a multiplier in [0.85, 1.15], centred at 1.0 (neutral).
//   - Each completed match blends the neutral-centred ResultQuality into the
//     EWMA with alpha (default 0.2): quality ≈ 1.0 means "performed exactly as
//     expected", so the rating naturally drifts back toward 1.0 whenever a
//     club's form normalizes. This is what produces multi-week runs and
//     slumps without ever dominating underlying squad quality.
//   - ResultQuality maps an actual-vs-expected goal difference onto that
//     neutral-centred input; the divisor (ExpectedGDDivisor) is a PROPOSAL
//     awaiting PM tuning sign-off, governing how many GD-swings over
//     expectation reach the band edges.
//   - LastUpdatedTick mirrors world.worlds.current_tick at the moment the
//     form row was written, so the same world saves replay the same form
//     timeline.
package form

import (
	"github.com/google/uuid"
)

// Tuning constants for the form model. Alpha and the rating band are the
// approved v1.2 values; ExpectedGDDivisor is a documented proposal.
const (
	// AlphaDefault decays each result's influence; 0.2 means the most recent
	// match moves the rating by 20% of its distance (section 2.2's example).
	AlphaDefault float64 = 0.2

	// MinRating/MaxRating clamp the multiplier band (±15%).
	MinRating float64 = 0.85
	MaxRating float64 = 1.15

	// ExpectedGDDivisor scales a goal-difference swing above/below
	// expectation onto the neutral-centred quality input (PROPOSAL).
	ExpectedGDDivisor float64 = 4.0
)

// Out-of-band sentinels for results passed to FormStringFromResults.
const (
	ResultWin  = "W"
	ResultDraw = "D"
	ResultLoss = "L"
	ResultNil  = "-"
)

// FormState is one club's persisted rolling form.
type FormState struct {
	ClubID uuid.UUID `json:"club_id"`
	// CurrentRating is the EWMA multiplier, clamped to [MinRating, MaxRating].
	CurrentRating float64 `json:"current_rating"`
	// LastUpdatedTick is world.worlds.current_tick at the row's last write.
	LastUpdatedTick int64 `json:"last_updated_tick"`
	// FormString is the Part 5 §3 read-model: the most recent results
	// oldest-first, joined with '-', length <= 5. Callers build it via
	// FormStringFromResults.
	FormString string `json:"form_string"`
}

// Neutral returns the unplayed (rating 1.0) form state for a club — the
// default every new club and every absent row resolves to.
func Neutral(clubID uuid.UUID, tick int64) FormState {
	return FormState{
		ClubID:          clubID,
		CurrentRating:   1.0,
		LastUpdatedTick: tick,
		FormString:      "",
	}
}

// Update blends one match's ResultQuality into the EWMA and returns the next
// form state, clamped to [MinRating, MaxRating]:
//
//	current = clamp((1-α) · prev + α · quality)
//
// quality must be the neutral-centred ResultQuality (1.0 = as expected); a
// zero or negative alpha falls back to AlphaDefault.
func Update(prev FormState, quality, alpha float64, tick int64) FormState {
	if alpha <= 0 {
		alpha = AlphaDefault
	}
	next := prev
	next.CurrentRating = (1-alpha)*prev.CurrentRating + alpha*quality
	next.CurrentRating = clampRating(next.CurrentRating)
	next.LastUpdatedTick = tick
	return next
}

// ResultQuality maps an actual-vs-expected goal difference onto the
// neutral-centred quality input the EWMA expects: 1.0 when a team lands
// exactly on its expected difference, >1.0 when it outperforms, <1.0 when it
// underperforms. (actualGD − expectedGD) counts from the RESULT OWNER's
// perspective — callers pass the per-club signed difference.
func ResultQuality(actualGD, expectedGD int) float64 {
	return ResultQualityDelta(float64(actualGD - expectedGD))
}

// ResultQualityDelta is the continuous twin of ResultQuality: it accepts the
// signed actual-vs-expected goal-difference delta directly, without integer
// rounding. internal/match uses it because the engine-mirrored expectation
// (computed from matchsim.GoalWeight incl. the home advantage) is naturally a
// sub-integer margin, and rounding it to an int would wipe almost every match's
// form signal.
func ResultQualityDelta(delta float64) float64 {
	// Mirror the engine band so quality itself cannot overshoot: Update's
	// clamp is the authoritative bound, this just keeps the input sane.
	return clampQuality(1 + delta/ExpectedGDDivisor)
}

// FormStringFromResults renders the 5-match read-model string (oldest first,
// joined with '-'). The provided slice may carry up to 5 most-recent results
// in order; anything beyond the 5th is dropped, absent slots are padded with
// '-' so managers always see a fixed-width string.
func FormStringFromResults(results []string) string {
	if len(results) > 5 {
		results = results[len(results)-5:]
	}
	out := make([]byte, 0, 9)
	for _, r := range results {
		switch r {
		case ResultWin, ResultDraw, ResultLoss:
		default:
			r = ResultNil
		}
		if len(out) > 0 {
			out = append(out, '-')
		}
		out = append(out, r...)
	}
	return string(out)
}

// AppendResult preserves the trailing-5 window when one more result is known:
// oldest first, at most five entries.
func AppendResult(window []string, result string) []string {
	window = append(window, result)
	if len(window) > 5 {
		window = window[len(window)-5:]
	}
	return window
}

func clampRating(v float64) float64 {
	if v < MinRating {
		return MinRating
	}
	if v > MaxRating {
		return MaxRating
	}
	return v
}

func clampQuality(q float64) float64 {
	// Allow the input a little slack beyond the band so a huge upset still
	// moves the needle, but cap the absurdities (a +20 GD outlier is not a
	// +4.0 quality input).
	if q < 0.5 {
		return 0.5
	}
	if q > 1.5 {
		return 1.5
	}
	return q
}