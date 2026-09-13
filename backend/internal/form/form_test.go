package form

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func epsilon(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestNeutralZeroState(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	f := Neutral(id, 7)
	if f.CurrentRating != 1.0 || f.LastUpdatedTick != 7 || f.FormString != "" {
		t.Fatalf("neutral state wrong: %+v", f)
	}
}

func TestUpdateEWMA(t *testing.T) {
	prev := Neutral(uuid.New(), 0)
	// Neutral-quality results leave the rating at exactly 1.0.
	for i := 0; i < 2; i++ {
		prev = Update(prev, 1.0, AlphaDefault, int64(i+1))
	}
	if !epsilon(prev.CurrentRating, 1.0) {
		t.Fatalf("neutral results drifted away from 1.0: %v", prev.CurrentRating)
	}

	// A long hot streak rides the rating up to the band ceiling.
	prev = Neutral(uuid.New(), 0)
	for i := 0; i < 10; i++ {
		prev = Update(prev, 1.25, AlphaDefault, int64(i+1))
	}
	if !epsilon(prev.CurrentRating, MaxRating) {
		t.Fatalf("a hot streak must ride the band to %v, got %v", MaxRating, prev.CurrentRating)
	}

	// An even longer slump sinks it to the floor.
	for i := 0; i < 20; i++ {
		prev = Update(prev, 0.75, AlphaDefault, int64(i+1))
	}
	if !epsilon(prev.CurrentRating, MinRating) {
		t.Fatalf("a long slump must sink to %v, got %v", MinRating, prev.CurrentRating)
	}
}

func TestUpdateDecaysToNeutral(t *testing.T) {
	hot := Neutral(uuid.New(), 0)
	for i := 0; i < 10; i++ {
		hot = Update(hot, 1.25, AlphaDefault, int64(i+1))
	}
	if !epsilon(hot.CurrentRating, MaxRating) {
		t.Fatalf("expected hot climb to the ceiling, got %v", hot.CurrentRating)
	}
	// Form always mean-reverts: several as-expected results pull it back
	// toward 1.0, converging asymptotically (EWMA, geometric in alpha).
	for i := 0; i < 200; i++ {
		hot = Update(hot, 1.0, AlphaDefault, int64(i+1))
	}
	if !epsilon(hot.CurrentRating, 1.0) {
		t.Fatalf("neutral play must decay toward 1.0, got %v", hot.CurrentRating)
	}
}

func TestUpdateClampsBand(t *testing.T) {
	prev := Neutral(uuid.New(), 0)
	prev = Update(prev, 2.0, AlphaDefault, 99)
	prev = Update(prev, 2.0, AlphaDefault, 100)
	if !epsilon(prev.CurrentRating, MaxRating) {
		t.Fatalf("absurd quality must clamp to %v, got %v", MaxRating, prev.CurrentRating)
	}
	prev = Update(prev, -1.0, AlphaDefault, 101)
	prev = Update(prev, -1.0, AlphaDefault, 102)
	if !epsilon(prev.CurrentRating, MinRating) {
		t.Fatalf("rating cannot fall below %v: got %v", MinRating, prev.CurrentRating)
	}
}

func TestUpdateBadAlphaFallsBack(t *testing.T) {
	prev := Neutral(uuid.New(), 0)
	next := Update(prev, 1.2, 0, 5)
	want := Update(prev, 1.2, AlphaDefault, 5)
	if !epsilon(next.CurrentRating, want.CurrentRating) {
		t.Fatalf("alpha<=0 must fall back to AlphaDefault: %v vs %v", next.CurrentRating, want.CurrentRating)
	}
}

func TestUpdateStampsTick(t *testing.T) {
	prev := Neutral(uuid.New(), 10)
	next := Update(prev, 1.0, AlphaDefault, 77)
	if next.LastUpdatedTick != 77 {
		t.Fatalf("tick not stamped: %v", next.LastUpdatedTick)
	}
}

func TestResultQualityScale(t *testing.T) {
	if q := ResultQuality(0, 0); !epsilon(q, 1.0) {
		t.Fatalf("as-expected result must be neutral, got %v", q)
	}
	if q := ResultQuality(2, 0); !epsilon(q, 1.5) {
		t.Fatalf("+2 GD over expectation must be 1.5, got %v", q)
	}
	if q := ResultQuality(0, 2); !epsilon(q, 0.5) {
		t.Fatalf("-2 GD under expectation must be 0.5, got %v", q)
	}
	// Outside the clamp window stays bounded.
	if q := ResultQuality(20, 0); !epsilon(q, 1.5) {
		t.Fatalf("huge upset quality must be capped at 1.5, got %v", q)
	}
	if q := ResultQuality(0, 20); !epsilon(q, 0.5) {
		t.Fatalf("huge disappointing quality must floor at 0.5, got %v", q)
	}
}

func TestResultQualityDeltaSubInteger(t *testing.T) {
	// The continuous twin must preserve sub-integer signals integer rounding
	// would destroy (internal/match's engine-mirrored expectation is a float).
	if q := ResultQualityDelta(0); !epsilon(q, 1.0) {
		t.Fatalf("zero delta must be neutral, got %v", q)
	}
	if q := ResultQualityDelta(0.5); !epsilon(q, 1.125) {
		t.Fatalf("+0.5 delta must be 1.125, got %v", q)
	}
	if q := ResultQualityDelta(-0.5); !epsilon(q, 0.875) {
		t.Fatalf("-0.5 delta must be 0.875, got %v", q)
	}
	// Integer outcomes still agree with the discrete form.
	if q := ResultQualityDelta(2); !epsilon(q, ResultQuality(2, 0)) {
		t.Fatalf("+2 delta must match the discrete form: %v vs %v", q, ResultQuality(2, 0))
	}
	if q := ResultQualityDelta(20); !epsilon(q, ResultQuality(20, 0)) {
		t.Fatalf("large delta must clamp exactly like the discrete form: %v", q)
	}
}

func TestFormStringFromResults(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{ResultWin, ResultDraw, ResultLoss}, "W-D-L"},
		{[]string{ResultLoss, ResultLoss, ResultWin, ResultWin, ResultWin}, "L-L-W-W-W"},
		// More than five results keep the trailing five, oldest first.
		{[]string{"W", "W", "W", "D", "W", "L", "W"}, "W-D-W-L-W"},
		// Unknown symbols render as the nil placeholder (kept as a slot).
		{[]string{"bogus", ResultWin}, "--W"},
	}
	for _, c := range cases {
		if got := FormStringFromResults(c.in); got != c.want {
			t.Fatalf("FormStringFromResults(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAppendResultWindow(t *testing.T) {
	window := []string{}
	for _, r := range []string{ResultWin, ResultWin, ResultDraw, ResultLoss, ResultWin, ResultLoss} {
		window = AppendResult(window, r)
	}
	if len(window) != 5 || window[0] != ResultWin || window[4] != ResultLoss {
		t.Fatalf("trailing-5 window wrong: %v", window)
	}
	if got := FormStringFromResults(window); got != "W-D-L-W-L" {
		t.Fatalf("rendered window wrong: %q", got)
	}
}