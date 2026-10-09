package competition

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// Outlook is GET /api/managers/me/competitions/:id/outlook: what is at stake
// for the manager's club in a league (IM56) and where it is projected to
// finish (IM57). Every "clinched" claim is mathematically proven; see
// finishRange.
type Outlook struct {
	Season    apiref.SeasonRef `json:"season"`
	Club      apiref.ClubRef   `json:"club"`
	Position  int              `json:"position"`
	Points    int              `json:"points"`
	GamesLeft int              `json:"games_left"`
	// Finish is the guaranteed range of final positions still possible.
	Finish FinishRange `json:"finish"`
	// Stakes is the single most relevant race (title, then promotion or
	// relegation, then cup qualification); nil when nothing is at stake.
	Stakes *Race `json:"stakes"`
	// Races lists every league race that exists in this league.
	Races []Race `json:"races"`
	// Attachments lists every competition the league feeds, every scope.
	Attachments []Race `json:"attachments"`
	// GuaranteedAtLeast names the worst-case cup when every still-possible
	// finish qualifies for something.
	GuaranteedAtLeast *CupRef         `json:"guaranteed_at_least"`
	NextMatch         *NextMatchSwing `json:"next_match"`
	Projection        Projection      `json:"projection"`
}

// FinishRange is the best and worst final position still mathematically
// possible.
type FinishRange struct {
	Best  int `json:"best"`
	Worst int `json:"worst"`
}

// Race kinds and statuses.
const (
	RaceTitle         = "title"
	RacePromotion     = "promotion"
	RaceRelegation    = "relegation"
	RaceQualification = "qualification"

	RaceClinched   = "clinched"
	RaceAlive      = "alive"
	RaceEliminated = "eliminated"
)

// CupRef identifies a competition a league feeds, with its scope
// (competition_type: domestic_cup, regional, continental, international…).
type CupRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Scope string    `json:"scope"`
}

// Race is one position band and our status in it. Gap is the points distance
// to the band edge nearest us: points behind it when below, our cushion when
// inside (for relegation: points to safety) or above.
type Race struct {
	Kind   string  `json:"kind"`
	Cup    *CupRef `json:"cup,omitempty"`
	From   int     `json:"from_position"`
	To     int     `json:"to_position"`
	Status string  `json:"status"`
	Inside bool    `json:"inside"`
	Gap    int     `json:"gap"`
}

// NextMatchSwing is our table position after each result of our next league
// fixture, holding every other club's points where they are.
type NextMatchSwing struct {
	Fixture Fixture `json:"fixture"`
	IfWin   int     `json:"if_win"`
	IfDraw  int     `json:"if_draw"`
	IfLoss  int     `json:"if_loss"`
}

// Projection is the Monte Carlo finish estimate and the factors behind it.
type Projection struct {
	Position int                `json:"position"`
	Range    [2]int             `json:"range"`
	Runs     int                `json:"runs"`
	Factors  []DifficultyFactor `json:"factors"`
}

// olClub is one table line for the outlook maths, in table order.
type olClub struct {
	id     uuid.UUID
	name   string
	pts    int
	gf, ga int
	left   int // league games still to play
}

func (c olClub) max() int { return c.pts + 3*c.left }

// finishRange returns the best and worst final position for table[us].
// A rival counts as able to finish above us when it can reach our current
// points: equal points count against us, because goal difference and goals
// can still change. A rival is guaranteed above only when its points already
// exceed our maximum. When no league games remain the table is final.
func finishRange(table []olClub, us int) FinishRange {
	done := true
	for _, c := range table {
		if c.left > 0 {
			done = false
			break
		}
	}
	if done {
		return FinishRange{Best: us + 1, Worst: us + 1}
	}
	r := FinishRange{Best: 1, Worst: 1}
	for i, c := range table {
		if i == us {
			continue
		}
		if c.pts > table[us].max() {
			r.Best++
		}
		if c.max() >= table[us].pts {
			r.Worst++
		}
	}
	return r
}

// raceStatus classifies positions [from, to] against a finish range.
func raceStatus(f FinishRange, from, to int) string {
	switch {
	case from <= f.Best && f.Worst <= to:
		return RaceClinched
	case f.Worst < from || f.Best > to:
		return RaceEliminated
	default:
		return RaceAlive
	}
}

// buildRace fills a race's status, inside flag and gap for table[us].
func buildRace(kind string, table []olClub, us int, f FinishRange, from, to int) Race {
	pos := us + 1
	r := Race{Kind: kind, From: from, To: to, Status: raceStatus(f, from, to), Inside: pos >= from && pos <= to}
	ptsAt := func(p int) int { return table[p-1].pts }
	ours := table[us].pts
	switch {
	case r.Inside && kind == RaceRelegation:
		if from > 1 {
			r.Gap = ptsAt(from-1) - ours
		}
	case r.Inside:
		if to < len(table) {
			r.Gap = ours - ptsAt(to+1)
		}
	case pos > to:
		r.Gap = ptsAt(to) - ours
	default: // above the band
		r.Gap = ours - ptsAt(from)
	}
	return r
}

// leagueRaces builds the races that exist in this league: the title always,
// promotion and relegation only when the league has those slots.
func leagueRaces(table []olClub, us int, f FinishRange, promotions, relegations int) []Race {
	n := len(table)
	races := []Race{buildRace(RaceTitle, table, us, f, 1, 1)}
	if promotions > 0 && promotions < n {
		races = append(races, buildRace(RacePromotion, table, us, f, 1, promotions))
	}
	if relegations > 0 && relegations < n {
		races = append(races, buildRace(RaceRelegation, table, us, f, n-relegations+1, n))
	}
	return races
}

// pickStakes chooses the card's race: title, then promotion, then
// relegation, then the nearest live cup band. A race is relevant unless it
// is settled against us (eliminated); for relegation "eliminated" means safe.
func pickStakes(races, attachments []Race, pos int) *Race {
	for _, kind := range []string{RaceTitle, RacePromotion, RaceRelegation} {
		for i := range races {
			if races[i].Kind == kind && races[i].Status != RaceEliminated {
				r := races[i]
				return &r
			}
		}
	}
	var best *Race
	bestDist := math.MaxInt
	for i := range attachments {
		a := attachments[i]
		if a.Status == RaceEliminated {
			continue
		}
		dist := 0
		if pos < a.From {
			dist = a.From - pos
		} else if pos > a.To {
			dist = pos - a.To
		}
		if dist < bestDist || (dist == bestDist && a.From < best.From) {
			best, bestDist = &a, dist
		}
	}
	return best
}

// guaranteedAtLeast returns the cup our worst possible finish qualifies for,
// but only when every position in the finish range is covered by some band.
func guaranteedAtLeast(attachments []Race, f FinishRange) *CupRef {
	covering := func(p int) *Race {
		for i := range attachments {
			if p >= attachments[i].From && p <= attachments[i].To {
				return &attachments[i]
			}
		}
		return nil
	}
	for p := f.Best; p <= f.Worst; p++ {
		if covering(p) == nil {
			return nil
		}
	}
	return covering(f.Worst).Cup
}

// tableLess is the standings order: points, goal difference, goals, name.
func tableLess(a, b olClub) bool {
	if a.pts != b.pts {
		return a.pts > b.pts
	}
	if gd1, gd2 := a.gf-a.ga, b.gf-b.ga; gd1 != gd2 {
		return gd1 > gd2
	}
	if a.gf != b.gf {
		return a.gf > b.gf
	}
	return a.name < b.name
}

// swingPositions re-sorts the table after our next fixture against opp,
// assuming the narrowest scoreline (1-0, 0-0, 0-1) and every other club
// standing still. opp < 0 means the opponent is not in this table.
func swingPositions(table []olClub, us, opp int) (win, draw, loss int) {
	at := func(ourPts, oppPts, ourGF, oppGF int) int {
		t := append([]olClub(nil), table...)
		t[us].pts += ourPts
		t[us].gf += ourGF
		t[us].ga += oppGF
		if opp >= 0 {
			t[opp].pts += oppPts
			t[opp].gf += oppGF
			t[opp].ga += ourGF
		}
		pos := 1
		for i := range t {
			if i != us && tableLess(t[i], t[us]) {
				pos++
			}
		}
		return pos
	}
	return at(3, 0, 1, 0), at(1, 1, 0, 0), at(0, 3, 0, 1)
}

// Projection tuning (IM57, product-owned).
const (
	projectionRuns = 1000
	// projectionDraw is the per-fixture draw probability.
	projectionDraw = 0.26
	// projectionShrink pulls small samples toward the league average: a
	// club's rate behaves as if it had this many extra average games.
	projectionShrink = 3.0
	// projectionDefaultPPG is the league points-per-game before any result.
	projectionDefaultPPG = 1.35
)

// olFixture is one unplayed league fixture by table index.
type olFixture struct{ home, away int }

// olRates are a club's points and games at home and away this season.
type olRates struct{ homePts, homeG, awayPts, awayG int }

// projectFinish runs projectionRuns seeded simulations of the remaining
// fixtures and returns our median finish and its 10th–90th percentile range.
// Results are drawn from each side's shrunk home/away points-per-game; the
// final order breaks points ties on the current table order.
func projectFinish(table []olClub, us int, remaining []olFixture, rates []olRates, seed1, seed2 uint64) (int, [2]int) {
	if len(remaining) == 0 {
		return us + 1, [2]int{us + 1, us + 1}
	}
	totalPts, totalG := 0, 0
	for _, r := range rates {
		totalPts += r.homePts + r.awayPts
		totalG += r.homeG + r.awayG
	}
	avg := projectionDefaultPPG
	if totalG > 0 {
		avg = float64(totalPts) / float64(totalG)
	}
	rate := func(pts, g int) float64 {
		return (float64(pts) + projectionShrink*avg) / (float64(g) + projectionShrink)
	}
	pHome := make([]float64, len(remaining))
	for i, f := range remaining {
		h := rate(rates[f.home].homePts, rates[f.home].homeG)
		a := rate(rates[f.away].awayPts, rates[f.away].awayG)
		pHome[i] = (1 - projectionDraw) * h / (h + a)
	}

	rng := rand.New(rand.NewPCG(seed1, seed2))
	pts := make([]int, len(table))
	positions := make([]int, projectionRuns)
	for run := range positions {
		for i, c := range table {
			pts[i] = c.pts
		}
		for i, f := range remaining {
			u := rng.Float64()
			switch {
			case u < pHome[i]:
				pts[f.home] += 3
			case u < pHome[i]+projectionDraw:
				pts[f.home]++
				pts[f.away]++
			default:
				pts[f.away] += 3
			}
		}
		pos := 1
		for i := range table {
			if i != us && (pts[i] > pts[us] || (pts[i] == pts[us] && i < us)) {
				pos++
			}
		}
		positions[run] = pos
	}
	sort.Ints(positions)
	return positions[projectionRuns/2], [2]int{positions[projectionRuns/10], positions[projectionRuns*9/10-1]}
}

// factorInputs are the season facts behind the projection's "Why" factors.
type factorInputs struct {
	clubs         int // league size
	inTop6        bool
	remaining     int
	remainingTop6 int // remaining games against the current top 6
	homeW, homeD  int
	homeL         int
	leagueHomePPG float64
	xiUnavailable int // of our best XI by rating
	recentGD      int // goal difference over the last recentGames
	recentGames   int
	seasonGDPerG  float64
	recentXGD     float64 // xG for minus against over recentXGGames
	recentXGGames int
	seasonXGDPerG float64
}

// projectionFactors explains the projection. Each delta is signed: positive
// helps our finish. Units are in each detail line.
func projectionFactors(in factorInputs) []DifficultyFactor {
	out := []DifficultyFactor{}
	if in.remaining > 0 && in.clubs > 1 {
		top := 6
		if in.inTop6 {
			top = 5
		}
		expected := float64(in.remaining) * float64(top) / float64(in.clubs-1)
		out = append(out, DifficultyFactor{
			Label:  "Remaining fixtures vs top 6",
			Delta:  int(math.Round(expected - float64(in.remainingTop6))),
			Detail: fmt.Sprintf("%d of %d remaining games are against the top 6 (games vs an even schedule)", in.remainingTop6, in.remaining),
		})
	}
	if g := in.homeW + in.homeD + in.homeL; g > 0 {
		pts := 3*in.homeW + in.homeD
		out = append(out, DifficultyFactor{
			Label:  "Home form",
			Delta:  int(math.Round(float64(pts) - float64(g)*in.leagueHomePPG)),
			Detail: fmt.Sprintf("W%d D%d L%d at home (points vs the league's home average)", in.homeW, in.homeD, in.homeL),
		})
	}
	if in.xiUnavailable > 0 {
		out = append(out, DifficultyFactor{
			Label:  "Injuries and absences",
			Delta:  -in.xiUnavailable,
			Detail: fmt.Sprintf("%d of the best XI unavailable for the next match", in.xiUnavailable),
		})
	}
	if in.recentGames > 0 {
		out = append(out, DifficultyFactor{
			Label:  "Goal difference trend",
			Delta:  int(math.Round(float64(in.recentGD) - in.seasonGDPerG*float64(in.recentGames))),
			Detail: fmt.Sprintf("%+d in the last %d vs %+.1f per game this season (goals)", in.recentGD, in.recentGames, in.seasonGDPerG),
		})
	}
	if in.recentXGGames > 0 {
		out = append(out, DifficultyFactor{
			Label:  "Chance quality trend",
			Delta:  int(math.Round(in.recentXGD - in.seasonXGDPerG*float64(in.recentXGGames))),
			Detail: fmt.Sprintf("xG difference %+.1f in the last %d vs %+.1f per game this season (expected goals)", in.recentXGD, in.recentXGGames, in.seasonXGDPerG),
		})
	}
	return out
}
