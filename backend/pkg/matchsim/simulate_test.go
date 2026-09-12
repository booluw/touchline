package matchsim

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

// sampleOptions is a fixed, representative match for the suite.
func sampleOptions(seed int64) Options {
	return Options{
		Seed: seed,
		Home: Team{ID: "home-club", ClubName: "Harbour City FC", Ability: 82},
		Away: Team{ID: "away-club", ClubName: "Northbank Rovers", Ability: 74},
	}
}

// canonicalDigest serialises the deterministic output for snapshot comparison.
func canonicalDigest(res MatchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d-%d-%.1f;", res.HomeGoals, res.AwayGoals, res.HomePossession)
	for _, ev := range res.Events {
		fmt.Fprintf(&b, "%d|%d|%s|%s|%s;", ev.Sequence, ev.Minute, ev.Type, ev.ClubID, ev.Description)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// goldenDigest forgets the output of SampleSeed under the v1.0-proposed tuning.
// Any drift (score, possession, event stream, or a single description) fails.
const goldenSeed = 424242

func TestGoldenReplay(t *testing.T) {
	got := canonicalDigest(Simulate(sampleOptions(goldenSeed)))
	const want = "0000000000000000000000000000000000000000000000000000000000000000"
	if got == want {
		t.Fatalf("test not snapshotted yet — paste the real digest\n%s", got)
	}
	// Pinned by the first passing run of this test.
	const pinned = "9836992225584048192f3aa51fc93a83511820946a2971157070dd01a10c23f3"
	if got != pinned {
		t.Fatalf("golden replay drifted:\n got %s\nwant %s\n(replay it with\n  t.Logf(Simulate(sampleOptions(goldenSeed)).Events))", got, pinned)
	}
}

func TestDeterminism(t *testing.T) {
	a := Simulate(sampleOptions(99))
	for i := 0; i < 25; i++ {
		b := Simulate(sampleOptions(99))
		if canonicalDigest(a) != canonicalDigest(b) {
			t.Fatalf("same inputs produced different outputs (run %d)", i)
		}
	}
	if canonicalDigest(Simulate(sampleOptions(100))) == canonicalDigest(a) {
		t.Fatal("different seeds unexpectedly produced identical output")
	}
}

func TestDeterministicAcrossCopies(t *testing.T) {
	base := DefaultTuning()
	base.SubWindows = append([]int{}, DefaultTuning().SubWindows...)
	opts := sampleOptions(1234)
	opts.Tuning = base
	alt := opts
	alt.Tuning = DefaultTuning()
	if canonicalDigest(Simulate(opts)) != canonicalDigest(Simulate(alt)) {
		t.Fatal("DefaultTuning copies must simulate identically")
	}
}

func TestShape(t *testing.T) {
	res := Simulate(sampleOptions(7))
	if res.HomeGoals < 0 || res.AwayGoals < 0 {
		t.Fatalf("negative score: %d-%d", res.HomeGoals, res.AwayGoals)
	}
	if len(res.Events) < 3 { // kickoff, half-time, full-time minimum
		t.Fatalf("expected at least structural events, got %d", len(res.Events))
	}
	if first := res.Events[0]; first.Type != EventKickoff || first.Minute != 1 || first.Sequence != 1 {
		t.Fatalf("first event must be kickoff at minute 1 sequence 1: %+v", first)
	}
	last := res.Events[len(res.Events)-1]
	if last.Type != EventFullTime && last.Minute != 90 {
		t.Fatalf("last event must be full-time at minute 90: %+v", last)
	}
	// Sequences are strictly increasing and minutes stay in [1,90].
	for i, ev := range res.Events {
		if ev.Sequence != i+1 {
			t.Fatalf("sequence gap at index %d: %+v", i, ev)
		}
		if ev.Minute < 1 || ev.Minute > 90 {
			t.Fatalf("minute out of range: %+v", ev)
		}
	}
}

func TestEventsSumToScore(t *testing.T) {
	res := Simulate(sampleOptions(31))
	goals := 0
	for _, ev := range res.Events {
		if ev.Type == EventGoal {
			goals++
		}
	}
	if goals != res.HomeGoals+res.AwayGoals {
		t.Fatalf("goal events %d disagree with score %d-%d", goals, res.HomeGoals, res.AwayGoals)
	}
}

func TestAbilityDominance(t *testing.T) {
	strong := Options{
		Seed: 5, Tuning: DefaultTuning(),
		Home: Team{ID: "a", ClubName: "Giants", Ability: 99},
		Away: Team{ID: "b", ClubName: "Minnows", Ability: 40},
	}
	weakFirst := Options{
		Seed: 5, Tuning: DefaultTuning(),
		Home: Team{ID: "b", ClubName: "Minnows", Ability: 40},
		Away: Team{ID: "a", ClubName: "Giants", Ability: 99},
	}
	strongRes := Simulate(strong)
	weakRes := Simulate(weakFirst)
	totalStrong := strongRes.HomeGoals + strongRes.AwayGoals
	totalWeak := weakRes.HomeGoals + weakRes.AwayGoals
	if strongRes.HomeGoals+strongRes.AwayGoals < weakRes.HomeGoals+weakRes.AwayGoals {
		t.Fatalf("swap of home/away must not flip totals: %d-%d vs %d-%d",
			strongRes.HomeGoals, strongRes.AwayGoals, weakRes.HomeGoals, weakRes.AwayGoals)
	}
	if totalStrong != totalWeak {
		t.Fatalf("home/away swap changed total goals: strong %d, weak %d", totalStrong, totalWeak)
	}
	if strongRes.HomePossession < 50 {
		t.Fatalf("the 99-ability team should dominate possession, got %.1f%%", strongRes.HomePossession)
	}
}

func TestPerformance(t *testing.T) {
	start := time.Now()
	for i := 0; i < 50; i++ {
		Simulate(sampleOptions(int64(i)))
	}
	elapsed := time.Since(start)
	if avg := elapsed / 50; avg > time.Millisecond {
		t.Fatalf("single simulation too slow: %v", avg)
	}
}

func TestPossessionShare(t *testing.T) {
	if share := possessionShare(75, 75, 3); share != 0.5 {
		t.Fatalf("equal abilities must be 50/50, got %v", share)
	}
	if share := possessionShare(99, 40, 3); share <= 0.9 {
		t.Fatalf("a 99-vs-40 superiority should be decisive, got %v", share)
	}
	if share := possessionShare(75, 75, 0); share != 0.5 {
		t.Fatalf("exponent 0 must be 50/50, got %v", share)
	}
}

func TestClampAbility(t *testing.T) {
	if clampAbility(0) != 1 || clampAbility(150) != 100 {
		t.Fatalf("clamp failed: %v %v", clampAbility(0), clampAbility(150))
	}
}