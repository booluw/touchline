package matchsim

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

// sampleOptions is a fixed, representative match for the suite (v1.2 block:
// two-sided Attack/Defense ratings).
func sampleOptions(seed int64) Options {
	return Options{
		Seed: seed,
		Home: Team{ID: "home-club", ClubName: "Harbour City FC", Attack: 82, Defense: 78},
		Away: Team{ID: "away-club", ClubName: "Northbank Rovers", Attack: 74, Defense: 76},
	}
}

// canonicalDigest serialises the deterministic output for snapshot comparison.
func canonicalDigest(res MatchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d-%d-%.1f;", res.HomeGoals, res.AwayGoals, res.HomePossession)
	for _, ev := range res.Events {
		fmt.Fprintf(&b, "%d|%d|%s|%s|%s|%s;", ev.Sequence, ev.Minute, ev.Type, ev.ClubID, ev.Description, ev.Detail)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// goldenSeed's output under the v1.2-approved block is pinned by the golden
// replay. Any drift (score, possession, event stream, or a single description)
// fails the test.
const goldenSeed = 424242

func TestGoldenReplay(t *testing.T) {
	got := canonicalDigest(Simulate(sampleOptions(goldenSeed)))
	const want = "0000000000000000000000000000000000000000000000000000000000000000"
	if got == want {
		t.Fatalf("test not snapshotted yet — paste the real digest:\n%s", got)
	}
	// Pinned by the first passing run of this test on the v1.2-approved block.
	// (v1.0-proposed digest for archaeology: 9836992225584048192f3aa51fc93a83511820946a2971157070dd01a10c23f3)
	const pinned = "f39c449a3b6616da9cccc657037b05d51061bd2d0741035ee26a4ea4c3b2b737"
	if got != pinned {
		t.Fatalf("golden replay drifted:\n got %s\nwant %s", got, pinned)
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
	if last.Type != EventFullTime || last.Minute != 90 {
		t.Fatalf("last event must be full-time at minute 90: %+v", last)
	}
	for i, ev := range res.Events {
		if ev.Sequence != i+1 {
			t.Fatalf("sequence gap at index %d: %+v", i, ev)
		}
		if ev.Minute < 1 || ev.Minute > 90 {
			t.Fatalf("minute out of range: %+v", ev)
		}
	}
}

// scoringEvents sums open-play goals and converted penalties; together they
// must match the score line exactly.
func scoringEvents(res MatchResult) int {
	n := 0
	for _, ev := range res.Events {
		if ev.Type == EventGoal || ev.Type == EventPenaltyScored {
			n++
		}
	}
	return n
}

func TestEventsSumToScore(t *testing.T) {
	res := Simulate(sampleOptions(31))
	if g := scoringEvents(res); g != res.HomeGoals+res.AwayGoals {
		t.Fatalf("scoring events %d disagree with score %d-%d", g, res.HomeGoals, res.AwayGoals)
	}
}

func TestOnlyApprovedEventTypes(t *testing.T) {
	allowed := map[string]bool{
		EventKickoff: true, EventGoal: true, EventAssist: true, EventChance: true,
		EventYellowCard: true, EventRedCard: true, EventSubstitution: true,
		EventInjury: true, EventPenaltyAwarded: true, EventPenaltyScored: true,
		EventPenaltyMissed: true, EventHalfTime: true, EventFullTime: true,
	}
	for i := 0; i < 40; i++ {
		for _, ev := range Simulate(sampleOptions(int64(i))).Events {
			if !allowed[ev.Type] {
				t.Fatalf("unapproved event type %q (seed %d)", ev.Type, i)
			}
		}
	}
}

func TestTwoSidedDominance(t *testing.T) {
	strong := Options{
		Seed: 5, Tuning: DefaultTuning(),
		Home: Team{ID: "a", ClubName: "Giants", Attack: 99, Defense: 99},
		Away: Team{ID: "b", ClubName: "Minnows", Attack: 40, Defense: 40},
	}
	swap := Options{
		Seed: 5, Tuning: DefaultTuning(),
		Home: Team{ID: "b", ClubName: "Minnows", Attack: 40, Defense: 40},
		Away: Team{ID: "a", ClubName: "Giants", Attack: 99, Defense: 99},
	}
	a := Simulate(strong)
	b := Simulate(swap)
	if a.HomeGoals+a.AwayGoals != b.HomeGoals+b.AwayGoals {
		t.Fatalf("home/away swap changed total goals: %d-%d vs %d-%d",
			a.HomeGoals, a.AwayGoals, b.HomeGoals, b.AwayGoals)
	}
	if a.HomeGoals+a.AwayGoals == 0 {
		t.Fatal("test is vacuous — the 99-vs-40 mismatch should produce goals")
	}
	if a.HomePossession < 50 {
		t.Fatalf("the 99 team should dominate possession, got %.1f%%", a.HomePossession)
	}
}

func TestHomeAdvantageOnEqualSides(t *testing.T) {
	equal := Options{
		Tuning: DefaultTuning(),
		Home:   Team{ID: "a", ClubName: "A", Attack: 80, Defense: 80},
		Away:   Team{ID: "b", ClubName: "B", Attack: 80, Defense: 80},
	}
	homeMinutes := 0
	const trials = 20
	for i := 0; i < trials; i++ {
		opts := equal
		opts.Seed = int64(i)
		res := Simulate(opts)
		homeMinutes += int(res.HomePossession)
	}
	// Equal ratings give 50/50 without the ×1.08 multiplier; across many
	// fixtures the expected share is well above half, far outside noise.
	if avg := float64(homeMinutes) / trials; avg <= 50 {
		t.Fatalf("home advantage could not lift average possession above 50%%: %.1f%%", avg)
	}
}

func TestPenaltyConversionFocus(t *testing.T) {
	// Force a penalty decision at (almost) every minute so conversion rates
	// are observed in bulk, not statistically.
	hot := DefaultTuning()
	hot.ShotFeedFraction = 0
	hot.ChancesPerMatchMin = 90 // every minute is a chance
	hot.OutcomeWeights = OutcomeWeights{Goal: 0, OnTarget: 0, OffTarget: 0, Blocked: 0, Foul: 10000}
	hot.PenaltyInBoxFraction = 1.0
	hot.RefereeNoiseFactor = 0 // never waved
	hot.RefereeBiasFactor = 0
	hot.FoulBasePerMatchMin = 0 // no extra per-minute fouls
	hot.SubAutoFraction = 0     // no auto subs perturbing the sample

	opts := Options{Tuning: hot,
		Home: Team{ID: "h", ClubName: "Home", Attack: 80, Defense: 80},
		Away: Team{ID: "a", ClubName: "Away", Attack: 80, Defense: 80},
	}
	always := opts
	always.Home.PenaltyConversionRate = 1.0
	never := opts
	never.Home.PenaltyConversionRate = 0.0

	a := scoreFromPenalties(t, always, 5)
	n := scoreFromPenalties(t, never, 5)
	if a == 0 {
		t.Fatal("no penalties sampled for the 1.0-conversion side; fixture broken")
	}
	if a <= n {
		t.Fatalf("a 1.0-conversion taker must convert strictly more often: %d vs %d", a, n)
	}
}

func scoreFromPenalties(t *testing.T, opts Options, n int) int {
	t.Helper()
	scored := 0
	for i := 0; i < n; i++ {
		o := opts
		o.Seed = int64(i)
		for _, ev := range Simulate(o).Events {
			if ev.Type == EventPenaltyScored {
				scored++
			}
		}
	}
	return scored
}

func TestLiveInputReplay(t *testing.T) {
	opts := Options{
		Seed: 9, Tuning: DefaultTuning(),
		Home: Team{ID: "h", ClubName: "Home FC", Attack: 80, Defense: 80},
		Away: Team{ID: "a", ClubName: "Away FC", Attack: 80, Defense: 80},
	}
	in1 := []LiveInput{
		{Minute: 60, ClubID: "a", Kind: "substitution", Detail: map[string]any{"player": "p", "sub": "r"}},
		{Minute: 60, ClubID: "h", Kind: "substitution", Detail: map[string]any{"player": "q", "sub": "s"}},
		{Minute: 75, ClubID: "a", Kind: "substitution", Detail: map[string]any{"player": "x", "sub": "y"}},
	}
	in2 := []LiveInput{in1[2], in1[0], in1[1]}
	live := opts
	live.LiveInputs = in1
	reordered := opts
	reordered.LiveInputs = in2
	if canonicalDigest(Simulate(live)) != canonicalDigest(Simulate(reordered)) {
		t.Fatalf("input ordering must not change the outcome")
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
		t.Fatalf("equal teams must be 50/50, got %v", share)
	}
	if share := possessionShare(99, 40, 3); share <= 0.9 {
		t.Fatalf("a 99-vs-40 superiority should be decisive, got %v", share)
	}
	if share := possessionShare(75, 75, 0); share != 0.5 {
		t.Fatalf("exponent 0 must be 50/50, got %v", share)
	}
}

func TestClampRating(t *testing.T) {
	if clampRating(0) != 1 || clampRating(150) != 100 {
		t.Fatalf("clamp failed: %v %v", clampRating(0), clampRating(150))
	}
	if clampRating(-5) != 1 {
		t.Fatalf("negative ratings must clamp to 1, got %v", clampRating(-5))
	}
}

func TestVarianceBand(t *testing.T) {
	low, high := 1.0, 0.0
	r := newSplitMix64(123)
	for i := 0; i < 2000; i++ {
		v := triangular(r, 0.85, 1.15)
		if v < 0.85 || v > 1.15 {
			t.Fatalf("variance outside band: %v", v)
		}
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
	}
	// mode-centred roll must span an interesting range, not degenerate.
	if high-low < 0.2 {
		t.Fatalf("variance roll too narrow: [%v,%v]", low, high)
	}
	mean := (low + high) / 2
	if mean < 0.9 || mean > 1.1 {
		t.Fatalf("variance not centred on 1.0: mean ~%v", mean)
	}
}

// styledOptions applies a style to the home side of the sample match.
func styledOptions(seed int64, style string) Options {
	opts := sampleOptions(seed)
	opts.Home.Tactics = Tactics{Style: style}
	opts.Tuning = DefaultTuning()
	return opts
}

// firstSeedWhere finds a seed where home <style> plays differently from home
// balanced, so style tests aren't vacuous.
func firstSeedWhere(style string) int64 {
	for seed := int64(0); seed < 200; seed++ {
		if canonicalDigest(Simulate(styledOptions(seed, style))) != canonicalDigest(Simulate(styledOptions(seed, StyleBalanced))) {
			return seed
		}
	}
	return -1
}

func TestGoldenStyleIsIdentity(t *testing.T) {
	// The default (no tactics set) path must equal an explicit balanced path:
	// the v1.5 block is hidden behind an identity lever for default callers.
	plain := sampleOptions(1234)
	explicit := styledOptions(1234, StyleBalanced)
	if canonicalDigest(Simulate(plain)) != canonicalDigest(Simulate(explicit)) {
		t.Fatal("balanced must be numerically identical to the unset/default path")
	}
}

func TestStylesChangeOutcome(t *testing.T) {
	// Every non-balanced style must produce at least one observable difference
	// from balanced across a seed sweep (the style levers are wired in).
	for _, style := range []string{StylePossession, StyleGegenpress, StyleLowBlock, StyleDirect} {
		if seed := firstSeedWhere(style); seed < 0 {
			t.Fatalf("style %s never differed from balanced across 200 seeds", style)
		} else {
			t.Logf("%s diverges from balanced at seed %d", style, seed)
		}
	}
}

func TestPossessionStyleShiftsPossession(t *testing.T) {
	pos, low, total := 0, 0, 0
	const trials = 30
	for i := 0; i < trials; i++ {
		seed := int64(1000 + i)
		pos += int(Simulate(styledOptions(seed, StylePossession)).HomePossession)
		low += int(Simulate(styledOptions(seed, StyleLowBlock)).HomePossession)
		total += int(Simulate(styledOptions(seed, StyleBalanced)).HomePossession)
	}
	avgPos := float64(pos) / trials
	avgLow := float64(low) / trials
	avgBase := float64(total) / trials
	if avgPos <= avgBase {
		t.Fatalf("possession style should raise home share: %.1f vs %.1f", avgPos, avgBase)
	}
	if avgLow >= avgBase {
		t.Fatalf("low-block style should lower home share: %.1f vs %.1f", avgLow, avgBase)
	}
}

func TestLiveTacticChangeReplaysStatic(t *testing.T) {
	// A tactic_change LiveInput at minute 1 must produce the byte-identical
	// match to kickoffing in that style (the switch happens before draw 1).
	style := StyleLowBlock
	static := styledOptions(3, style)
	live := sampleOptions(3)
	live.LiveInputs = []LiveInput{
		{Minute: 1, ClubID: static.Home.ID, Kind: "tactic_change", Detail: map[string]any{"style": style}},
	}
	a := Simulate(static)
	b := Simulate(live)
	if canonicalDigest(a) != canonicalDigest(b) {
		t.Fatalf("minute-1 tactic_change must equal a static kickoff in that style:\n%s\n%s",
			canonicalDigest(a), canonicalDigest(b))
	}
}

func TestLiveTacticChangeDeterministicOnReplay(t *testing.T) {
	opts := sampleOptions(8)
	inputs := []LiveInput{
		{Minute: 40, ClubID: opts.Home.ID, Kind: "tactic_change", Detail: map[string]any{"style": StyleGegenpress}},
		{Minute: 70, ClubID: opts.Away.ID, Kind: "tactic_change", Detail: map[string]any{"style": StyleLowBlock}},
	}
	reversed := []LiveInput{inputs[1], inputs[0]}
	a := opts
	a.LiveInputs = inputs
	b := opts
	b.LiveInputs = reversed
	if canonicalDigest(Simulate(a)) != canonicalDigest(Simulate(b)) {
		t.Fatal("tactic_change input ordering must not change the outcome")
	}
}

func TestFitnessDrainsLateMatchOutput(t *testing.T) {
	// A drained squad (Fitness 0.5, balanced) must underperform a fresh one
	// over many forced-chance fixtures: the post-75 penalty slices conversion
	// and possession momentum from minute 76. Forced high-volume tuning keeps
	// the difference far outside seed noise.
	hot := DefaultTuning()
	hot.ChancesPerMatchMin = 60
	hot.SubAutoFraction = 0 // no auto subs: the tank drains untouched
	opt := sampleOptions(0)
	opt.Tuning = hot
	fresh := opt
	fresh.Home.Fitness = 1.0
	tired := opt
	tired.Home.Fitness = 0.5

	freshGoals, tiredGoals := 0, 0
	const trials = 60
	for i := 0; i < trials; i++ {
		s := int64(500 + i)
		f := fresh
		f.Seed = s
		t := tired
		t.Seed = s
		fr := Simulate(f)
		tr := Simulate(t)
		freshGoals += fr.HomeGoals
		tiredGoals += tr.HomeGoals
	}
	if freshGoals-tiredGoals < trials/9 {
		t.Fatalf("drained squads should score less than fresh ones: fresh=%d tired=%d over %d trials",
			freshGoals, tiredGoals, trials)
	}
}

func TestDefaultTuningStylesAreIsolated(t *testing.T) {
	// Mutating a DefaultTuning copy must never leak into the shared proposal.
	base := DefaultTuning()
	mut := DefaultTuning()
	mut.Styles[StylePossession] = StyleSpec{PossessionShift: 0.99, ChanceVolume: 3, StaminaDecay: 2}
	if got := ProposedTuning.Styles[StylePossession].PossessionShift; got != 0.20 {
		t.Fatalf("shared proposal mutated: possession shift now %v", got)
	}
	if got := base.Styles[StylePossession].StaminaDecay; got != 1.0 {
		t.Fatalf("DefaultTuning call changed subsequent copies: %v", got)
	}
	if got := DefaultTuning().Styles[StylePossession].ChanceVolume; got != 0.90 {
		t.Fatalf("proposal corrupted through mutual copy: %v", got)
	}
}

func TestStyleEffectsAreDeterministic(t *testing.T) {
	for _, style := range []string{StylePossession, StyleGegenpress, StyleLowBlock, StyleDirect} {
		a := Simulate(styledOptions(101, style))
		b := Simulate(styledOptions(101, style))
		if canonicalDigest(a) != canonicalDigest(b) {
			t.Fatalf("style %s not deterministic", style)
		}
	}
}
