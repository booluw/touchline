package competition

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- builders -----------------------------------------------------------------

func resolvCup(prefix byte, tier int, created time.Time) CompetitionRef {
	return CompetitionRef{
		ID:        uuid.New(),
		WorldID:   uuid.New(),
		Name:      string(prefix),
		Type:      "regional_cup",
		Tier:      tier,
		Scope:     ScopeRegion,
		CreatedAt: created,
	}
}

func resolvField(cup CompetitionRef, bands []Band, entrants []Entrant) Field {
	return Field{Cup: cup, Bands: bands, ClubCount: len(entrants), Entrants: entrants}
}

func resolvInput(fields []Field, tables map[uuid.UUID]Table, choices map[uuid.UUID][]uuid.UUID) ResolveInput {
	return ResolveInput{Fields: fields, Tables: tables, Choices: choices}
}

func mustResolve(t *testing.T, in ResolveInput) *ResolveOutput {
	t.Helper()
	out, err := ResolveField(in)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return out
}

func entrantIDSet(entrants []Entrant) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	for _, e := range entrants {
		out[e.ClubID] = e.Origin
	}
	return out
}

func cascadeFor(clubID uuid.UUID, cascades []Cascade) (Cascade, bool) {
	for _, c := range cascades {
		if c.ClubID == clubID {
			return c, true
		}
	}
	return Cascade{}, false
}

// --- the C1-conflicts fixture ---------------------------------------------------
//
// League L ranks [C1, C2, C3, C4, C5]; league M ranks [z, w]. Every fixture
// below builds exactly ONE double booking: C1 holds a seat in both cup A and
// cup B (via band L 1..1 each); every other club sits in exactly one cup, so
// the resolution outcome does not depend on club processing order.
//
//   - A holds C1 as its reigning champion (champOrigin) and draws the rest of
//     its field from band L 3..3 {C3} and M 1..1 {z}.
//   - B draws C1 from L 1..1 + w from M 2..2, exactly mirroring A's shape.

func conflictFixtures(champOrigin string) ([]Field, map[uuid.UUID]Table, []uuid.UUID) {
	l, m := uuid.New(), uuid.New()
	L := clubIDs(5) // C1..C5
	z, w := uuid.New(), uuid.New()
	tables := map[uuid.UUID]Table{
		l: {LeagueID: l, SeasonNumber: 7, Ranks: L},
		m: {LeagueID: m, SeasonNumber: 7, Ranks: []uuid.UUID{z, w}},
	}
	toOne, toTwo, toThree := 1, 2, 3
	a := resolvCup('A', 1, time.Unix(1_000_000, 0))
	b := resolvCup('B', 1, time.Unix(2_000_000, 0))
	fields := []Field{
		resolvField(a, []Band{{LeagueID: l, From: 3, To: &toThree}, {LeagueID: m, From: 1, To: &toOne}}, []Entrant{
			{ClubID: L[0], Origin: champOrigin, Rank: 0},    // C1 champion of A
			{ClubID: L[2], Origin: OriginPosition, Rank: 3}, // C3 by band L 3..3
			{ClubID: z, Origin: OriginPosition, Rank: 1},    // z by band M 1..1
		}),
		resolvField(b, []Band{{LeagueID: l, From: 1, To: &toOne}, {LeagueID: m, From: 2, To: &toTwo}}, []Entrant{
			{ClubID: L[0], Origin: OriginPosition, Rank: 1}, // C1 by band L 1..1
			{ClubID: w, Origin: OriginPosition, Rank: 2},    // w by band M 2..2
		}),
	}
	return fields, tables, L
}

func TestResolveDefendDefaultKeepsChampionInOwnedCup(t *testing.T) {
	fields, tables, L := conflictFixtures(OriginChampionDirect)
	out := mustResolve(t, resolvInput(fields, tables, nil))

	aSet := entrantIDSet(out.Assignments[fields[0].Cup.ID])
	if aSet[L[0]] != OriginChampionDirect {
		t.Fatalf("champion should keep the cup it is champion of, A = %v", aSet)
	}
	cas, ok := cascadeFor(L[0], out.Cascades)
	if !ok {
		t.Fatalf("expected a C1 forfeit, got %+v", out.Cascades)
	}
	if cas.KeptCupID != fields[0].Cup.ID || cas.LostCupID != fields[1].Cup.ID || cas.ReplacementID != L[1] {
		t.Fatalf("expected C1: B→A with next-best C2, got %+v", cas)
	}
	if len(out.Cascades) != 1 {
		t.Fatalf("expected exactly one cascade, got %+v", out.Cascades)
	}
}

func TestResolveManagerChoiceBeatsDefend(t *testing.T) {
	fields, tables, L := conflictFixtures(OriginChampionOutOfBand)
	// C1 is champion of A but opts into B: the unique choice must carry it.
	out := mustResolve(t, resolvInput(fields, tables, map[uuid.UUID][]uuid.UUID{
		L[0]: {fields[1].Cup.ID},
	}))

	aSet := entrantIDSet(out.Assignments[fields[0].Cup.ID])
	if _, ok := aSet[L[0]]; ok {
		t.Fatalf("champion chose cup B, must not stay in A: %v", aSet)
	}
	bSet := entrantIDSet(out.Assignments[fields[1].Cup.ID])
	if bSet[L[0]] == "" {
		t.Fatalf("champion chose cup B, must stay there: %v", bSet)
	}
	cas, ok := cascadeFor(L[0], out.Cascades)
	if !ok || cas.KeptCupID != fields[1].Cup.ID || cas.LostCupID != fields[0].Cup.ID || cas.ReplacementID != L[3] {
		t.Fatalf("the C1 choice must favour cup B with A cascading to C4, got %+v", cas)
	}
}

func TestResolveTierPrecedenceBeatsChoiceAndDefend(t *testing.T) {
	fields, tables, L := conflictFixtures(OriginChampionOutOfBand)
	// Cup B becomes the higher tier (bigger number ⇒ higher tier, IM08/IM09
	// convention); it must beat the champion-of-A defence AND an explicit
	// choice for A.
	fields[1].Cup.Tier = 3
	out := mustResolve(t, resolvInput(fields, tables, map[uuid.UUID][]uuid.UUID{
		L[0]: {fields[0].Cup.ID},
	}))

	bSet := entrantIDSet(out.Assignments[fields[1].Cup.ID])
	if bSet[L[0]] == "" {
		t.Fatalf("higher tier must keep the club regardless of choice/defend: %v", bSet)
	}
	aSet := entrantIDSet(out.Assignments[fields[0].Cup.ID])
	if _, ok := aSet[L[0]]; ok {
		t.Fatalf("lower tier must lose the champion: %v", aSet)
	}
	cas, ok := cascadeFor(L[0], out.Cascades)
	if !ok || cas.KeptCupID != fields[1].Cup.ID {
		t.Fatalf("cascade must point at the higher tier cup, got %+v", cas)
	}
}

func TestResolveEarlierCreatedBreaksTie(t *testing.T) {
	fields, tables, L := conflictFixtures(OriginChampionOutOfBand)
	// Drop C1's champion-of-A token so neither cup is defended; equal tiers
	// then fall to the earlier-created cup.
	for i := range fields[0].Entrants {
		if fields[0].Entrants[i].ClubID != L[0] {
			continue
		}
		fields[0].Entrants[i].Origin = OriginPosition
	}
	fields[0].Entrants[0].Rank = 1
	out := mustResolve(t, resolvInput(fields, tables, nil))

	aSet := entrantIDSet(out.Assignments[fields[0].Cup.ID])
	bSet := entrantIDSet(out.Assignments[fields[1].Cup.ID])
	if _, ok := aSet[L[0]]; !ok {
		t.Fatalf("earlier-created cup A must keep the tied club: %v", aSet)
	}
	if _, ok := bSet[L[0]]; ok {
		t.Fatalf("later-created cup B must lose the tied club: %v", bSet)
	}
	cas, ok := cascadeFor(L[0], out.Cascades)
	if !ok || cas.KeptCupID != fields[0].Cup.ID || cas.ReplacementID != L[1] {
		t.Fatalf("cascade must favour cup A with B cascading to C2, got %+v", cas)
	}
}

func TestResolveAmbiguousChoicesFallBackToDefault(t *testing.T) {
	fields, tables, L := conflictFixtures(OriginChampionDirect)
	// Both cups chosen: the meaning is ambiguous, so defend (champion of A)
	// decides it — not one arbitrary choice.
	out := mustResolve(t, resolvInput(fields, tables, map[uuid.UUID][]uuid.UUID{
		L[0]: {fields[0].Cup.ID, fields[1].Cup.ID},
	}))

	aSet := entrantIDSet(out.Assignments[fields[0].Cup.ID])
	if _, ok := aSet[L[0]]; !ok {
		t.Fatalf("double choice must defer to the defend default (A): %v", aSet)
	}
	cas, ok := cascadeFor(L[0], out.Cascades)
	if !ok || cas.KeptCupID != fields[0].Cup.ID {
		t.Fatalf("cascade must favour cup A, got %+v", cas)
	}
}

func TestResolveChoicesForUnprojectedCupsAreNoise(t *testing.T) {
	fields, tables, L := conflictFixtures(OriginChampionOutOfBand)
	noise := resolvCup('X', 0, time.Unix(0, 0)) // a cup C1 is not in
	// Choice recorded for the noise cup + cup B: the only *meaningful* choice
	// is B, so it must win.
	out := mustResolve(t, resolvInput(fields, tables, map[uuid.UUID][]uuid.UUID{
		L[0]: {noise.ID, fields[1].Cup.ID},
	}))

	bSet := entrantIDSet(out.Assignments[fields[1].Cup.ID])
	if bSet[L[0]] == "" {
		t.Fatalf("the single meaningful choice (B) must carry C1: %v", bSet)
	}
	cas, ok := cascadeFor(L[0], out.Cascades)
	if !ok || cas.KeptCupID != fields[1].Cup.ID {
		t.Fatalf("cascade must favour cup B, got %+v", cas)
	}
}

func TestResolveCascadeChainToFixpoint(t *testing.T) {
	// League L ranks [C1, x, C2, C3]; league M ranks [z].
	// Cup C (tier 3, higher): band L 1..2 = {C1, x}.
	// Cup A (tier 2, lower): band L 1..1 = {C1} + band M 1..1 = {z}.
	// C1 is double-booked; tier keeps C1 in C. A's next-best x is already in C,
	// so x forfeits A for C too, and A cascades again to C2 (IM09's documented
	// cascade chain: A→x qualifies for C→x forfeits A for C, A cascades again).
	l, m := uuid.New(), uuid.New()
	c1, x, c2, c3, z := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tables := map[uuid.UUID]Table{
		l: {LeagueID: l, SeasonNumber: 7, Ranks: []uuid.UUID{c1, x, c2, c3}},
		m: {LeagueID: m, SeasonNumber: 7, Ranks: []uuid.UUID{z}},
	}
	toOne := 1
	toTwo := 2
	c := resolvCup('C', 3, time.Unix(0, 0))
	a := resolvCup('A', 2, time.Unix(1, 0))
	fields := []Field{
		resolvField(c, []Band{{LeagueID: l, From: 1, To: &toTwo}}, []Entrant{
			{ClubID: c1, Origin: OriginPosition, Rank: 1},
			{ClubID: x, Origin: OriginPosition, Rank: 2},
		}),
		resolvField(a, []Band{{LeagueID: l, From: 1, To: &toOne}, {LeagueID: m, From: 1, To: &toOne}}, []Entrant{
			{ClubID: c1, Origin: OriginPosition, Rank: 1},
			{ClubID: z, Origin: OriginPosition, Rank: 1},
		}),
	}
	out := mustResolve(t, resolvInput(fields, tables, nil))

	cSet := entrantIDSet(out.Assignments[c.ID])
	aSet := entrantIDSet(out.Assignments[a.ID])
	if aSet[c1] != "" || aSet[x] != "" {
		t.Fatalf("C1 and x must both end up in the higher tier cup C, A = %v", aSet)
	}
	if cSet[c1] == "" || cSet[x] == "" {
		t.Fatalf("higher tier cup C keeps C1 and x, got %v", cSet)
	}
	if _, ok := aSet[c2]; !ok {
		t.Fatalf("cup A cascades again and lands on C2, got %v", aSet)
	}
	if len(out.Cascades) != 2 {
		t.Fatalf("expected 2 cascades, got %+v", out.Cascades)
	}
	for _, cas := range out.Cascades {
		if cas.KeptCupID != c.ID || cas.LostCupID != a.ID {
			t.Fatalf("every cascade must forfeit A to C, got %+v", cas)
		}
	}
}

func TestResolveIsDeterministicUnderInputShuffle(t *testing.T) {
	fields, tables, _ := conflictFixtures(OriginChampionOutOfBand)
	first := mustResolve(t, resolvInput(fields, tables, nil))

	shuffled := []Field{fields[1], fields[0]}
	second := mustResolve(t, resolvInput(shuffled, tables, nil))
	if !reflect.DeepEqual(first.Assignments, second.Assignments) {
		t.Fatalf("assignments differ when field order changes:\n%v\n%v", first.Assignments, second.Assignments)
	}
	if !reflect.DeepEqual(first.Cascades, second.Cascades) {
		t.Fatalf("cascades differ when field order changes:\n%+v\n%+v", first.Cascades, second.Cascades)
	}
}

func TestResolveErrResolutionImpossibleWhenCupShrinks(t *testing.T) {
	// Cup A is champion_direct only: C1 is its champion with no band seat, so
	// it has no natural replacement when the higher tier cup claims C1.
	l := uuid.New()
	c1, c2, c3 := uuid.New(), uuid.New(), uuid.New()
	tables := map[uuid.UUID]Table{l: {LeagueID: l, SeasonNumber: 7, Ranks: []uuid.UUID{c1, c2, c3}}}
	toThree := 3
	a := resolvCup('A', 2, time.Unix(0, 0))
	b := resolvCup('B', 3, time.Unix(1, 0))
	fields := []Field{
		resolvField(a, nil, []Entrant{{ClubID: c1, Origin: OriginChampionDirect, Rank: 0}}),
		resolvField(b, []Band{{LeagueID: l, From: 1, To: &toThree}}, []Entrant{
			{ClubID: c1, Origin: OriginPosition, Rank: 1},
			{ClubID: c2, Origin: OriginPosition, Rank: 2},
			{ClubID: c3, Origin: OriginPosition, Rank: 3},
		}),
	}
	_, err := ResolveField(resolvInput(fields, tables, nil))
	if err == nil {
		t.Fatal("expected ErrResolutionImpossible for a cup that shrinks below two clubs")
	}
}
