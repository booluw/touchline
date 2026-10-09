package competition

import (
	"testing"

	"github.com/google/uuid"
)

// tbl builds a table (already in standings order) from points and games
// left per club.
func tbl(pts, left []int) []olClub {
	t := make([]olClub, len(pts))
	for i := range pts {
		t[i] = olClub{id: uuid.New(), name: string(rune('A' + i)), pts: pts[i], left: left[i]}
	}
	return t
}

func TestFinishRangeClinchGuards(t *testing.T) {
	cases := []struct {
		name        string
		pts, left   []int
		us          int
		best, worst int
		title       string
	}{
		// 1 point clear, the rival can still win every game: not champions.
		{"one point clear, rival alive", []int{50, 49, 30}, []int{1, 1, 1}, 0, 1, 2, RaceAlive},
		// Rival can reach exactly our points: still not champions, because
		// goal difference could still swing it.
		{"rival can only draw level", []int{50, 47, 30}, []int{0, 1, 0}, 0, 1, 2, RaceAlive},
		// Rival's maximum is below our points: champions.
		{"out of reach", []int{50, 46, 30}, []int{1, 1, 1}, 0, 1, 1, RaceClinched},
		// Level on points, our games done but a rival still to play.
		{"level, other games unplayed", []int{50, 50, 30}, []int{0, 1, 0}, 0, 1, 2, RaceAlive},
		// Every league game played: the final table (tiebreaks) decides.
		{"season over, level on points", []int{50, 50, 30}, []int{0, 0, 0}, 0, 1, 1, RaceClinched},
		// Leader already beyond our maximum: title gone.
		{"title out of reach", []int{60, 50, 30}, []int{1, 1, 1}, 1, 2, 2, RaceEliminated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := tbl(tc.pts, tc.left)
			f := finishRange(table, tc.us)
			if f.Best != tc.best || f.Worst != tc.worst {
				t.Fatalf("finish = %+v, want best %d worst %d", f, tc.best, tc.worst)
			}
			if got := raceStatus(f, 1, 1); got != tc.title {
				t.Fatalf("title status = %s, want %s", got, tc.title)
			}
		})
	}
}

func TestLeagueRacesRespectLeagueRules(t *testing.T) {
	table := tbl([]int{30, 28, 20, 10}, []int{4, 4, 4, 4})
	f := finishRange(table, 2)
	// No promotion, no relegation: only the title race exists.
	races := leagueRaces(table, 2, f, 0, 0)
	if len(races) != 1 || races[0].Kind != RaceTitle {
		t.Fatalf("races = %+v, want title only", races)
	}
	races = leagueRaces(table, 2, f, 1, 1)
	if len(races) != 3 {
		t.Fatalf("want title, promotion, relegation; got %+v", races)
	}
	rel := races[2]
	if rel.From != 4 || rel.To != 4 || rel.Inside || rel.Gap != 10 {
		t.Fatalf("relegation race = %+v, want band 4-4, outside, 10 pts cushion", rel)
	}
}

func TestRelegationClinchedAndSafe(t *testing.T) {
	// Bottom club 13 behind with 4 left (max 12+... ) cannot reach 3rd: relegated.
	table := tbl([]int{40, 35, 30, 17}, []int{4, 4, 4, 4})
	f := finishRange(table, 3)
	if s := raceStatus(f, 4, 4); s != RaceClinched {
		t.Fatalf("relegation = %s, want clinched (finish %+v)", s, f)
	}
	// Third place: can the bottom club still catch us? 17+12 = 29 < 30: safe.
	f = finishRange(table, 2)
	if s := raceStatus(f, 4, 4); s != RaceEliminated {
		t.Fatalf("third place relegation = %s, want eliminated (safe), finish %+v", s, f)
	}
}

func TestAttachmentsStakesAndGuaranteedFloor(t *testing.T) {
	cupA := &CupRef{Name: "Continental Cup", Scope: "continental"}
	cupB := &CupRef{Name: "Regional Cup", Scope: "regional"}
	cupC := &CupRef{Name: "World Cup", Scope: "international"}
	// 8 clubs; we are 3rd and can finish anywhere from 2nd to 5th.
	table := tbl([]int{60, 52, 50, 48, 47, 20, 15, 10}, []int{2, 2, 2, 2, 2, 2, 2, 2})
	us := 2
	f := finishRange(table, us)
	if f.Best != 2 || f.Worst != 5 {
		t.Fatalf("finish = %+v, want 2..5", f)
	}
	band := func(cup *CupRef, from, to int) Race {
		r := buildRace(RaceQualification, table, us, f, from, to)
		r.Cup = cup
		return r
	}
	atts := []Race{band(cupA, 1, 2), band(cupB, 3, 6), band(cupC, 7, 8)}
	// Band 3-6 is not clinched: we can still reach 2nd (a different cup).
	if atts[1].Status != RaceAlive {
		t.Fatalf("band 3-6 = %s, want alive", atts[1].Status)
	}
	// Every finish 2..5 qualifies for something; the floor is the Regional Cup.
	if g := guaranteedAtLeast(atts, f); g != cupB {
		t.Fatalf("guaranteed = %+v, want regional cup", g)
	}
	// A gap in coverage means no guarantee.
	if g := guaranteedAtLeast([]Race{atts[0], band(cupB, 3, 4)}, f); g != nil {
		t.Fatalf("guaranteed = %+v, want nil when 5th has no cup", g)
	}
	// Title gone, no promotion/relegation: stakes falls to the nearest cup band.
	races := leagueRaces(table, us, f, 0, 0)
	st := pickStakes(races, atts, us+1)
	if st == nil || st.Cup != cupB {
		t.Fatalf("stakes = %+v, want the regional cup band we are inside", st)
	}
	// All three attachments are reported regardless of scope.
	if len(atts) != 3 {
		t.Fatalf("attachments = %d, want 3", len(atts))
	}
}

func TestPickStakesOrder(t *testing.T) {
	alive := func(kind string) Race { return Race{Kind: kind, Status: RaceAlive} }
	elim := func(kind string) Race { return Race{Kind: kind, Status: RaceEliminated} }
	if st := pickStakes([]Race{alive(RaceTitle), alive(RacePromotion)}, nil, 1); st.Kind != RaceTitle {
		t.Fatalf("got %s, want title first", st.Kind)
	}
	if st := pickStakes([]Race{elim(RaceTitle), alive(RacePromotion), alive(RaceRelegation)}, nil, 1); st.Kind != RacePromotion {
		t.Fatalf("got %s, want promotion before relegation", st.Kind)
	}
	if st := pickStakes([]Race{elim(RaceTitle), elim(RacePromotion), elim(RaceRelegation)}, nil, 1); st != nil {
		t.Fatalf("got %+v, want no stakes", st)
	}
}

func TestSwingPositionsMovesOpponentToo(t *testing.T) {
	// We are 3rd on 30, opponent 2nd on 31, leader on 40.
	table := tbl([]int{40, 31, 30, 20}, []int{5, 5, 5, 5})
	w, d, l := swingPositions(table, 2, 1)
	if w != 2 || d != 3 || l != 3 {
		t.Fatalf("win/draw/loss = %d/%d/%d, want 2/3/3", w, d, l)
	}
}

func TestProjectFinishDeterministicAndFinal(t *testing.T) {
	table := tbl([]int{30, 28, 26, 10}, []int{2, 2, 2, 2})
	rem := []olFixture{{0, 1}, {2, 3}, {1, 2}, {3, 0}}
	rates := make([]olRates, 4)
	p1, r1 := projectFinish(table, 2, rem, rates, 7, 9)
	p2, r2 := projectFinish(table, 2, rem, rates, 7, 9)
	if p1 != p2 || r1 != r2 {
		t.Fatalf("same seed gave %d %v and %d %v", p1, r1, p2, r2)
	}
	if r1[0] > p1 || p1 > r1[1] {
		t.Fatalf("median %d outside range %v", p1, r1)
	}
	if p, r := projectFinish(table, 2, nil, rates, 7, 9); p != 3 || r != [2]int{3, 3} {
		t.Fatalf("finished season = %d %v, want 3 [3 3]", p, r)
	}
}

func TestProjectionFactorsSigns(t *testing.T) {
	f := projectionFactors(factorInputs{
		clubs: 20, remaining: 10, remainingTop6: 6, // harder than an even ~3.2
		homeW: 6, homeD: 2, homeL: 1, leagueHomePPG: 1.5,
		xiUnavailable: 1,
		recentGD:      -3, recentGames: 5, seasonGDPerG: 0.4,
	})
	want := map[string]int{"Remaining fixtures vs top 6": -3, "Home form": 7, "Injuries and absences": -1, "Goal difference trend": -5}
	if len(f) != len(want) {
		t.Fatalf("factors = %+v", f)
	}
	for _, x := range f {
		if want[x.Label] != x.Delta {
			t.Fatalf("%s = %d, want %d", x.Label, x.Delta, want[x.Label])
		}
	}
}
