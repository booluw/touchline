package injury

import (
	"testing"

	"github.com/google/uuid"
)

func TestEvaluateDeterminism(t *testing.T) {
	in := Input{Seed: 42, Fatigue: 0.6, Susceptibility: 50, MedicalLevel: 5, Minutes: 90, FoulContext: true}
	a := Evaluate(in)
	for i := 0; i < 20; i++ {
		b := Evaluate(in)
		if a.Type != b.Type || a.Severity != b.Severity || a.DaysOut != b.DaysOut ||
			a.RecurrenceRisk != b.RecurrenceRisk {
			t.Fatalf("Evaluate not deterministic: %+v vs %+v", a, b)
		}
	}
}

func TestEvaluateBounds(t *testing.T) {
	seeds := []int64{1, 7, 99, 1234, 5}
	for _, seed := range seeds {
		for _, f := range []float64{0, 0.3, 0.8, 1} {
			o := Evaluate(Input{Seed: seed, Fatigue: f, Susceptibility: 30, MedicalLevel: 3, Minutes: 90})
			if o.Severity < SeverityMin || o.Severity > SeverityMax {
				t.Fatalf("severity %d out of range", o.Severity)
			}
			if o.DaysOut < 1 {
				t.Fatalf("days %d < 1", o.DaysOut)
			}
			if o.RecurrenceRisk < 0 || o.RecurrenceRisk > 1 {
				t.Fatalf("recurrence %v out of range", o.RecurrenceRisk)
			}
			if o.Explanation == nil || len(o.Explanation.Factors) == 0 {
				t.Fatalf("expected a non-empty explanation, got %+v", o.Explanation)
			}
		}
	}
}

func TestEvaluateFatigueMonotonic(t *testing.T) {
	// Same seed keeps type/severity-base/uncertainty draws identical; the
	// severity steps from effFatigue make both severity and days non-decreasing.
	for _, seed := range []int64{3, 11, 201} {
		prevSeverity, prevDays := 0, 0
		for _, f := range []float64{0, 0.4, 0.7, 0.8, 0.95, 1} {
			o := Evaluate(Input{Seed: seed, Fatigue: f, Susceptibility: 50, MedicalLevel: 5, Minutes: 90})
			if o.Severity < prevSeverity || o.DaysOut < prevDays {
				t.Fatalf("seed %d fatigue %v: severity/days not monotonic (%d/%d before %d/%d)",
					seed, f, prevSeverity, prevDays, o.Severity, o.DaysOut)
			}
			prevSeverity, prevDays = o.Severity, o.DaysOut
		}
	}
}

func TestEvaluateMedicalMonotonic(t *testing.T) {
	for _, seed := range []int64{4, 22, 301} {
		prevDays, prevRec := 1<<30, +1.0
		for m := 1; m <= 10; m++ {
			o := Evaluate(Input{Seed: seed, Fatigue: 0.7, Susceptibility: 50, MedicalLevel: m, Minutes: 90, FoulContext: true})
			// round() is monotonic, so days are non-increasing in medical level.
			if m > 1 && o.DaysOut > prevDays {
				t.Fatalf("seed %d med %d: days %d > %d from weaker medical", seed, m, o.DaysOut, prevDays)
			}
			if o.RecurrenceRisk > prevRec+1e-9 {
				t.Fatalf("seed %d med %d: recurrence %v > %v from weaker medical", seed, m, o.RecurrenceRisk, prevRec)
			}
			prevDays, prevRec = o.DaysOut, o.RecurrenceRisk
		}
	}
}

func TestEvaluateRecurrenceLoad(t *testing.T) {
	prevSeverity, prevRec := 0, -1.0
	for _, load := range []float64{0, 0.2, 0.5, 0.7, 1} {
		o := Evaluate(Input{Seed: 12, Fatigue: 0.5, Susceptibility: 50, MedicalLevel: 5, Minutes: 90, FoulContext: false, RecurrenceLoad: load})
		if o.Severity < prevSeverity || o.RecurrenceRisk < prevRec {
			t.Fatalf("load %v: severity/recurrence not monotonic (before %d/%v)", load, prevSeverity, prevRec)
		}
		prevSeverity, prevRec = o.Severity, o.RecurrenceRisk
	}
}

func TestEvaluateRecurringForced(t *testing.T) {
	for _, seed := range []int64{1, 2, 3, 4, 5} {
		o := Evaluate(Input{Seed: seed, Fatigue: 0.2, Susceptibility: 20, MedicalLevel: 5, Minutes: 0, RecurrenceLoad: 0.9})
		if o.Type != TypeRecurring {
			t.Fatalf("seed %d: high recurrence load did not force recurring (got %s)", seed, o.Type)
		}
	}
}

func TestRushedRecurrence(t *testing.T) {
	for _, r := range []float64{0, 0.1, 0.4, 0.8, 1} {
		if got := RushedRecurrence(r); got < RushRecurrenceFloor {
			t.Fatalf("RushedRecurrence(%v) = %v below floor %v", r, got, RushRecurrenceFloor)
		}
	}
}

func TestSetback(t *testing.T) {
	// Determinism per stream key.
	a := Setback(SetbackStream(uuid.MustParse("11111111-1111-1111-1111-111111111111"), 5))
	b := Setback(SetbackStream(uuid.MustParse("11111111-1111-1111-1111-111111111111"), 5))
	if a != b {
		t.Fatalf("Setback not deterministic: %d vs %d", a, b)
	}
	if a != 0 && (a < SetbackDaysMin || a >= SetbackDaysMin+SetbackDaysSpan) {
		t.Fatalf("setback days %d outside 3..7", a)
	}
	// Rate sanity across many keys: most weeks no setback.
	hits := 0
	const n = 10000
	for i := 0; i < n; i++ {
		id := uuid.New()
		if Setback(SetbackStream(id, 1)) != 0 {
			hits++
		}
	}
	rate := float64(hits) / n
	if rate < 0.2 || rate > 0.5 {
		t.Fatalf("setback rate %.3f too far from %.2f", rate, SetbackRate)
	}
}

func TestMatchStreamDistinct(t *testing.T) {
	a := MatchStream(7, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	b := MatchStream(8, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	c := MatchStream(7, uuid.MustParse("22222222-2222-2222-2222-222222222222"))
	if a == b || a == c || b == c {
		t.Fatalf("MatchStream keys alias: %d %d %d", a, b, c)
	}
}

func TestTrainingHitDeterminismAndMonotonicChance(t *testing.T) {
	pid := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	week := int64(40)
	seed := WeekStream(week, pid, "training")
	a := TrainingHit(seed, 0.5, 0.5)
	b := TrainingHit(seed, 0.5, 0.5)
	if a != b {
		t.Fatalf("training hit not deterministic for same stream")
	}
	if TrainingChance(0.0, 0.0) <= 0 {
		t.Fatalf("floor chance must be positive, got %v", TrainingChance(0, 0))
	}
	if c := TrainingChance(1.0, 1.0); c <= TrainingChance(0.5, 0.5) {
		t.Fatalf("chance must increase with risk/intensity (%.3f <= %.3f)", c, TrainingChance(0.5, 0.5))
	}
	hits := 0
	const n = 20000
	for i := int64(0); i < n; i++ {
		if TrainingHit(WeekStream(i, pid, "training"), 1.0, 1.0) {
			hits++
		}
	}
	rate := float64(hits) / n
	if rate < 0.08 || rate > 0.18 {
		t.Fatalf("high-risk hit rate %.3f outside [8%%, 18%%]", rate)
	}
}
