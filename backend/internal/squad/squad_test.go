package squad

import (
	"fmt"
	"math"
	"testing"

	"github.com/google/uuid"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func moraleStub(id uuid.UUID, leadership int, starter bool, sentiment int) PlayerMoraleInput {
	return PlayerMoraleInput{PlayerID: id, Leadership: leadership, IsLikelyStarter: starter, CurrentSentiment: sentiment}
}

func TestComputeSquadMoraleNeutralWhenNoSentiment(t *testing.T) {
	tuning := ProposedTuning
	squad := []PlayerMoraleInput{
		moraleStub(uuid.New(), 60, true, 0),
		moraleStub(uuid.New(), 80, true, 0),
	}
	if m := ComputeSquadMorale(squad, tuning); !approx(m.Rating, 1.0) {
		t.Fatalf("no sentiment => morale %v, want 1.0", m.Rating)
	}
}

func TestComputeSquadMoraleWeightsLeadersAndStarters(t *testing.T) {
	tuning := ProposedTuning
	// Same sentiment everywhere: the group average of sentiment -50 must give
	// 1 - 0.5*0.10 = 0.95 regardless of who is leader or starter.
	squad := []PlayerMoraleInput{
		moraleStub(uuid.New(), 90, true, -50),
		moraleStub(uuid.New(), 10, false, -50),
		moraleStub(uuid.New(), 40, true, -50),
	}
	want := 1 - 0.5*tuning.MoraleSpan
	if m := ComputeSquadMorale(squad, tuning); !approx(m.Rating, want) {
		t.Fatalf("uniform negative sentiment => %v, want %v", m.Rating, want)
	}
}

func TestComputeSquadMoraleClampsToBand(t *testing.T) {
	tuning := ProposedTuning
	squad := []PlayerMoraleInput{
		moraleStub(uuid.New(), 100, true, 100),
		moraleStub(uuid.New(), 90, true, 100),
		moraleStub(uuid.New(), 60, true, 100),
	}
	m := ComputeSquadMorale(squad, tuning)
	if m.Rating > 1+tuning.MoraleSpan+1e-9 {
		t.Fatalf("morale %v above band ceiling %v", m.Rating, 1+tuning.MoraleSpan)
	}

	squad = []PlayerMoraleInput{
		moraleStub(uuid.New(), 100, true, -100),
		moraleStub(uuid.New(), 90, true, -100),
	}
	m = ComputeSquadMorale(squad, tuning)
	if m.Rating < 1-tuning.MoraleSpan-1e-9 {
		t.Fatalf("morale %v below band floor %v", m.Rating, 1-tuning.MoraleSpan)
	}
	if !approx(m.Rating, 1-tuning.MoraleSpan) {
		t.Fatalf("unanimous extreme sentiment must hit band edge exactly, got %v", m.Rating)
	}
}

func TestComputeMotivationHighStakesLift(t *testing.T) {
	tuning := ProposedTuning
	amb := 90
	dna := ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: amb}
	seed := int64(20260101)
	got := ComputeMotivation(dna, FixtureContext{IsCupTie: true}, 0, seed, tuning)
	want := 1.0 + tuning.HighStakesSpan*(0.5+0.5*float64(amb)/100)
	if !approx(got.Factor, want) {
		t.Fatalf("cup tie => %v, want %v", got.Factor, want)
	}
}

func TestComputeMotivationDeadRubberDrag(t *testing.T) {
	tuning := ProposedTuning
	seed := int64(7)
	// Ambition 100 -> drag off: factor must be a clean 1.0.
	dnaHigh := ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: 100}
	if got := ComputeMotivation(dnaHigh, FixtureContext{IsDeadRubber: true}, 0, seed, tuning); !approx(got.Factor, 1.0) {
		t.Fatalf("dead rubber with ambition 100 => %v, want 1.0", got.Factor)
	}
	// Ambition 0 -> floor: exactly the drag low is a clean, rounding-safe mark.
	dnaLow := ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: 0}
	if got := ComputeMotivation(dnaLow, FixtureContext{IsDeadRubber: true}, 0, seed+1, tuning); !approx(got.Factor, tuning.AmbitionDragLow) {
		t.Fatalf("dead rubber with ambition 0 => %v, want %v", got.Factor, tuning.AmbitionDragLow)
	}
}

func TestComputeMotivationDerbyFloor(t *testing.T) {
	tuning := ProposedTuning
	// Ambition 0 keeps the high-stakes base at its minimum (1 + span/2) so the
	// rivalry floor has room to dominate in an intense derby.
	dna := ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: 0}
	got := ComputeMotivation(dna, FixtureContext{IsDerby: true, DerbyIntensity: 68}, 0, 3, tuning)
	want := 1.0 + 0.68*tuning.DeriveFloorSpan
	if !approx(got.Factor, want) {
		t.Fatalf("derby floor => %v, want %v", got.Factor, want)
	}

	// A low-intensity derby keeps the weaker ambition base.
	got = ComputeMotivation(ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: 100},
		FixtureContext{IsDerby: true, DerbyIntensity: 10}, 0, 3, tuning)
	want = 1.0 + tuning.HighStakesSpan*(1.0)
	if !approx(got.Factor, want) {
		t.Fatalf("low-intensity derby => %v, want %v", got.Factor, want)
	}
}

func TestComputeMotivationGiantKillingIsDeterministicAndGated(t *testing.T) {
	tuning := ProposedTuning
	dna := ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: 30}
	seed := int64(99)
	ctx := FixtureContext{IsDeadRubber: false}

	// Deterministic: same input, same result.
	a := ComputeMotivation(dna, ctx, 30, seed, tuning)
	b := ComputeMotivation(dna, ctx, 30, seed, tuning)
	if !approx(a.Factor, b.Factor) {
		t.Fatalf("same seed must reproduce, got %v vs %v", a.Factor, b.Factor)
	}

	// Any roll's outcome is inside the band (this is the whole guarantee).
	for s := int64(0); s < 200; s++ {
		got := ComputeMotivation(dna, ctx, 30, s, tuning)
		if got.Factor < tuning.MotivBandLow || got.Factor > tuning.MotivBandHigh+1e-9 {
			t.Fatalf("seed %d: factor %v outside band", s, got.Factor)
		}
	}

	// Gating: a dead rubber (no stakes) never flips to the upset max.
	if got := ComputeMotivation(dna, FixtureContext{IsDeadRubber: true}, 30, 5, tuning); approx(got.Factor, tuning.GrantUnderdogMax) {
		t.Fatalf("dead rubber must not giant-kill, got %v", got.Factor)
	}

	// Sweep seeds so the per-club deterministic draw eventually lands inside
	// the 10% roll: the upset must be reachable.
	flipped := false
	for s := int64(0); s < 10000; s++ {
		g := ComputeMotivation(dna, ctx, 30, s, tuning)
		if approx(g.Factor, tuning.GrantUnderdogMax) {
			flipped = true
			break
		}
	}
	if !flipped {
		t.Fatal("giant-killing roll never fired across the 10,000-seed sweep")
	}
}

func TestComputeMotivationReputationGapGate(t *testing.T) {
	tuning := ProposedTuning
	dna := ClubDNAInput{ClubID: uuid.New(), CompetitiveAmbition: 30}
	// Sweep seeds with a below-threshold gap: the roll must be suppressed, so
	// the upset max is unreachable (no other mechanism produces exactly it).
	for s := int64(0); s < 200; s++ {
		got := ComputeMotivation(dna, FixtureContext{IsCupTie: true}, tuning.ReputationGapMin-1, s, tuning)
		if approx(got.Factor, tuning.GrantUnderdogMax) {
			t.Fatalf("seed %d: sub-threshold gap reached upset max %v", s, got.Factor)
		}
	}
}

func TestComputePlayerPerformanceFactorBand(t *testing.T) {
	tuning := ProposedTuning
	base := PlayerPerformanceInput{
		PlayerID: uuid.New(), Consistency: 80, Temperament: 60, PressureHandling: 60, Professionalism: 70, CurrentSentiment: 0,
	}
	for s := int64(0); s < 500; s++ {
		f := ComputePlayerPerformanceFactor(base, FixtureContext{IsDeadRubber: true}, s, tuning)
		if f.Factor < tuning.PerfBandLow-1e-9 || f.Factor > tuning.PerfBandHigh+1e-9 {
			t.Fatalf("seed %d: factor %v outside [%v, %v]", s, f.Factor, tuning.PerfBandLow, tuning.PerfBandHigh)
		}
	}
}

func TestComputePlayerPerformanceFactorConsistencyWidth(t *testing.T) {
	tuning := ProposedTuning
	// Always within 1% of neutral for a consistency-100 player, whatever seed.
	steady := PlayerPerformanceInput{PlayerID: uuid.New(), Consistency: 100, CurrentSentiment: 0}
	for s := int64(0); s < 200; s++ {
		f := ComputePlayerPerformanceFactor(steady, FixtureContext{}, s, tuning)
		if math.Abs(f.Factor-1) > tuning.ConsistencyMinWidth+1e-9 {
			t.Fatalf("consistency 100, seed %d: |factor-1|=%v > min width %v",
				s, math.Abs(f.Factor-1), tuning.ConsistencyMinWidth)
		}
	}
}

func TestComputePlayerPerformanceFactorStakesTemperament(t *testing.T) {
	tuning := ProposedTuning
	// A very hot-headed player in a derby must be dragged below neutral by
	// exactly the temperament+pressure divergence; the dead-rubber variant
	// must never apply it. Same seed => same consistency draw => the delta is
	// measurable precisely.
	hot := PlayerPerformanceInput{
		PlayerID: uuid.New(), Consistency: 100, Temperament: 5, PressureHandling: 60, CurrentSentiment: 0,
	}
	derby := ComputePlayerPerformanceFactor(hot, FixtureContext{IsDerby: true, DerbyIntensity: 90}, 1, tuning)
	glacial := ComputePlayerPerformanceFactor(hot, FixtureContext{IsDeadRubber: true}, 1, tuning)
	if math.Abs(glacial.Factor-1) > tuning.ConsistencyMinWidth+1e-9 {
		t.Fatalf("dead rubber must not apply temperament penalty: %v", glacial.Factor)
	}
	pressure := (60.0 - 50) / 50 * tuning.StakesPressureSpan
	temper := (5.0 - 50) / 50 * tuning.StakesTemperamentSpan
	if !approx(derby.Factor-glacial.Factor, pressure+temper) {
		t.Fatalf("derby vs dead-rubber delta %v, want %v (pressure %v + temper %v)",
			derby.Factor-glacial.Factor, pressure+temper, pressure, temper)
	}
}

func TestComputePlayerPerformanceFactorSentimentNudge(t *testing.T) {
	tuning := ProposedTuning
	// Sentiment is the only non-neutral input here; the residual width noise
	// is bounded by the consistency-100 minimum width.
	down := PlayerPerformanceInput{PlayerID: uuid.New(), Consistency: 100, CurrentSentiment: -50}
	f := ComputePlayerPerformanceFactor(down, FixtureContext{}, 1, tuning)
	want := 1 - 0.5*tuning.SentimentSpan
	if math.Abs(f.Factor-want) > tuning.ConsistencyMinWidth+1e-9 {
		t.Fatalf("sentiment -50 => %v, want ~%v (± width)", f.Factor, want)
	}
}

func TestSelectKeyPlayersTakesTopThree(t *testing.T) {
	xi := []SquadMember{
		{PlayerID: mustID(1), Position: "ST", AttributeWeight: 90, IsCaptain: true, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(2), Position: "CM", AttributeWeight: 80, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(3), Position: "CB", AttributeWeight: 75, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(4), Position: "DM", AttributeWeight: 60, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(5), Position: "LB", AttributeWeight: 55, Hidden: hidden(70), CurrentSentiment: 0},
	}
	got := SelectKeyPlayers(xi)
	if len(got) != 3 {
		t.Fatalf("routine top-3 band, got %d", len(got))
	}
	wantOrder := []uuid.UUID{mustID(1), mustID(2), mustID(3)}
	for i, p := range got {
		if p.PlayerID != wantOrder[i] {
			t.Fatalf("rank %d: got %v, want %v", i, p.PlayerID, wantOrder[i])
		}
	}
}

func TestSelectKeyPlayersExtendsForAnchors(t *testing.T) {
	xi := []SquadMember{
		{PlayerID: mustID(1), Position: "ST", AttributeWeight: 50, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(2), Position: "AM", AttributeWeight: 49, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(3), Position: "ST", AttributeWeight: 48, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(4), Position: "GK", AttributeWeight: 30, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(5), Position: "CM", AttributeWeight: 40, Hidden: hidden(70), CurrentSentiment: 0},
		{PlayerID: mustID(6), Position: "RB", AttributeWeight: 35, Hidden: hidden(70), CurrentSentiment: 0},
	}
	got := SelectKeyPlayers(xi)
	// Top-3 by ability are ST(50), AM(49), ST(48); the goalkeeper (weight 30)
	// is the fourth — an anchor the ability scan and the ability-driven top-3
	// miss, so she promotes into the key-player set. Rotation players never do.
	if len(got) != 4 {
		t.Fatalf("anchor band expected 4, got %d", len(got))
	}
	if !containsKeyPlayer(got, mustID(4)) {
		t.Fatalf("GK anchor missing from selection")
	}
	if !containsKeyPlayer(got, mustID(1)) || !containsKeyPlayer(got, mustID(2)) || !containsKeyPlayer(got, mustID(3)) {
		t.Fatalf("striker/playmaker/top-3 anchors missing")
	}
	if containsKeyPlayer(got, mustID(5)) || containsKeyPlayer(got, mustID(6)) {
		t.Fatalf("rotation players selected as key players")
	}
}

func TestSelectKeyPlayersEmotionalUnion(t *testing.T) {
	xi := []SquadMember{
		{PlayerID: mustID(1), Position: "ST", AttributeWeight: 60, Hidden: hidden(70), CurrentSentiment: 0},
		// Not in top-3, but the press night before has him at -65: still a key
		// player per Part 7 §1.
		{PlayerID: mustID(2), Position: "DM", AttributeWeight: 40, Hidden: hidden(70), CurrentSentiment: -65},
		{PlayerID: mustID(3), Position: "CB", AttributeWeight: 55, Hidden: hidden(70), CurrentSentiment: 0},
	}
	got := SelectKeyPlayers(xi)
	if !containsKeyPlayer(got, mustID(2)) {
		t.Fatalf("emotionally-severe starter missing from selection")
	}
}

func TestPickCaptainHighestLeadership(t *testing.T) {
	xi := []SquadMember{
		{PlayerID: mustID(1), Position: "CM", Leadership: 40, Hidden: hidden(50)},
		{PlayerID: mustID(2), Position: "CB", Leadership: 88, Hidden: hidden(50)},
		{PlayerID: mustID(3), Position: "GK", Leadership: 70, Hidden: hidden(50)},
	}
	if c := PickCaptain(xi); c == nil || c.PlayerID != mustID(2) {
		t.Fatalf("PickCaptain chose %+v, want CB with leadership 88", c)
	}
}

func hidden(c int) PlayerHiddenTraitsSnapshot {
	return PlayerHiddenTraitsSnapshot{Consistency: c, Professionalism: 60, Temperament: 60, PressureHandling: 60}
}

// mustID builds a stable deterministic uuid from a small int (distinct within
// a test is all that matters; digits are valid hex).
func mustID(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", n))
}

func containsKeyPlayer(xs []PlayerPerformanceInput, id uuid.UUID) bool {
	for _, x := range xs {
		if x.PlayerID == id {
			return true
		}
	}
	return false
}
