package scout

import (
	"testing"

	"github.com/touchline/backend/pkg/matchsim"
)

func f(v float64) *float64 { return &v }

func TestRecommendLeakyLowBlock(t *testing.T) {
	adv := recommend("Brennock", OpponentProfile{Style: matchsim.StyleLowBlock, Matches: 5,
		GoalsFor: 1, GoalsAgainst: 2, XGFor: f(0.9), XGAgainst: f(1.8)})
	if len(adv) != 2 || adv[0].dial != "mentality" || adv[0].value != 1 || adv[1].dial != "width" || adv[1].value != 1 {
		t.Fatalf("want positive mentality + wide, got %+v", adv)
	}
	if adv[0].weight != 9 || adv[0].reason != "Brennock concede 1.8 xG a game and create 0.9" {
		t.Fatalf("evidence should use xG: %+v", adv[0])
	}
}

func TestRecommendFallsBackToGoalsWithoutXG(t *testing.T) {
	adv := recommend("X", OpponentProfile{Style: matchsim.StyleBalanced, Matches: 3, GoalsFor: 2.4, GoalsAgainst: 0.6})
	if len(adv) != 1 || adv[0].value != -1 || adv[0].reason != "X create 2.4 goals a game and concede 0.6" {
		t.Fatalf("want cautious from goals, got %+v", adv)
	}
}

func TestRecommendNoEvidence(t *testing.T) {
	adv := recommend("X", OpponentProfile{Style: matchsim.StyleBalanced})
	if len(adv) != 0 {
		t.Fatalf("no matches + balanced style should give no advice: %+v", adv)
	}
	h, summary := headlineFor("X", adv)
	if h != "No clear edge against X yet" || summary == "" {
		t.Fatalf("headline: %q / %q", h, summary)
	}
}

func TestFitRewardsFollowingAdvice(t *testing.T) {
	adv := recommend("X", OpponentProfile{Style: matchsim.StyleGegenpress})
	follow := fitOf(matchsim.Instructions{Tempo: 1, Pressing: -1}, adv)
	ignore := fitOf(matchsim.Instructions{Tempo: -1, Pressing: 1}, adv)
	if follow.Score != 70 || ignore.Score != 30 {
		t.Fatalf("scores: follow %d ignore %d", follow.Score, ignore.Score)
	}
	if follow.Factors[0].Delta != 6 || ignore.Factors[0].Delta != -6 {
		t.Fatalf("factors: %+v / %+v", follow.Factors, ignore.Factors)
	}
}
