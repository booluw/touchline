package personality

import "testing"

// --- S09-01 acceptance evidence (DB-free, deterministic) -------------------
// The engine is a pure, deterministic function of (Action, TraitSet): no RNG,
// no database, no wall clock, so these run in the plain CI unit gate
// (`go test -race ./...`, backend-test) and are stable forever.

func TestReactCoversEveryAction(t *testing.T) {
	e := NewEngine()
	base := TraitSet{
		Professionalism: 5, Ambition: 5, Loyalty: 5, Ego: 5, Sociability: 5,
		Adaptability: 5, Patience: 5, Leadership: 5, Volatility: 5,
		Potential: 5, PotentialLocked: false, Consistency: 5,
		InjurySusceptibility: 5, PressureHandling: 5, LearningSpeed: 5,
	}

	// ActionResearchDenied is the one documented reserved seam: the research
	// room is a "not built yet" backlog item, so its contract is a
	// deterministic error naming the seam — asserted separately below, keeping
	// the every-action loop total over everything that is actually resolvable.
	actions := []Action{
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
	}
	for _, a := range actions {
		resp, err := e.React(a, base)
		if err != nil {
			t.Fatalf("action %q: unexpected error: %v", a, err)
		}
		if resp.Action != a {
			t.Fatalf("action %q: response echoed %q", a, resp.Action)
		}
		if len(resp.Reactions) == 0 {
			t.Fatalf("action %q: produced no reaction", a)
		}
		for _, r := range resp.Reactions {
			if r.Explanation.Trait == "" || r.Explanation.Text == "" {
				t.Fatalf("action %q: reaction missing its Explanation (trait=%q text=%q)", a, r.Explanation.Trait, r.Explanation.Text)
			}
		}
	}

	if _, err := e.React(ActionResearchDenied, base); err == nil {
		t.Fatal("ActionResearchDenied is a documented reserved seam and MUST error")
	}
}

func TestHiddenTraitGraduallyReveals(t *testing.T) {
	e := NewEngine()
	// A high-potential, pressure-adept player dropped from the squad on release
	// — the one seam React wires to maybeReveal for today.
	hidden := TraitSet{
		Professionalism: 6, Ambition: 7, Loyalty: 4, Ego: 6, Sociability: 6,
		Adaptability: 5, Patience: 5, Leadership: 5, Volatility: 4,
		Potential: 9, PotentialLocked: false, Consistency: 6,
		InjurySusceptibility: 5, PressureHandling: 8, LearningSpeed: 6,
	}

	first, err := e.React(ActionReleasedFromSquad, hidden)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revealed == nil {
		t.Fatal("expected a gradual reveal on the release seam for a hidden-capable player")
	}
	if first.Revealed.Confidence >= 100 {
		t.Fatalf("first reveal must be gradual, got confidence %d", first.Revealed.Confidence)
	}

	// The hardening side of "gradual over time" is the seam's own calculus, and
	// it is provable in-package, DB-free: maybeReveal takes a caller-supplied
	// priorConfidence (that is the OPD-30 reveal-ledger seam — the ledger owner
	// feeds evidence in, the engine never reaches for a database), and hardens
	// it deterministically toward the bounded cap. We prove the full curve here
	// so the acceptance owns the actual acceptance: starts low, hardens every
	// step, converges to the 95 cap, and NEVER touches 100.
	prior := 0
	for step := 1; step <= 8; step++ {
		r := e.maybeReveal(SrcPressureLeak, hidden, prior)
		if r == nil {
			t.Fatalf("hardening step %d: no reveal for pressure evidence", step)
		}
		if r.Confidence >= 100 {
			t.Fatalf("hardening step %d: reveal must stay bounded under 100, got %d", step, r.Confidence)
		}
		if r.Confidence < prior {
			t.Fatalf("hardening step %d: confidence must never decay: prior=%d got=%d", step, prior, r.Confidence)
		}
		if r.Confidence < 95 && r.Confidence <= prior {
			t.Fatalf("hardening step %d: strictly hardening below the cap: prior=%d got=%d", step, prior, r.Confidence)
		}
		prior = r.Confidence
	}
	if prior != 95 {
		t.Fatalf("repeated consistent evidence must converge to the embedded cap 95, got %d", prior)
	}
}
