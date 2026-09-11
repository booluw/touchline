package world

// Explanation provides the reasoning breakdown behind any major game decision.
// This is the cross-cutting explainability layer (PRD section 54).
type Explanation struct {
	Subject string              `json:"subject"` // "board_confidence", "transfer_desire", etc.
	Score   int                 `json:"score"`
	Factors []ExplanationFactor `json:"factors"`
}

type ExplanationFactor struct {
	Label string `json:"label"` // "Wage bill 18% above structure"
	Delta int    `json:"delta"` // -12
}
