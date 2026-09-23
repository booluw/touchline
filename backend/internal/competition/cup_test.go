package competition

import (
	"testing"

	"github.com/google/uuid"
)

func TestCupLadder(t *testing.T) {
	cases := []struct {
		name string
		f, x int
		n    int
		want []roundPlan
	}{
		{
			name: "f=x (join at round 1)", f: 5, x: 5, n: 5,
			want: []roundPlan{{Round: 1, N: 10, Ties: 5, Byes: 0}, {Round: 2, N: 5, Ties: 2, Byes: 1}, {Round: 3, N: 3, Ties: 1, Byes: 1}, {Round: 4, N: 2, Ties: 1, Byes: 0}},
		},
		{
			name: "even field halves cleanly", f: 8, x: 4, n: 2,
			want: []roundPlan{
				{Round: 1, N: 8, Ties: 4, Byes: 0},
				{Round: 2, N: 6, Ties: 3, Byes: 0},
				{Round: 3, N: 3, Ties: 1, Byes: 1},
				{Round: 4, N: 2, Ties: 1, Byes: 0},
			},
		},
		{
			name: "partial landing round", f: 6, x: 4, n: 2,
			want: []roundPlan{
				{Round: 1, N: 6, Ties: 2, Byes: 2},
				{Round: 2, N: 6, Ties: 3, Byes: 0},
				{Round: 3, N: 3, Ties: 1, Byes: 1},
				{Round: 4, N: 2, Ties: 1, Byes: 0},
			},
		},
		{
			name: "no late entry", f: 6, x: 3, n: 0,
			want: []roundPlan{
				{Round: 1, N: 6, Ties: 3, Byes: 0},
				{Round: 2, N: 3, Ties: 1, Byes: 1},
				{Round: 3, N: 2, Ties: 1, Byes: 0},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cupLadder(tc.f, tc.x, tc.n)
			if err != nil {
				t.Fatalf("cupLadder(%d,%d,%d) error: %v", tc.f, tc.x, tc.n, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("cupLadder rounds = %d, want %d (%+v)", len(got), len(tc.want), got)
			}
			// Structural invariants across the ladder.
			for i, rp := range got {
				if rp.Round != i+1 {
					t.Errorf("round %d: matchday = %d, want %d", i, rp.Round, i+1)
				}
				if rp.Ties*2+rp.Byes != rp.N {
					t.Errorf("round %d: %d ties + %d byes != %d participants", rp.Round, rp.Ties, rp.Byes, rp.N)
				}
				if rp.Ties < 0 || rp.Byes < 0 {
					t.Errorf("round %d: negative ties/byes %d/%d", rp.Round, rp.Ties, rp.Byes)
				}
				if rp != tc.want[i] {
					t.Errorf("round %d = %+v, want %+v", rp.Round, rp, tc.want[i])
				}
			}
			// First round always holds the whole bottom pool (or the join field
			// when F == X); the last round is exactly two clubs.
			if got[len(got)-1].N != 2 {
				t.Errorf("final round participants = %d, want 2", got[len(got)-1].N)
			}
		})
	}
}

func TestCupLadderInvalid(t *testing.T) {
	for _, tt := range []struct{ f, x, n int }{
		{4, 4, -1}, {4, 0, 2}, {4, 1, 0}, {3, 4, 2},
	} {
		if _, err := cupLadder(tt.f, tt.x, tt.n); err == nil {
			t.Errorf("cupLadder(%d,%d,%d) expected error", tt.f, tt.x, tt.n)
		}
	}
}

func TestDrawRoundDeterministic(t *testing.T) {
	seed := int64(42)
	cupID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		uuid.MustParse("00000000-0000-0000-0000-000000000005"),
		uuid.MustParse("00000000-0000-0000-0000-000000000006"),
		uuid.MustParse("00000000-0000-0000-0000-000000000007"),
		uuid.MustParse("00000000-0000-0000-0000-000000000008"),
		uuid.MustParse("00000000-0000-0000-0000-000000000009"),
		uuid.MustParse("00000000-0000-0000-0000-00000000000a"),
	}
	// Same call twice => identical pairs and byes (replay stability).
	aPairs, aByes := drawRound(seed, cupID, 1, 2, 2, ids)
	bPairs, bByes := drawRound(seed, cupID, 1, 2, 2, ids)
	if len(aPairs) != 2 {
		t.Fatalf("pairs = %d, want 2", len(aPairs))
	}
	if len(aByes) != 2 {
		t.Fatalf("byes = %d, want 2", len(aByes))
	}
	if len(aPairs) != len(bPairs) {
		t.Fatalf("replay pair count differs")
	}
	for i := range aPairs {
		if aPairs[i] != bPairs[i] {
			t.Fatalf("replay pair %d differs: %v vs %v", i, aPairs[i], bPairs[i])
		}
	}
	for i := range aByes {
		if aByes[i] != bByes[i] {
			t.Fatalf("replay bye %d differs: %v vs %v", i, aByes[i], bByes[i])
		}
	}
	// Participants are partitioned exactly: 2 ties (4 clubs) + 2 byes (2 clubs)
	// = the full 6.
	seen := map[uuid.UUID]bool{}
	for _, p := range aPairs {
		seen[p[0]] = true
		seen[p[1]] = true
		if p[0] == p[1] {
			t.Fatalf("self-tie %v", p)
		}
	}
	for _, b := range aByes {
		if seen[b] {
			t.Fatalf("club %v appears in a pair and a bye", b)
		}
		seen[b] = true
	}
	if len(seen) != 6 {
		t.Fatalf("draw covers %d clubs, want 6", len(seen))
	}
	// Different round => different draw from the same field.
	cPairs, _ := drawRound(seed, cupID, 2, 2, 2, ids)
	diff := false
	for i := range cPairs {
		if i < len(aPairs) && cPairs[i] != aPairs[i] {
			diff = true
		}
	}
	if !diff {
		t.Fatalf("round 1 and round 2 produced identical draws")
	}
}

func TestDrawRoundFullField(t *testing.T) {
	seed := int64(7)
	cupID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		uuid.MustParse("00000000-0000-0000-0000-000000000004"),
	}
	// Even field: 2 ties, no byes.
	pairs, byes := drawRound(seed, cupID, 1, 2, 0, ids)
	if len(pairs) != 2 || len(byes) != 0 {
		t.Fatalf("even draw = %d pairs/%d byes, want 2/0", len(pairs), len(byes))
	}
	// Odd field with a single bye.
	ids5 := append(append([]uuid.UUID(nil), ids...), uuid.MustParse("00000000-0000-0000-0000-000000000005"))
	pairs5, byes5 := drawRound(seed, cupID, 1, 2, 1, ids5)
	if len(pairs5) != 2 || len(byes5) != 1 {
		t.Fatalf("odd draw = %d pairs/%d byes, want 2/1", len(pairs5), len(byes5))
	}
}

func TestStagingFromRules(t *testing.T) {
	raw, err := cupQualificationJSON(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	n, x := stagingFromRules(raw)
	if n != 3 || x != 4 {
		t.Fatalf("staging = (%d,%d), want (3,4)", n, x)
	}
	if g, err := cupRulesJSON(nil); err != nil {
		t.Fatal(err)
	} else if string(g) == "" {
		t.Fatal("cup rules default is empty")
	}
}
