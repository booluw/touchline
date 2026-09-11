// Package explanation defines the shared, cross-cutting "why" contract for
// every scored or consequential decision in Touchline (PRD section 54, technical
// plan section 8). One pattern is built here and reused by every producer —
// board confidence, transfer desire, match outcomes, AI bids, news — so the UI
// and news generator render stored reasons instead of recalculating decisions.
//
// # Structure
//
// An Explanation identifies a decision by Subject, carries the resulting Score,
// and lists the contributing Factors as labeled deltas:
//
//	{"subject":"board_confidence","score":-14,"factors":[{"label":"Wage bill 18% above structure","delta":-12}]}
//
// Score represents the resulting total only where the domain defines one.
// Factors are the authoritative "why"; they are not required to sum to Score
// (narrative reasons such as an AI club's bidding motivation carry no number).
// Validate reports a broken sum only when a producer opts in.
//
// # Persistence and rendering boundary
//
// Explanations are persisted with their causing event in the
// world.events.explanation JSONB column so a consumer can render them without
// recomputing anything. This package owns the wire shape and the rendering
// boundary: producers store structured factors, and Render produces the
// canonical §54 lines. Additive changes to the JSON shape are allowed; no field
// may be repurposed or removed without a versioned contract change.
package explanation

import "fmt"

// Explanation is the reasoning breakdown behind a major game decision.
type Explanation struct {
	Subject string   `json:"subject"`
	Score   int      `json:"score"`
	Factors []Factor `json:"factors"`
}

// Factor is a single labeled contributor to the decision (PRD section 54).
type Factor struct {
	Label string `json:"label"`
	Delta int    `json:"delta"`
}

// New returns an Explanation for the given subject with its resulting score
// and no factors yet. Factors are added in order via Add.
func New(subject string, score int) *Explanation {
	return &Explanation{Subject: subject, Score: score, Factors: []Factor{}}
}

// Add appends a labeled delta factor, preserving display order.
func (e *Explanation) Add(label string, delta int) *Explanation {
	e.Factors = append(e.Factors, Factor{Label: label, Delta: delta})
	return e
}

// Validate reports whether the factor deltas sum to the stated score. It is
// opt-in: producers whose explanations are narrative (no true numeric total)
// are not expected to call it. An explanation with no factors validates.
func (e *Explanation) Validate() error {
	var sum int
	for _, f := range e.Factors {
		sum += f.Delta
	}
	if sum != e.Score {
		return fmt.Errorf("%s: factor deltas sum to %d but score is %d", e.Subject, sum, e.Score)
	}
	return nil
}

// Render produces the canonical PRD section 54 explanation lines, one per
// factor, in stored order. Consumers (UI, news) may render their own way; this
// is the server-side reference rendering and never recalculates the decision.
func (e *Explanation) Render() []string {
	lines := make([]string, 0, len(e.Factors))
	for _, f := range e.Factors {
		lines = append(lines, fmt.Sprintf("%s: %+d", f.Label, f.Delta))
	}
	return lines
}
