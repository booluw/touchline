package matchsim

import "testing"

func instructedOptions(seed int64, in Instructions) Options {
	opts := styledOptions(seed, StyleBalanced)
	opts.Home.Tactics.Instructions = in
	return opts
}

// Neutral dials must not move a single draw (pre-IM61 snapshots replay).
func TestNeutralInstructionsAreIdentity(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		if canonicalDigest(Simulate(instructedOptions(seed, Instructions{}))) != canonicalDigest(Simulate(styledOptions(seed, StyleBalanced))) {
			t.Fatalf("seed %d: neutral instructions changed the match", seed)
		}
	}
}

// Each dial at either end must reach the engine.
func TestEveryInstructionChangesOutcome(t *testing.T) {
	dials := map[string]func(int) Instructions{
		"mentality": func(v int) Instructions { return Instructions{Mentality: v} },
		"pressing":  func(v int) Instructions { return Instructions{Pressing: v} },
		"width":     func(v int) Instructions { return Instructions{Width: v} },
		"tempo":     func(v int) Instructions { return Instructions{Tempo: v} },
	}
	for name, mk := range dials {
		for _, v := range []int{-1, 1} {
			diverged := false
			for seed := int64(0); seed < 200 && !diverged; seed++ {
				diverged = canonicalDigest(Simulate(instructedOptions(seed, mk(v)))) != canonicalDigest(Simulate(styledOptions(seed, StyleBalanced)))
			}
			if !diverged {
				t.Fatalf("%s=%d never changed a match across 200 seeds", name, v)
			}
		}
	}
}

// Positive mentality should produce more home chances on average than cautious.
func TestMentalityMovesChanceVolume(t *testing.T) {
	pos, cau := 0, 0
	for seed := int64(0); seed < 60; seed++ {
		pos += homeChances(Simulate(instructedOptions(seed, Instructions{Mentality: 1})))
		cau += homeChances(Simulate(instructedOptions(seed, Instructions{Mentality: -1})))
	}
	if pos <= cau {
		t.Fatalf("positive mentality should create more chances: %d vs %d", pos, cau)
	}
}

// Live style switches keep the side's instructions.
func TestLiveTacticChangeKeepsInstructions(t *testing.T) {
	in := Instructions{Pressing: 1}
	got := withInstructions(DefaultTuning().styleSpec(StyleDirect), in)
	want := DefaultTuning().styleSpec(StyleDirect)
	if got.StaminaDecay <= want.StaminaDecay || got.CardRate <= want.CardRate {
		t.Fatalf("high press not applied over direct: %+v vs %+v", got, want)
	}
}

func homeChances(r MatchResult) int {
	n := 0
	for _, e := range r.Events {
		if (e.Type == EventChance || e.Type == EventGoal) && e.ClubID == sampleOptions(0).Home.ID {
			n++
		}
	}
	return n
}
