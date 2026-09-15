package board

import (
	"fmt"
	"math"
	"strconv"
)

// All decision numbers in this file are PROPOSAL data until PM tuning sign-off
// (source of truth: docs/design/board-numerics.md). Recalibration means editing
// constants here, never steering logic.

const (
	// PerformanceSwingPerPosition moves `performance` when a club finishes one
	// league place above/below its mandate target (neutral baseline 50).
	PerformanceSwingPerPosition = 8

	// ExpectationPerMet / ExpectationPerBroken nudge `expectations` per
	// resolved mandate.
	ExpectationPerMet    = 10
	ExpectationPerBroken = 15

	// FinancialWageRatioScale weights how far under the wage budget the club
	// sits; a breakeven/positive operating result adds/removes
	// FinancialProfitSignal.
	FinancialWageRatioScale = 40
	FinancialProfitSignal   = 10
	FinancialNoDataScore    = 60

	// SupporterSentimentAlpha is the EWMA blend of results into supporter
	// sentiment each weekly review.
	SupporterSentimentAlpha = 0.20
	SupporterSentimentMin   = 15
	SupporterSentimentMax   = 95

	// RelationshipPatienceShare lets club DNA patience soften/pressure the
	// board relationship score.
	RelationshipPatienceShare = 5

	// DnaAlignmentPerStrategicMet / PerFinancialBroken move club-dna alignment
	// on resolved strategic/financial mandates (deeper DNA adherence waits on
	// S10-03).
	DnaAlignmentPerStrategicMet    = 15
	DnaAlignmentPerFinancialBroken = 25

	// AlternativesReputationPerPoint: world-scoped reputation (manager
	// career log) resilience against replacement pressure.
	AlternativesReputationPerPoint = 5
	AlternativesMin                = 10
	AlternativesMax                = 90

	// SackThresholdTotal is the confidence line below which a human-managed
	// club's board fires the manager on the weekly review.
	SackThresholdTotal = 25

	// Mandate grading windows (0-100 scale inputs).
	FinishSlackEarly       = 3    // league places of forgiveness before 40% of season
	FinishSlackMid         = 2    // ...before 80%
	FinishSlackLate        = 1    // ...before the final whistle
	FinishBrokenMargin     = 6    // clearly-off position marks a finish mandate broken
	PointsTargetWindow     = 10   // points under the scaled target before broken
	WageTolerancePct       = 0.05 // committed wage may exceed budget by 5%
	OperatingLossWageShare = 0.25 // profit loss beyond ¼ of the wage budget is broken

	// Negotiation bounds (MVP: sporting targets only).
	NegotiationFinishPlaces = 3
	NegotiationPointsPoints = 8
	NegotiationFinishMin    = 1
	NegotiationFinishMax    = 24
	NegotiationPointsMin    = 10
	NegotiationPointsMax    = 95
)

// personaWeights maps each board persona to the seven factor weights
// (performance, expectations, financial, board_relationship, club_dna_alignment,
// supporter_sentiment, alternatives). Each row sums to 1.0.
var personaWeights = map[Persona][7]float64{
	PersonaPatientOwner:          {0.18, 0.12, 0.18, 0.18, 0.14, 0.10, 0.10},
	PersonaDemandingOwner:        {0.30, 0.25, 0.12, 0.10, 0.10, 0.08, 0.05},
	PersonaFinancialConservative: {0.18, 0.12, 0.30, 0.12, 0.10, 0.08, 0.10},
	PersonaAcademyOwner:          {0.12, 0.10, 0.12, 0.12, 0.32, 0.10, 0.12},
	PersonaPrestigeOwner:         {0.28, 0.22, 0.10, 0.10, 0.12, 0.12, 0.06},
	PersonaPoliticalBoard:        {0.18, 0.18, 0.14, 0.22, 0.08, 0.12, 0.08},
}

// personaNegotiationTolerance is the maximum demotion (worsening) of a
// sporting target a persona will still accept. None may exceed the window.
var personaNegotiationTolerance = map[Persona]int{
	PersonaPatientOwner:          3,
	PersonaFinancialConservative: 2,
	PersonaAcademyOwner:          2,
	PersonaPoliticalBoard:        1,
	PersonaDemandingOwner:        0,
	PersonaPrestigeOwner:         0,
}

// personaIsKnown reports whether p has a weight row (unknown personas fall
// back to the political-board weights rather than panic).
func personaIsKnown(p Persona) bool {
	_, ok := personaWeights[p]
	return ok
}

func weights(p Persona) [7]float64 {
	if w, ok := personaWeights[p]; ok {
		return w
	}
	return personaWeights[PersonaPoliticalBoard]
}

func negotiationTolerance(p Persona) int {
	if t, ok := personaNegotiationTolerance[p]; ok {
		return t
	}
	return personaNegotiationTolerance[PersonaPoliticalBoard]
}

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// performanceScore scores league position against the mandate expected finish
// (the smaller the position the better). position is nil when the club has no
// domestic league standings yet — neutral 50.
func performanceScore(position *int, finish int) int {
	if position == nil {
		return 50
	}
	return clamp(50+PerformanceSwingPerPosition*(finish-*position), 0, 100)
}

// expectationsScore rewards met and punishes broken mandates.
func expectationsScore(met, broken int) int {
	return clamp(50+ExpectationPerMet*met-ExpectationPerBroken*broken, 0, 100)
}

// financialScore reads the wage ratio and operating result (scale-free; the
// budget/annual pairs must be in the same unit, always pence in this codebase).
func financialScore(operatingProfit, wageBudget, committedAnnual int64) int {
	if wageBudget <= 0 {
		if committedAnnual > 0 {
			return FinancialNoDataScore - FinancialProfitSignal
		}
		return FinancialNoDataScore
	}
	ratio := float64(committedAnnual) / float64(wageBudget)
	if ratio > 1 {
		ratio = 1
	}
	score := 50 + math.Round(FinancialWageRatioScale*(1-ratio))
	if operatingProfit >= 0 {
		score += FinancialProfitSignal
	} else {
		score -= FinancialProfitSignal
	}
	return clamp(int(score), 0, 100)
}

// relationshipScore blends the kept-mandate ratio with club DNA patience.
func relationshipScore(met, broken, patience int) int {
	score := 50 + (patience-50)/RelationshipPatienceShare
	if resolved := met + broken; resolved > 0 {
		score += 25 * (met - broken) / resolved
	}
	return clamp(score, 0, 100)
}

// dnaAlignmentScore responds to strategic/financial promises kept or broken.
func dnaAlignmentScore(metStrategic, metFinancial, brokenStrategic, brokenFinancial int) int {
	score := 50 + DnaAlignmentPerStrategicMet*metStrategic
	score += DnaAlignmentPerStrategicMet * metFinancial
	score -= DnaAlignmentPerFinancialBroken * brokenStrategic
	score -= DnaAlignmentPerFinancialBroken * brokenFinancial
	return clamp(score, 0, 100)
}

// supporterScore is the (already clamped) stored supporter sentiment.
func supporterScore(sentiment int) int {
	return clamp(sentiment, 0, 100)
}

// alternativesScore turns world-scoped manager reputation into replacement
// pressure resilience.
func alternativesScore(reputation int) int {
	return clamp(50+AlternativesReputationPerPoint*reputation, AlternativesMin, AlternativesMax)
}

// totalScore weighs the seven 0-100 factors by persona. It is defined as the
// SUM of the rounded weighted contributions, so the explanation factors
// (one per factor, delta = the rounded contribution) validate exactly.
func totalScore(p Persona, s FactorScores) (int, [7]int) {
	w := weights(p)
	var total int
	var deltas [7]int
	vals := [7]int{s.Performance, s.Expectations, s.Financial, s.BoardRelationship,
		s.ClubDNAAlignment, s.SupporterSentiment, s.Alternatives}
	for i := 0; i < 7; i++ {
		deltas[i] = int(math.Round(w[i] * float64(vals[i])))
		total += deltas[i]
	}
	return clamp(total, 0, 100), deltas
}

// expectedFinish derives a club's expected league finish from its DNA ambition
// and patience: more ambitious boards expect higher table spots; patient ones
// grant a position or two.
func expectedFinish(ambition, patience int) int {
	finish := float64(110-ambition) / 8.0
	if patience >= 70 {
		finish += 1.5
	}
	if finish < 1 {
		finish = 1
	}
	if finish > 24 {
		finish = 24
	}
	return int(math.Round(finish))
}

// expectedPoints converts an expected finish into a season points target.
func expectedPoints(finish int) int {
	return clamp(int(math.Round(88-3.5*float64(finish))), NegotiationPointsMin, NegotiationPointsMax)
}

// mandateSeed is one row generated for a new (club, manager, season).
type mandateSeed struct {
	Category    string
	Description string
	TargetType  string
	TargetValue string
}

// mandateSet builds the deterministic four-category mandate set.
func mandateSet(finish, points int) []mandateSeed {
	return []mandateSeed{
		{CatPrimary, fmt.Sprintf("finish the league season at or above position %d", finish),
			TargetLeagueFinish, strconv.Itoa(finish)},
		{CatSecondary, fmt.Sprintf("collect at least %d league points", points),
			TargetPointsTarget, strconv.Itoa(points)},
		{CatStrategic, "maintain a break-even operating balance",
			TargetOperatingBank, "0"},
		{CatFinancial, "keep the wage bill within the board-allocated structure",
			TargetWageStructure, "0"},
	}
}

func parseInt(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// finishSlack shrinks as the season progresses: early tables are forgiven the
// most, the closing stretch the least.
func finishSlack(progress float64, seasonComplete bool) int {
	if seasonComplete {
		return 0
	}
	switch {
	case progress < 0.4:
		return FinishSlackEarly
	case progress < 0.8:
		return FinishSlackMid
	default:
		return FinishSlackLate
	}
}

// evaluateFinish grades a league_finish mandate: met when within slack,
// broken when clearly adrift (or past the close with the target missed).
func evaluateFinish(position int, finish int, progress float64, seasonComplete bool) (met, broken bool) {
	if seasonComplete {
		return position <= finish, position > finish
	}
	slack := finishSlack(progress, false)
	if position <= finish+slack {
		return true, false
	}
	if position > finish+FinishBrokenMargin {
		return false, true
	}
	return false, false
}

// evaluatePoints grades a points_target mandate against the scaled season
// target, with a miss window so early blips don't count against the manager.
// A season-complete miss is strict: any shortfall is broken.
func evaluatePoints(points, target int, progress float64, seasonComplete bool) (met, broken bool) {
	scaled := target
	if !seasonComplete {
		scaled = int(math.Round(float64(target) * progress))
	}
	if points >= scaled {
		return true, false
	}
	if seasonComplete || points <= scaled-PointsTargetWindow {
		return false, true
	}
	return false, false
}

// evaluateWageStructure grades wage discipline: committed annual wages within
// budget × (return wants + tolerance). No budget means nothing to grade.
func evaluateWageStructure(committedAnnual, wageBudget int64, targetPP int) (met, broken bool) {
	if wageBudget <= 0 {
		return false, false
	}
	allowance := float64(wageBudget) * (1 + float64(targetPP)/100 + WageTolerancePct)
	if float64(committedAnnual) <= allowance {
		return true, false
	}
	return false, true
}

// evaluateOperatingBalance grades a break-even operating mandate; a small
// shortfall stays unresolved early on (wages are front-loaded).
func evaluateOperatingBalance(opProfit, wageBudget int64) (met, broken bool) {
	if opProfit >= 0 {
		return true, false
	}
	floor := int64(float64(wageBudget) * OperatingLossWageShare)
	if -opProfit > floor || wageBudget <= 0 {
		return false, true
	}
	return false, false
}

// supporterBlend is the EWMA sentiment update from the computed performance.
func supporterBlend(current int, performance int) int {
	next := int(math.Round(float64(current) + SupporterSentimentAlpha*(float64(performance)-float64(current))))
	return clamp(next, SupporterSentimentMin, SupporterSentimentMax)
}

// negotiationDelta reports the signed proposal movement for a numeric target:
// positive means the manager asked the board to soften the target (worse for
// the club for finish, easier for points). finish targets: smaller is better.
func negotiationDelta(targetType string, current, proposal int) int {
	if targetType == TargetLeagueFinish {
		return proposal - current
	}
	return current - proposal
}

// withinNegotiationWindow reports whether the proposal stays in the bounded
// window for its target type.
func withinNegotiationWindow(targetType string, current, proposal int) bool {
	switch targetType {
	case TargetLeagueFinish:
		if proposal < NegotiationFinishMin || proposal > NegotiationFinishMax {
			return false
		}
		return abs(proposal-current) <= NegotiationFinishPlaces
	case TargetPointsTarget:
		if proposal < NegotiationPointsMin || proposal > NegotiationPointsMax {
			return false
		}
		return abs(proposal-current) <= NegotiationPointsPoints
	default:
		return false
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
