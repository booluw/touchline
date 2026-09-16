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
