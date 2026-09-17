package squad

import "testing"

func TestSentimentFromStates(t *testing.T) {
	cases := []struct {
		name   string
		states []EmotionalStateRef
		want   int
	}{
		{"no states", nil, 0},
		{"single happy", []EmotionalStateRef{{"happy", 100}}, 70},
		{"single angry", []EmotionalStateRef{{"angry", 100}}, -80},
		{"happy newest halves angry", []EmotionalStateRef{{"happy", 100}, {"angry", 100}}, 20},
		{"angry newest halves happy", []EmotionalStateRef{{"angry", 100}, {"happy", 100}}, -30},
		{"unknown state reads neutral-positive", []EmotionalStateRef{{"not_a_state", 100}}, 15},
		{"intensity clamps", []EmotionalStateRef{{"happy", 200}}, 100},
	}
	for _, c := range cases {
		if got := SentimentFromStates(c.states); got != c.want {
			t.Errorf("%s: SentimentFromStates(%v) = %d, want %d", c.name, c.states, got, c.want)
		}
	}
}

func TestSentimentFromStatesRecencyOrder(t *testing.T) {
	// The newest state must be the strongest voice: same pair, flipped
	// order, flipped sign.
	pairA := SentimentFromStates([]EmotionalStateRef{{"happy", 100}, {"angry", 100}})
	pairB := SentimentFromStates([]EmotionalStateRef{{"angry", 100}, {"happy", 100}})
	if pairA <= 0 {
		t.Fatalf("happy-newest must read positive, got %d", pairA)
	}
	if pairB >= 0 {
		t.Fatalf("angry-newest must read negative, got %d", pairB)
	}
}

func TestMatchEligibleA07(t *testing.T) {
	cases := []struct {
		name        string
		status      string
		origin      string
		hasContract bool
		open        bool
		age         int
		want        bool
	}{
		{"street 17 with contract", "active", "street", true, false, 17, false},
		{"street 18 with contract", "active", "street", true, false, 18, true},
		{"street 21 with contract", "active", "street", true, false, 21, true},
		{"academy 16 with contract", "active", "club_academy", true, false, 16, true},
		{"generated 16 with contract", "active", "generated", true, false, 16, true},
		{"no contract", "active", "generated", false, false, 23, false},
		{"retired street 19", "retired", "street", true, false, 19, false},
		{"injured with contract", "injured", "generated", true, false, 23, false},
		{"on loan", "on_loan", "generated", true, false, 23, false},
		{"open injury", "active", "generated", true, true, 23, false},
	}
	for _, c := range cases {
		got := MatchEligible(c.status, c.origin, c.hasContract, c.open, c.age)
		if got != c.want {
			t.Errorf("%s: MatchEligible(%q, %q, %v, %v, %d) = %v, want %v",
				c.name, c.status, c.origin, c.hasContract, c.open, c.age, got, c.want)
		}
	}
}
