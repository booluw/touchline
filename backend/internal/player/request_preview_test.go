package player

import "testing"

// Factors must sum to the score, or the "Why?" panel's net lies (IM64).
func TestMoraleTargetExplanationSums(t *testing.T) {
	people := []PlayerPersonality{{}, {Patience: 100, Loyalty: 100}, {Ambition: 100, Ego: 100}, {Patience: 37, Loyalty: 81, Ambition: 13, Ego: 66}}
	for _, p := range people {
		for _, c := range [][2]float64{{0.8, 0.75}, {0.4, 0.75}, {0.1, 0.75}, {0, 0}} {
			e := moraleTargetExplanation(c[0], c[1], p)
			if err := e.Validate(); err != nil {
				t.Fatalf("share %v expected %v %+v: %v", c[0], c[1], p, err)
			}
			if want := int(moraleTarget(c[0], c[1], p)*100+0.5) - 50; e.Score != want {
				t.Fatalf("score %d, want %d", e.Score, want)
			}
		}
	}
}

// The deny preview must not claim a bigger drop than morale allows, and the
// prices follow the presets the approve handler accepts (IM65).
func TestBuildRequestPreview(t *testing.T) {
	p := buildRequestPreview(0.04, 1_000_000)
	if got := p.Prices[0].AskingPrice; got != 800_000 {
		t.Fatalf("quick sale = %d", got)
	}
	if got := p.Prices[2].AskingPrice; got != 1_250_000 {
		t.Fatalf("hold out = %d", got)
	}
	for _, pr := range p.Prices {
		if AskingPricePresets[pr.Preset] != pr.Multiplier {
			t.Fatalf("preset %s mismatch", pr.Preset)
		}
	}
	deny := p.Options[2]
	if deny.Action != "deny" || deny.Effects[0].Delta != -4 || deny.Effects[1].Delta != SentimentTransferDenied {
		t.Fatalf("deny effects = %+v", deny.Effects)
	}
}
