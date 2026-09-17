// Package injury owns the deterministic injury engine (S08-03, OPD-03 injury
// probability curve). The math is pure — no I/O, no global state — and all
// randomness flows from the caller-supplied seed so a redelivered match or
// weekly tick reproduces identical type/severity/duration. The engine is
// deliberately blind to the matchsim replay contract: matchsim emits WHO is
// injured (its frozen v1.6 attribution), this package decides the rest from a
// SEPARATE stream (internal/injury/stream.go), so the canonical match digest
// is never touched. All numerics are proposal constants living here and in
// docs/design/injury-numerics.md; recalibration is a data-only change.
package injury

import (
	"math"
	"math/rand"

	"github.com/touchline/backend/pkg/explanation"
)

// Type is an injury classification. Values must match the
// player.injuries.injury_type CHECK constraint.
type Type string

const (
	TypeMuscle     Type = "muscle"
	TypeLigament   Type = "ligament"
	TypeBone       Type = "bone"
	TypeConcussion Type = "concussion"
	TypeIllness    Type = "illness"
	TypeRecurring  Type = "recurring"
)

// Input is every number Evaluate may look at. It is a pure function input:
// the same Input always yields the same Outcome.
type Input struct {
	Seed           int64   // deterministic stream seed (match or weekly)
	Fatigue        float64 // player_condition.fatigue, clamped to [0,1]
	Susceptibility int     // player_hidden_traits.injury_susceptibility, 0..100
	MedicalLevel   int     // club.facilities medical level, 1..10 (neutral 5 default)
	Minutes        int     // minutes played in the causing match (0 for training)
	FoulContext    bool    // true when caused by a physical tackle/foul in play
	RecurrenceLoad float64 // carried recurrence risk from recent injuries, 0..1
}

// Outcome is the resolved injury. Severity is 1..10 and DaysOut the expected
// recovery days BEFORE the seeded uncertainty spread is shown; the persisted
// expected_recovery_date uses DaysOut as-is (the ±spread is already folded in).
type Outcome struct {
	Type           Type
	Severity       int
	DaysOut        int
	RecurrenceRisk float64
	Explanation    *explanation.Explanation
}

// Recovery seconds buckets below (severityDays) are days returned for a fully-
// healed player; the type multiplier stretches or compresses the bucket.
//
//	severity: 1    2    3    4    5    6    7    8    9   10
//	days:       3    7   14   21   30   45   60   90  120  180
var severityDays = [11]int{0, 3, 7, 14, 21, 30, 45, 60, 90, 120, 180}

// typeDays is the per-type multiplier on the severity bucket (muscle = 1.0).
var typeDays = map[Type]float64{
	TypeMuscle:     1.0,
	TypeLigament:   1.5,
	TypeBone:       2.0,
	TypeConcussion: 0.5,
	TypeIllness:    0.4,
	TypeRecurring:  1.3,
}

// typeRecurrenceBase is the baseline recurrence risk per type.
var typeRecurrenceBase = map[Type]float64{
	TypeMuscle:     0.15,
	TypeLigament:   0.25,
	TypeBone:       0.12,
	TypeConcussion: 0.10,
	TypeIllness:    0.05,
	TypeRecurring:  0.60,
}

// Proposal constants (OPD-03 — recalibration is a data-only change).
const (
	RecurringMinLoad         = 0.60 // RecurrenceLoad at/above this forces the 'recurring' type
	SeverityDrawMin          = 2    // base severity draw is [2, 9]
	SeverityDrawSpan         = 8
	SeverityMin              = 1
	SeverityMax              = 10
	FatigueSeverityThreshold = 0.75  // effFatigue at/above this adds one severity step
	FatigueSeverityHigh      = 0.90  // at/above this adds a second step
	SusceptibilitySeverity   = 60    // susceptibility at/above this adds a severity step
	SusceptibilitySeverityHi = 80    // at/above this adds a second step
	RecurrenceSeverityStepA  = 0.30  // RecurrenceLoad at/above this adds a severity step
	RecurrenceSeverityStepB  = 0.60  // at/above this adds a further step (never double with the recurring-type flip)
	MedicalRecoveryStep      = 0.05  // per medical level above 1, fraction of recovery days removed
	MedicalMinFactor         = 0.55  // floor on the medical recovery factor
	UncertaintySpread        = 0.15  // ±15% seeded spread on recovery days
	RecurrenceSeverityShare  = 0.20  // severity's contribution to recurrence risk
	RecurrenceHistoryWeight  = 0.25  // prior injury history's contribution
	MedicalRecurrenceStep    = 0.035 // per medical level above 1, absolute recurrence reduction
	RushRecurrenceFloor      = 0.65  // rushed-return recurrence risk is at least this
	SetbackWindowStart       = 0.55  // setbacks considered from this elapsed fraction of the plan
	SetbackRate              = 0.35  // per-week probability a setback hits inside the window
	SetbackDaysMin           = 3
	SetbackDaysSpan          = 5     // setback days are [Min, Min+Span) = 3..7
	TrainingBaseChance       = 0.004 // weekly training-injury floor probability
	TrainingRiskChance       = 0.08  // per unit accumulated injury risk
	TrainingIntensityChance  = 0.05  // per unit plan intensity when risk > 0
	TrainingVolumeSaturation = 0.20  // plan volume in attr-points that reads as intensity 1.0
)

type typeWeight struct {
	t Type
	w int
}

// pickType draws the injury type from a context-weighted table. A strong
// recurrence history forces the 'recurring' type outright.
func pickType(rng *rand.Rand, in Input) Type {
	if in.RecurrenceLoad >= RecurringMinLoad {
		return TypeRecurring
	}
	weights := []typeWeight{
		{TypeMuscle, 38}, {TypeLigament, 24}, {TypeBone, 14},
		{TypeConcussion, 8}, {TypeIllness, 16},
	}
	if in.FoulContext {
		weights = []typeWeight{
			{TypeMuscle, 30}, {TypeLigament, 30}, {TypeBone, 20},
			{TypeConcussion, 12}, {TypeIllness, 8},
		}
	}
	total := 0
	for _, w := range weights {
		total += w.w
	}
	roll := int(rng.Float64() * float64(total))
	acc := 0
	for _, w := range weights {
		acc += w.w
		if roll < acc {
			return w.t
		}
	}
	return TypeMuscle
}

// Evaluate resolves one injury deterministically.
//
// Draw order (documented in docs/design/injury-numerics.md): one fresh
// math/rand source is seeded from Input.Seed; draws are type, severity, then
// recovery uncertainty; nothing else consumes RNG, so the same Input always
// replays the same Outcome.
func Evaluate(in Input) Outcome {
	if in.MedicalLevel < 1 {
		in.MedicalLevel = 1
	}
	if in.MedicalLevel > 10 {
		in.MedicalLevel = 10
	}
	rng := rand.New(rand.NewSource(in.Seed))

	t := pickType(rng, in)

	effFatigue := clamp01(in.Fatigue + 0.001*float64(in.Minutes))
	sev := SeverityDrawMin + int(rng.Float64()*SeverityDrawSpan)
	if effFatigue >= FatigueSeverityHigh {
		sev++
	}
	if effFatigue >= FatigueSeverityThreshold {
		sev++
	}
	if in.Susceptibility >= SusceptibilitySeverityHi {
		sev++
	}
	if in.Susceptibility >= SusceptibilitySeverity {
		sev++
	}
	if in.RecurrenceLoad >= RecurrenceSeverityStepB {
		sev++
	}
	if in.RecurrenceLoad >= RecurrenceSeverityStepA {
		sev++
	}
	if in.FoulContext {
		sev++
	}
	sev = clampInt(sev, SeverityMin, SeverityMax)

	base := float64(severityDays[sev]) * typeDays[t]

	med := 1.0 - MedicalRecoveryStep*float64(in.MedicalLevel-1)
	if med < MedicalMinFactor {
		med = MedicalMinFactor
	}
	base *= med

	spread := 1.0 + UncertaintySpread*(2*rng.Float64()-1)
	days := int(math.Round(base * spread))
	if days < 1 {
		days = 1
	}

	rec := typeRecurrenceBase[t]
	rec += RecurrenceSeverityShare * float64(sev) / float64(SeverityMax)
	rec += RecurrenceHistoryWeight * clamp01(in.RecurrenceLoad)
	rec -= MedicalRecurrenceStep * float64(in.MedicalLevel-1)
	rec = clamp01(rec)

	fatigueSteps := 0
	if effFatigue >= FatigueSeverityHigh {
		fatigueSteps++
	}
	if effFatigue >= FatigueSeverityThreshold {
		fatigueSteps++
	}
	suceptSteps := 0
	if in.Susceptibility >= SusceptibilitySeverityHi {
		suceptSteps++
	}
	if in.Susceptibility >= SusceptibilitySeverity {
		suceptSteps++
	}
	recDays := 0.0
	if in.RecurrenceLoad >= RecurrenceSeverityStepA {
		recDays = float64(severityDays[sev]) * RecurrenceHistoryWeight * 0.5
	}

	exp := explanation.New("player_injury", days).
		Add(string(t)+" injury", severityDays[sev])
	if in.FoulContext {
		exp = exp.Add("physical contact", 1)
	}
	if fatigueSteps > 0 {
		exp = exp.Add("fatigue", fatigueSteps)
	}
	if suceptSteps > 0 {
		exp = exp.Add("injury susceptibility", suceptSteps)
	}
	if recDays > 0 {
		exp = exp.Add("recurrence history", int(recDays))
	}
	if in.MedicalLevel > 1 {
		exp = exp.Add("medical staff", -int(math.Round(base*(1-med))))
	}

	return Outcome{
		Type:           t,
		Severity:       sev,
		DaysOut:        days,
		RecurrenceRisk: rec,
		Explanation:    exp,
	}
}

// TrainingChance is the weekly probability a training session injures a player
// given the accumulated injury risk and the plan's workload intensity (0..1).
// Defense in depth: the floor keeps even pristine squads very unlikely to roll,
// risk scales the curve, and intensity multiplies the risk term.
func TrainingChance(injuryRisk, intensity float64) float64 {
	return clamp01(TrainingBaseChance + TrainingRiskChance*clamp01(injuryRisk) + TrainingIntensityChance*clamp01(intensity)*clamp01(injuryRisk))
}

// TrainingHit draws one (week, player)-scoped training-injury presence check
// from the given stream value (callers seed with WeekStream(tick, pid,
// "training")). Deterministic: redelivery of the same weekly tick replays the
// same hit/no-hit.
func TrainingHit(seed uint64, injuryRisk, intensity float64) bool {
	rng := rand.New(rand.NewSource(int64(seed)))
	return rng.Float64() < TrainingChance(injuryRisk, intensity)
}

// RushedRecurrence is the recurrence risk stamped on an injury whose manager
// rushes the player back before the expected return (high by the S08-03 AC).
func RushedRecurrence(recurrence float64) float64 {
	r := clamp01(recurrence)
	if r < RushRecurrenceFloor {
		r = RushRecurrenceFloor
	}
	return r
}

// Setback decides whether an open injury suffers a setback during one weekly
// recovery window. Deterministic per (injury stream key, week) — the caller
// seeds it with SetbackStream (internal/injury/stream.go). returns days (0 = no
// setback; otherwise the extra recovery days this week).
func Setback(seed uint64) int {
	rng := rand.New(rand.NewSource(int64(seed)))
	if rng.Float64() >= SetbackRate {
		return 0
	}
	return SetbackDaysMin + int(rng.Float64()*SetbackDaysSpan)
}

func clamp01(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return math.Max(0, math.Min(1, f))
}

func clampInt(v, lo, hi int) int {
	return int(math.Max(float64(lo), math.Min(float64(hi), float64(v))))
}
