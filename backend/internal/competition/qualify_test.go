package competition

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// --- provider fakes (DB-free) -------------------------------------------------

type fakeStandings struct {
	tables map[uuid.UUID]*Table
}

func (f fakeStandings) LastCompletedStandings(_ context.Context, leagueID uuid.UUID) (*Table, error) {
	return f.tables[leagueID], nil
}

type fakeChampion struct {
	reigning *Reigning
	byCup    map[uuid.UUID]*Reigning
}

func (f fakeChampion) ReigningChampion(_ context.Context, cupID uuid.UUID) (*Reigning, error) {
	if f.byCup != nil {
		if r, ok := f.byCup[cupID]; ok {
			return r, nil
		}
	}
	return f.reigning, nil
}

type fakeSiblings struct {
	siblings []SiblingCup
}

func (f fakeSiblings) SiblingCups(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]SiblingCup, error) {
	return f.siblings, nil
}

// --- small builders ------------------------------------------------------------

func clubIDs(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

func leagueTable(league uuid.UUID, number int, clubs ...uuid.UUID) *Table {
	return &Table{LeagueID: league, SeasonNumber: number, Ranks: clubs}
}

func ptr(v int) *int { return &v }

func qualLeague(_ byte) uuid.UUID {
	return uuid.New()
}

// --- the case matrix -------------------------------------------------------------

func TestQualifyFieldUnionOfBands(t *testing.T) {
	ctx := context.Background()
	l1, l2 := qualLeague('1'), qualLeague('2')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3], clubs[4], clubs[5])
	table2 := leagueTable(l2, 1, clubs[6], clubs[7])

	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(2)}, {LeagueID: l2, From: 1, To: ptr(1)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1, l2: table2}},
		fakeChampion{}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != 3 {
		t.Fatalf("club count = %d, want 3", field.ClubCount)
	}
	want := map[uuid.UUID]bool{clubs[0]: true, clubs[1]: true, clubs[6]: true}
	got := map[uuid.UUID]bool{}
	for _, e := range field.Entrants {
		got[e.ClubID] = true
		if e.Origin != OriginPosition {
			t.Fatalf("entrant %s origin = %s, want position", e.ClubID, e.Origin)
		}
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("entrants = %+v, want %+v", got, want)
	}
}

func TestQualifyFieldBandOrderIndependentOfInput(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(6)
	table1 := leagueTable(l1, 1, clubs...)
	bands := []Band{{LeagueID: l1, From: 1, To: ptr(3)}}

	a, err := ComputeField(ctx, QualifyField{Cup: CompetitionRef{ID: uuid.New(), WorldID: uuid.New()}, Bands: bands},
		fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}}, fakeChampion{}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if !reflect.DeepEqual(a.Entrants, []Entrant{
		{clubs[0], OriginPosition, 1}, {clubs[1], OriginPosition, 2}, {clubs[2], OriginPosition, 3},
	}) {
		t.Fatalf("entrants = %+v", a.Entrants)
	}
}

func TestQualifyFieldChampionInsideBand(t *testing.T) {
	ctx := context.Background()
	l1, l2 := qualLeague('1'), qualLeague('2')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3], clubs[4], clubs[5])
	table2 := leagueTable(l2, 1, clubs[6], clubs[7])

	cases := []struct {
		name string
		band Band
		want int
	}{
		{"band 1..1", Band{LeagueID: l1, From: 1, To: ptr(1)}, 2},
		{"band 1..2", Band{LeagueID: l1, From: 1, To: ptr(2)}, 3},
		{"band 1..4 (field 5)", Band{LeagueID: l1, From: 1, To: ptr(4)}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reigning := &Reigning{ClubID: clubs[0], LeagueID: l1} // champion won (rank 1)
			field, err := ComputeField(ctx, QualifyField{
				Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
				Bands: []Band{tc.band},
			}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1, l2: table2}},
				fakeChampion{reigning: reigning}, fakeSiblings{})
			if err != nil {
				t.Fatalf("compute: %v", err)
			}
			if field.ClubCount != tc.want {
				t.Fatalf("club count = %d, want %d (band + champion +1)", field.ClubCount, tc.want)
			}
		})
	}

	// The champion won rank 1 inside band 1..1: the +1 must be the next-best
	// club (rank 2), with the champion itself entering on position.
	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(1)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{reigning: &Reigning{ClubID: clubs[0], LeagueID: l1}}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.Entrants[0].ClubID != clubs[0] || field.Entrants[0].Origin != OriginPosition {
		t.Fatalf("first entrant = %+v, want champion on position", field.Entrants[0])
	}
	if field.Entrants[1].ClubID != clubs[1] || field.Entrants[1].Origin != OriginChampionNextBest || field.Entrants[1].Rank != 2 {
		t.Fatalf("second entrant = %+v, want next-best rank 2", field.Entrants[1])
	}
}

func TestQualifyFieldChampionThirdInFourBandTakesNextBest(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3], clubs[4], clubs[5], clubs[6], clubs[7])

	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(4)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{reigning: &Reigning{ClubID: clubs[2], LeagueID: l1}}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != 5 {
		t.Fatalf("club count = %d, want 5 (band 1..4 + next-best)", field.ClubCount)
	}
	want := []uuid.UUID{clubs[0], clubs[1], clubs[2], clubs[3], clubs[4]}
	for i, e := range field.Entrants {
		if e.ClubID != want[i] {
			t.Fatalf("entrant %d = %s, want %s", i, e.ClubID, want[i])
		}
		if i < 4 && e.Origin != OriginPosition {
			t.Fatalf("entrant %d origin = %s, want position", i, e.Origin)
		}
	}
	last := field.Entrants[4]
	if last.Origin != OriginChampionNextBest || last.Rank != 5 {
		t.Fatalf("5th entrant = %+v, want next-best rank 5", last)
	}
}

func TestQualifyFieldChampionOutsideBand(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs...)

	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(4)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{reigning: &Reigning{ClubID: clubs[6], LeagueID: l1}}, fakeSiblings{}) // finished 7th
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != 5 {
		t.Fatalf("club count = %d, want 5 (band 1..4 + champion out-of-band)", field.ClubCount)
	}
	last := field.Entrants[4]
	if last.ClubID != clubs[6] || last.Origin != OriginChampionOutOfBand || last.Rank != 7 {
		t.Fatalf("champion entrant = %+v, want out-of-band rank 7", last)
	}
}

func TestQualifyFieldChampionDirectNoBandRow(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3], clubs[4], clubs[5], clubs[6], clubs[7])

	// The champion belongs to league l9 which has NO band row; its country's
	// cup band is on l1 only. The champion must enter directly.
	foreignLeague := qualLeague('9')
	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(4)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{reigning: &Reigning{ClubID: clubs[7], LeagueID: foreignLeague}}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != 5 {
		t.Fatalf("club count = %d, want 5 (band + champion_direct)", field.ClubCount)
	}
	direct := field.Entrants[4]
	if direct.ClubID != clubs[7] || direct.Origin != OriginChampionDirect || direct.Rank != 0 {
		t.Fatalf("direct champion = %+v", direct)
	}
}

func TestQualifyFieldChampionInFullTableBandAddsNothing(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(6)
	table1 := leagueTable(l1, 1, clubs...)

	for _, champ := range clubs {
		field, err := ComputeField(ctx, QualifyField{
			Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
			Bands: []Band{{LeagueID: l1, From: 1, To: nil}}, // whole table
		}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
			fakeChampion{reigning: &Reigning{ClubID: champ, LeagueID: l1}}, fakeSiblings{})
		if err != nil {
			t.Fatalf("compute: %v", err)
		}
		if field.ClubCount != 6 {
			t.Fatalf("champ %s: club count = %d, want 6 (no next-best beyond full table)", champ, field.ClubCount)
		}
		for _, e := range field.Entrants {
			if e.Origin == OriginChampionNextBest {
				t.Fatalf("champ %s: unexpected next-best %+v", champ, e)
			}
		}
	}
}

func TestQualifyFieldNextBestSkipsAlreadyEntered(t *testing.T) {
	ctx := context.Background()
	l1, l2 := qualLeague('1'), qualLeague('2')
	clubs := clubIDs(8)
	// Defensive cross-league duplicate (schema forbids it via uq_club_one_league,
	// but the guard is spec'd): club C is already in the field through l2's band,
	// so the champion's next-best must skip it and take rank 4 of l1.
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3])
	table2 := leagueTable(l2, 1, clubs[2], clubs[4])

	field, err := ComputeField(ctx, QualifyField{
		Cup: CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{
			{LeagueID: l1, From: 1, To: ptr(2)},
			{LeagueID: l2, From: 1, To: ptr(1)},
		},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1, l2: table2}},
		fakeChampion{reigning: &Reigning{ClubID: clubs[0], LeagueID: l1}}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if field.ClubCount != 4 {
		t.Fatalf("club count = %d, want 4", field.ClubCount)
	}
	// The next-best (rank 3 of l1 == clubs[2]) is already entered by l2's band.
	var nextBest *Entrant
	for i := range field.Entrants {
		if field.Entrants[i].Origin == OriginChampionNextBest {
			nextBest = &field.Entrants[i]
		}
	}
	if nextBest == nil {
		t.Fatal("no next-best entrant")
	}
	if nextBest.ClubID != clubs[3] {
		t.Fatalf("next-best = %s, want %s (rank 4, skipping entered rank 3)", nextBest.ClubID, clubs[3])
	}
}

func TestQualifyFieldEmptyFieldError(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(6)
	table1 := leagueTable(l1, 1, clubs...)

	// Band beyond the table length → empty cut → field < 2.
	_, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New()},
		Bands: []Band{{LeagueID: l1, From: 9, To: ptr(10)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}}, fakeChampion{}, fakeSiblings{})
	if !errors.Is(err, ErrQualificationField) {
		t.Fatalf("err = %v, want ErrQualificationField", err)
	}

	// No completed season at all → nothing enters.
	_, err = ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New()},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(2)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{}}, fakeChampion{}, fakeSiblings{})
	if !errors.Is(err, ErrQualificationField) {
		t.Fatalf("err = %v, want ErrQualificationField", err)
	}
}

func TestQualifyBandValidation(t *testing.T) {
	cases := []struct {
		name string
		b    Band
	}{
		{"from zero", Band{LeagueID: uuid.New(), From: 0, To: ptr(1)}},
		{"to before from", Band{LeagueID: uuid.New(), From: 4, To: ptr(3)}},
		{"no league", Band{From: 1, To: ptr(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateQualBand(tc.b); err == nil {
				t.Fatalf("band %+v: want validation error", tc.b)
			}
		})
	}
	if err := validateQualBand(Band{LeagueID: uuid.New(), From: 1, To: nil}); err != nil {
		t.Fatalf("valid open band rejected: %v", err)
	}
}

func TestQualifyFieldUnavailableLeagues(t *testing.T) {
	ctx := context.Background()
	l1, l2 := qualLeague('1'), qualLeague('2')
	clubs := clubIDs(8)
	table2 := leagueTable(l2, 1, clubs[6], clubs[7])

	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(4)}, {LeagueID: l2, From: 1, To: ptr(2)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l2: table2}}, fakeChampion{}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(field.Unavailable) != 1 || field.Unavailable[0].LeagueID != l1 {
		t.Fatalf("unavailable = %+v, want exactly l1", field.Unavailable)
	}
	if field.ClubCount != 2 {
		t.Fatalf("club count = %d, want 2 (only l2 banded entrants)", field.ClubCount)
	}
}

func TestQualifyFieldChampionLeagueUnavailableEntersDirect(t *testing.T) {
	ctx := context.Background()
	l1, l2 := qualLeague('1'), qualLeague('2')
	clubs := clubIDs(8)
	table2 := leagueTable(l2, 1, clubs[2], clubs[3])

	// The champion's league l1 HAS a band row but no completed season: the
	// league is flagged unavailable and the champion still enters directly.
	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(1)}, {LeagueID: l2, From: 1, To: ptr(2)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l2: table2}},
		fakeChampion{reigning: &Reigning{ClubID: clubs[0], LeagueID: l1}}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(field.Unavailable) != 1 || field.Unavailable[0].LeagueID != l1 {
		t.Fatalf("unavailable = %+v, want l1", field.Unavailable)
	}
	var direct *Entrant
	for i := range field.Entrants {
		if field.Entrants[i].Origin == OriginChampionDirect {
			direct = &field.Entrants[i]
		}
	}
	if direct == nil || direct.ClubID != clubs[0] {
		t.Fatalf("champion direct entrant missing: %+v", field.Entrants)
	}
}

func TestQualifyFieldChampionMissingFromTable(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3])

	// Defensive: the champion belongs to a banded league yet does not appear in
	// its completed table — the entitlement is preserved as out-of-band.
	notInTable := clubs[7]
	field, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Scope: ScopeRegion},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(4)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{reigning: &Reigning{ClubID: notInTable, LeagueID: l1}}, fakeSiblings{})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	last := field.Entrants[field.ClubCount-1]
	if last.ClubID != notInTable || last.Origin != OriginChampionOutOfBand {
		t.Fatalf("champion entrant = %+v, want out-of-band", last)
	}
}

func TestQualifyFieldConflicts(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3], clubs[4])

	mainCup := CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Tier: 1, Scope: ScopeRegion}
	sibA := CompetitionRef{ID: uuid.New(), WorldID: mainCup.WorldID, Tier: 2, Scope: ScopeRegion}
	sibB := CompetitionRef{ID: uuid.New(), WorldID: mainCup.WorldID, Tier: 1, Scope: ScopeRegion}

	field, err := ComputeField(ctx, QualifyField{
		Cup:   mainCup,
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(5)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{},
		fakeSiblings{siblings: []SiblingCup{
			{Ref: sibA, Bands: []Band{{LeagueID: l1, From: 1, To: ptr(2)}}},
			{Ref: sibB, Bands: []Band{{LeagueID: l1, From: 1, To: ptr(1)}}},
		}})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	// clubs[0] is in sibA's (1..2) and sibB's (1..1) fields; clubs[1] only in
	// sibA's; clubs[2] in neither.
	if len(field.Conflicts[clubs[0]]) != 2 {
		t.Fatalf("conflicts for club[0] = %+v, want both siblings", field.Conflicts[clubs[0]])
	}
	for _, exp := range []Conflict{{sibA.ID, sibA.Tier}, {sibB.ID, sibB.Tier}} {
		found := false
		for _, c := range field.Conflicts[clubs[0]] {
			if c == exp {
				found = true
			}
		}
		if !found {
			t.Fatalf("conflicts for club[0] = %+v, missing %+v", field.Conflicts[clubs[0]], exp)
		}
	}
	if len(field.Conflicts[clubs[1]]) != 1 || field.Conflicts[clubs[1]][0].OtherCupID != sibA.ID {
		t.Fatalf("conflicts for club[1] = %+v, want sibA only", field.Conflicts[clubs[1]])
	}
	if _, ok := field.Conflicts[clubs[2]]; ok {
		t.Fatal("club[2] must not conflict with any sibling")
	}
}

func TestQualifyFieldSiblingChampionAffectsOverlap(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1], clubs[2], clubs[3], clubs[4])

	mainCup := CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Tier: 1, Scope: ScopeRegion}
	sib := CompetitionRef{ID: uuid.New(), WorldID: mainCup.WorldID, Tier: 4, Scope: ScopeRegion}

	// The sibling's field is band 1..1 PLUS its direct champion (clubs[2], via
	// its own league — a club that is already inside the main field's band). So
	// clubs[0] and clubs[2] both overlap the main field; without the sibling
	// champion the overlap would only be clubs[0].
	field, err := ComputeField(ctx, QualifyField{
		Cup:   mainCup,
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(3)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}},
		fakeChampion{byCup: map[uuid.UUID]*Reigning{
			sib.ID: {ClubID: clubs[2], LeagueID: qualLeague('9')},
		}},
		fakeSiblings{siblings: []SiblingCup{
			{Ref: sib, Bands: []Band{{LeagueID: l1, From: 1, To: ptr(1)}}},
		}})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(field.Conflicts[clubs[0]]) != 1 || field.Conflicts[clubs[0]][0].OtherCupID != sib.ID {
		t.Fatalf("conflict of club[0] = %+v, want sibling", field.Conflicts[clubs[0]])
	}
	if len(field.Conflicts[clubs[2]]) != 1 {
		t.Fatalf("conflict of sibling's direct champion = %+v, want one", field.Conflicts[clubs[2]])
	}
	if _, ok := field.Conflicts[clubs[1]]; ok {
		t.Fatal("club[1] is in the main field only, and not in the sibling field")
	}
}

func TestQualifyFieldDisjointSiblingsNoConflicts(t *testing.T) {
	ctx := context.Background()
	l1, l2 := qualLeague('1'), qualLeague('2')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs[0], clubs[1])
	table2 := leagueTable(l2, 1, clubs[6], clubs[7])

	mainCup := CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Tier: 1, Scope: ScopeRegion}
	sib := CompetitionRef{ID: uuid.New(), WorldID: mainCup.WorldID, Tier: 1, Scope: ScopeRegion}

	field, err := ComputeField(ctx, QualifyField{
		Cup:   mainCup,
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(2)}},
	}, fakeStandings{tables: map[uuid.UUID]*Table{l1: table1, l2: table2}},
		fakeChampion{},
		fakeSiblings{siblings: []SiblingCup{
			{Ref: sib, Bands: []Band{{LeagueID: l2, From: 1, To: ptr(2)}}},
		}})
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if len(field.Conflicts) != 0 {
		t.Fatalf("conflicts = %+v, want none (disjoint leagues)", field.Conflicts)
	}
}

func TestQualifyFieldDeterminism(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	clubs := clubIDs(8)
	table1 := leagueTable(l1, 1, clubs...)

	mainCup := CompetitionRef{ID: uuid.New(), WorldID: uuid.New(), Tier: 2, Scope: ScopeRegion}
	sib := CompetitionRef{ID: uuid.New(), WorldID: mainCup.WorldID, Tier: 1, Scope: ScopeRegion}

	in := QualifyField{
		Cup:   mainCup,
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(4)}},
	}
	st := fakeStandings{tables: map[uuid.UUID]*Table{l1: table1}}
	rp := fakeChampion{reigning: &Reigning{ClubID: clubs[2], LeagueID: l1}}
	cp := fakeSiblings{siblings: []SiblingCup{
		{Ref: sib, Bands: []Band{{LeagueID: l1, From: 1, To: ptr(2)}}},
	}}

	a, err := ComputeField(ctx, in, st, rp, cp)
	if err != nil {
		t.Fatalf("first compute: %v", err)
	}
	b, err := ComputeField(ctx, in, st, rp, cp)
	if err != nil {
		t.Fatalf("second compute: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("identical inputs must produce identical fields")
	}
}

func TestQualifyFieldProviderErrorPropagates(t *testing.T) {
	ctx := context.Background()
	l1 := qualLeague('1')
	sentinel := errors.New("standings down")

	_, err := ComputeField(ctx, QualifyField{
		Cup:   CompetitionRef{ID: uuid.New(), WorldID: uuid.New()},
		Bands: []Band{{LeagueID: l1, From: 1, To: ptr(1)}},
	}, errorStandings{err: sentinel}, fakeChampion{}, fakeSiblings{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the standings-provider error", err)
	}
}

type errorStandings struct{ err error }

func (e errorStandings) LastCompletedStandings(_ context.Context, _ uuid.UUID) (*Table, error) {
	return nil, e.err
}
