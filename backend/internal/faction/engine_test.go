package faction

import (
	"testing"
)

// room is a deterministic two-faction snapshot: a veteran core (v1..v4) and a
// foreign cohort (f1..f3) with dense internal bonds and a single bridge. Every
// test in this file is DB-free — the engine never touches a database, the wall
// clock or RNG, exactly the S09-01 discipline carried into the dressing room.
func room() SquadSnapshot {
	members := []SquadMember{
		{PlayerID: "v1", Name: "Vet One", Leadership: 92, Loyalty: 80, Volatility: 30},
		{PlayerID: "v2", Name: "Vet Two", Leadership: 85, Loyalty: 78, Volatility: 25},
		{PlayerID: "v3", Name: "Vet Three", Leadership: 76, Loyalty: 82, Volatility: 28},
		{PlayerID: "v4", Name: "Vet Four", Leadership: 70, Loyalty: 75, Volatility: 33},
		{PlayerID: "f1", Name: "Foreign One", Leadership: 74, Loyalty: 35, Volatility: 82},
		{PlayerID: "f2", Name: "Foreign Two", Leadership: 66, Loyalty: 30, Volatility: 90},
		{PlayerID: "f3", Name: "Foreign Three", Leadership: 62, Loyalty: 28, Volatility: 77},
	}
	edges := []RelationshipEdge{
		{From: "v1", To: "v2", Strength: 60, Trust: 40, Kind: KindFriendship},
		{From: "v1", To: "v3", Strength: 55, Trust: 35, Kind: KindFriendship},
		{From: "v2", To: "v3", Strength: 50, Trust: 30, Kind: KindFriendship},
		{From: "v3", To: "v4", Strength: 45, Trust: 25, Kind: KindMentorship},
		{From: "f1", To: "f2", Strength: 52, Trust: 20, Kind: KindNationalTeam},
		{From: "f1", To: "f3", Strength: 48, Trust: 18, Kind: KindNationalTeam},
		{From: "f2", To: "f3", Strength: 44, Trust: 16, Kind: KindNationalTeam},
		{From: "v1", To: "f1", Strength: 30, Trust: 15, Kind: KindFriendship},
	}
	roots := map[string]string{
		"v1": "f1", "v2": "f1", "v3": "f1", "v4": "f1",
		"f1": "f1", "f2": "f1", "f3": "f1",
	}
	return SquadSnapshot{Members: members, Edges: edges, Roots: roots}
}

// TestEvaluateCoversEveryAction: the engine resolves EVERY management action
// deterministically and every outcome carries an auditable Explanation —
// mirrors S09-01's full-matrix discipline.
func TestEvaluateCoversEveryAction(t *testing.T) {
	e := NewEngine()
	snap := room()
	for _, a := range []Action{
		ActionInspect,
		ActionMissedPromise,
		ActionDroppedFromStartingXI,
		ActionBenchedLongTerm,
		ActionWageCutOffered,
		ActionContractApproved,
		ActionTransferBidQuery,
		ActionTransferBidBlocked,
		ActionReleasedFromSquad,
		ActionTrainingLoadIncreased,
		ActionTeamTalkMotivational,
		ActionTeamTalkCritical,
	} {
		a := a
		t.Run(string(a), func(t *testing.T) {
			res := e.Evaluate(a, snap)
			if res == nil {
				t.Fatalf("action %q: nil result", a)
			}
			if res.Explanation == nil || res.Explanation.Subject == "" {
				t.Fatalf("action %q: every outcome must carry an auditable Explanation", a)
			}
			again := e.Evaluate(a, snap)
			if !sameTiers(res.Tiers, again.Tiers) || !sameContagion(res.Contagion, again.Contagion) {
				t.Fatalf("action %q: engine must be deterministic (pure function of (action, snapshot))", a)
			}
		})
	}
}

// TestReleasedFromSquadBoundedContagion: selling a player starts a gradual,
// bounded (<100), explained contagion that at least reaches its epicentre.
func TestReleasedFromSquadBoundedContagion(t *testing.T) {
	e := NewEngine()
	c := e.Contagion("v1", room())
	if c == nil {
		t.Fatalf("a squad release must produce a contagion result")
	}
	if c.Confidence >= 100 {
		t.Fatalf("contagion confidence must be strictly < 100, got %d", c.Confidence)
	}
	if c.Confidence <= 0 {
		t.Fatalf("contagion must start > 0 and harden gradually, got %d", c.Confidence)
	}
	if c.Explanation == nil || len(c.Explanation.Factors) == 0 {
		t.Fatalf("contagion must carry an auditable Explanation")
	}
	if len(c.Affected) == 0 || c.Affected[0] != "v1" {
		t.Fatalf("contagion must reach its epicentre first, got %v", c.Affected)
	}
}

// TestContagionStartsWellBelowCapAndHardensGradually: confidence starts at the
// documented floor, climbs in bounded steps and is capped below 100 no matter
// how many hops.
func TestContagionStartsWellBelowCapAndHardensGradually(t *testing.T) {
	e := NewEngine()
	c := e.Contagion("v1", room())
	if c.Confidence < ContagionStart {
		t.Fatalf("confidence must never start below ContagionStart=%d, got %d", ContagionStart, c.Confidence)
	}
	if c.Confidence > ContagionCap {
		t.Fatalf("confidence must be capped at %d, got %d", ContagionCap, c.Confidence)
	}
	if c.Confidence > ContagionStart+ContagionHopBump*len(c.Affected) {
		t.Fatalf("confidence grew faster than the bounded per-hop rule")
	}
}

// TestUnrestOnlyAfterThreshold: a two-man snapshot stays under the unrest line;
// a dense room breaches it. Unrest is bounded <100, names a concrete demand and
// carries an explanation.
func TestUnrestOnlyAfterThreshold(t *testing.T) {
	e := NewEngine()

	quiet := SquadSnapshot{
		Members: []SquadMember{
			{PlayerID: "a", Leadership: 50, Loyalty: 50, Volatility: 50},
			{PlayerID: "b", Leadership: 40, Loyalty: 40, Volatility: 40},
		},
		Edges: []RelationshipEdge{{From: "a", To: "b", Strength: 20, Trust: 10, Kind: KindFriendship}},
	}
	if u := e.UnrestFrom(e.Contagion("a", quiet)); u != nil {
		t.Fatalf("a quiet two-man room must stay below the unrest threshold, got severity %d", u.Severity)
	}

	u := e.UnrestFrom(e.Contagion("v1", room()))
	if u == nil {
		t.Fatalf("a severe contagion must escalate to documented Unrest")
	}
	if u.Severity >= 100 {
		t.Fatalf("unrest severity must stay strictly < 100, got %d", u.Severity)
	}
	if u.Demand == "" {
		t.Fatalf("unrest must carry a specific demand")
	}
	if u.Explanation == nil {
		t.Fatalf("unrest must carry an auditable Explanation")
	}
}

// TestEnMasseDemandAtTopSeverity: a contagion at the cap escalates to en-masse
// transfer requests rather than a board meeting.
func TestEnMasseDemandAtTopSeverity(t *testing.T) {
	e := NewEngine()
	u := e.UnrestFrom(e.Contagion("v1", room()))
	if u == nil {
		t.Fatalf("expected unrest")
	}
	if u.Severity >= EnMasseThreshold && u.Demand != DemandEnMasseTransferReqs {
		t.Fatalf("severity %d must demand en-masse transfer requests, got %q", u.Severity, u.Demand)
	}
	if u.Severity < EnMasseThreshold && u.Demand != DemandBoardMeeting {
		t.Fatalf("severity %d must demand a board meeting, got %q", u.Severity, u.Demand)
	}
}

// TestFactionsFromRootsAndFallback: CTE roots and union-find agree, and faction
// labels/leaders/cohesion are deterministic and bounded.
func TestFactionsFromRootsAndFallback(t *testing.T) {
	e := NewEngine()

	withRoots := room()
	withoutRoots := room()
	withoutRoots.Roots = nil

	first := e.Evaluate(ActionTeamTalkMotivational, withRoots)
	second := e.Evaluate(ActionTeamTalkMotivational, withoutRoots)
	if len(first.Factions) != len(second.Factions) {
		t.Fatalf("component count must match with/without CTE roots: %d vs %d", len(first.Factions), len(second.Factions))
	}
	for i := range first.Factions {
		f := first.Factions[i]
		if f.Cohesion < CohesionMin || f.Cohesion > CohesionMax {
			t.Fatalf("faction %q cohesion %d out of [%d,%d]", f.Label, f.Cohesion, CohesionMin, CohesionMax)
		}
		if f.Label == "" || f.LeaderID == "" {
			t.Fatalf("faction must carry a deterministic label and leader")
		}
	}
	again := e.Evaluate(ActionTeamTalkMotivational, withRoots)
	for i := range first.Factions {
		if first.Factions[i].Cohesion != again.Factions[i].Cohesion || first.Factions[i].LeaderID != again.Factions[i].LeaderID {
			t.Fatalf("factions must be deterministic across evaluations")
		}
	}
}

// TestHierarchyLeaderAndTiers: the top-centrality player is the team leader and
// every member receives exactly one tier.
func TestHierarchyLeaderAndTiers(t *testing.T) {
	e := NewEngine()
	res := e.Evaluate(ActionInspect, room())
	if len(res.Tiers) != len(room().Members) {
		t.Fatalf("every member must receive a tier, got %d of %d", len(res.Tiers), len(room().Members))
	}
	if res.Tiers["v3"] != TierTeamLeader {
		t.Fatalf("v3 has the highest centrality and must be team leader, got %s", res.Tiers["v3"])
	}
	if res.Tiers["v4"] != TierOther {
		t.Fatalf("lowest-centrality member must be 'other', got %s", res.Tiers["v4"])
	}
}

// TestBondingRaisesCohesionOnly: approved contracts / motivational talks never
// produce contagion and only ever raise cohesion, bounded by CohesionMax.
func TestBondingRaisesCohesionOnly(t *testing.T) {
	e := NewEngine()
	base := e.Evaluate(ActionInspect, room())
	res := e.Evaluate(ActionTeamTalkMotivational, room())
	if res.Contagion != nil {
		t.Fatalf("bonding actions must not produce contagion")
	}
	if len(res.Factions) != len(base.Factions) {
		t.Fatalf("bonding must not change faction count")
	}
	for i := range res.Factions {
		if res.Factions[i].Cohesion < base.Factions[i].Cohesion {
			t.Fatalf("bonding must not lower cohesion")
		}
		if res.Factions[i].Cohesion > CohesionMax {
			t.Fatalf("cohesion %d exceeds cap %d", res.Factions[i].Cohesion, CohesionMax)
		}
	}
}

// TestContagionUnknownPlayer: an epicentre outside the snapshot yields no
// contagion rather than a fabricated one.
func TestContagionUnknownPlayer(t *testing.T) {
	if c := NewEngine().Contagion("ghost", room()); c != nil {
		t.Fatalf("unknown epicentre must yield nil contagion, got %+v", c)
	}
}

func sameTiers(a, b map[string]Tier) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sameContagion(a, b *Contagion) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	if a.Confidence != b.Confidence || len(a.Affected) != len(b.Affected) {
		return false
	}
	for i := range a.Affected {
		if a.Affected[i] != b.Affected[i] {
			return false
		}
	}
	return true
}
