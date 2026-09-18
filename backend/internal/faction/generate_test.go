package faction

import (
	"reflect"
	"testing"
)

func profiles() []MemberProfile {
	return []MemberProfile{
		{PlayerID: "p1", Name: "One", Nationality: "ENG", Age: 31, Position: "MID", Leadership: 80, Sociability: 70, Loyalty: 70, Volatility: 40},
		{PlayerID: "p2", Name: "Two", Nationality: "ENG", Age: 22, Position: "MID", Leadership: 40, Sociability: 55, Loyalty: 50, Volatility: 60},
		{PlayerID: "p3", Name: "Three", Nationality: "ESP", Age: 29, Position: "DEF", AcademyProduct: true, Leadership: 55, Sociability: 45, Loyalty: 60, Volatility: 50},
		{PlayerID: "p4", Name: "Four", Nationality: "ESP", Age: 19, Position: "FWD", AcademyProduct: true, Leadership: 30, Sociability: 80, Loyalty: 45, Volatility: 70},
	}
}

// TestGenerateEdgesDeterministic: same seed + squad always yields the same
// edges, in the same order, independent of the input ordering.
func TestGenerateEdgesDeterministic(t *testing.T) {
	a := GenerateEdges(42, profiles())
	b := GenerateEdges(42, profiles())
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("generation must be deterministic for identical inputs")
	}

	shuffled := profiles()
	shuffled[0], shuffled[3] = shuffled[3], shuffled[0]
	shuffled[1], shuffled[2] = shuffled[2], shuffled[1]
	c := GenerateEdges(42, shuffled)
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("generation must not depend on input ordering")
	}
}

// TestGenerateEdgesDifferentSeed: a different world seed re-rolls the affinity
// edges (the fact-based national_team/academy bonds are seed-independent).
func TestGenerateEdgesDifferentSeed(t *testing.T) {
	a := GenerateEdges(1, profiles())
	b := GenerateEdges(2, profiles())
	if reflect.DeepEqual(a, b) {
		t.Fatalf("different world seeds should change at least one affinity edge")
	}
}

// TestGenerateEdgesFactBasedKinds: shared nationality, academy products and
// mentorship all produce their documented edge kinds.
func TestGenerateEdgesFactBasedKinds(t *testing.T) {
	edges := GenerateEdges(7, profiles())
	kinds := map[string]int{}
	for _, e := range edges {
		kinds[e.Kind]++
		if e.From >= e.To {
			t.Fatalf("edges must use canonical orientation (from < to), got %s→%s", e.From, e.To)
		}
		if e.Strength < -100 || e.Strength > 100 || e.Trust < -100 || e.Trust > 100 {
			t.Fatalf("edge %+v out of [-100,100]", e)
		}
	}
	if kinds[KindNationalTeam] < 1 {
		t.Fatalf("expected a national_team edge, got %v", kinds)
	}
	if kinds[KindAcademy] < 1 {
		t.Fatalf("expected an academy edge, got %v", kinds)
	}
	if kinds[KindMentorship] < 1 {
		t.Fatalf("expected a mentorship edge, got %v", kinds)
	}
}

// TestPairAffinityStableAcrossSquad: a pair's affinity depends only on the pair,
// so adding a team-mate never re-rolls an existing bond.
func TestPairAffinityStableAcrossSquad(t *testing.T) {
	base := pairAffinity(11, profiles()[0], profiles()[1])
	withExtra := append(profiles(), MemberProfile{PlayerID: "p9", Sociability: 90})
	_ = GenerateEdges(11, withExtra)
	if again := pairAffinity(11, withExtra[0], withExtra[1]); again != base {
		t.Fatalf("pair affinity drifted when the squad changed: %d → %d", base, again)
	}
}

// TestFormerTeammateEdges: sale bonds are canonical, exclude self, and are
// modestly positive.
func TestFormerTeammateEdges(t *testing.T) {
	edges := FormerTeammateEdges("p2", []string{"p1", "p2", "p3"})
	if len(edges) != 2 {
		t.Fatalf("expected 2 former-teammate edges (self excluded), got %d", len(edges))
	}
	for _, e := range edges {
		if e.Kind != KindFormerTeammate {
			t.Fatalf("wrong kind: %s", e.Kind)
		}
		if e.From >= e.To {
			t.Fatalf("non-canonical orientation: %s→%s", e.From, e.To)
		}
		if e.Strength <= 0 || e.Trust <= 0 {
			t.Fatalf("former-teammate bond should be positive, got %+v", e)
		}
	}
}
