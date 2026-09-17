// Package development is the pure S08-02 weekly player-development engine. It
// decides, for one player and one week, how much faster or slower the club's
// training plan moves each attribute key, whether the hidden potential ceiling
// flexes, when it locks, and why (an auditable explanation per input). The
// engine holds no database access: the training sweep supplies the inputs via
// Input and applies the resulting multipliers through its single
// player_attributes writer. All numbers are proposal data from
// docs/design/development-numerics.md until PM sign-off.
package development

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/explanation"
	"github.com/touchline/backend/pkg/playergen"
)

// ExpansionBudget is the lifetime cap on potential flex expansions a player
// can consume (data, not logic).
const ExpansionBudget = 3

// LockAge is the age at which a player's potential ceiling stops flexing.
const LockAge = 27

// StagnatingThreshold is the consecutive-week count from which a player is
// reported (and multiplied) as stagnating.
const StagnatingThreshold = 4

// Input is one player's weekly development context, read by the training sweep.
type Input struct {
	PlayerID            uuid.UUID
	Age                 int
	Position            string // primary position ("GK", "ST", ...)
	FacilityLevel       int    // composite academy/training-ground level 1..10
	Skills              map[string]int
	Potential           int // hidden ceiling 1..100; 0 = no ceiling data on disk
	Locked              bool
	ExpansionsLeft      int
	Professionalism     int // 1..100; 0 treated as 50
	PlayingTimePct      float64
	SeasonAvgRating     float64 // 0 when unanswered
	ConsecutiveStagnant int
	WeekTick            int64
}

// State is the next player.player_development row after this week.
type State struct {
	ExpansionsLeft      int
	ConsecutiveStagnant int
	LastEvaluatedWeek   int64
	Locked              bool
	LockedWeek          int64 // 0 = not locked
}

// Outcome is what the sweep persists: per-key growth multipliers, any
// potential change/lock, and the auditable explanation.
type Outcome struct {
	mult             func(key string) float64
	Potential        int
	PotentialChanged bool
	Locked           bool
	LockedWeek       int64
	State            State
	Stagnating       bool
	Explanation      *explanation.Explanation
}

// Multiplier returns the composed weekly growth factor for an attribute key
// (1.0 = plan deltas unchanged). The sweep applies it only to positive deltas;
// decay stays ×1.0.
func (o Outcome) Multiplier(key string) float64 {
	if o.mult == nil {
		return 1.0
	}
	return o.mult(key)
}

// Evaluate runs the weekly development pass for one player. It is pure and
// deterministic: the same Input always yields the same Outcome, so a
// redelivered weekly tick cannot drift.
func Evaluate(in Input) Outcome {
	if in.Age <= 0 {
		in.Age = 25
	}
	if in.Professionalism <= 0 {
		in.Professionalism = 50
	}
	clampInts(&in.Potential, &in.ExpansionsLeft, &in.FacilityLevel)
	if in.PlayingTimePct < 0 {
		in.PlayingTimePct = 0
	}
	if in.PlayingTimePct > 1 {
		in.PlayingTimePct = 1
	}

	stagnant := in.PlayingTimePct < 0.20
	consec := in.ConsecutiveStagnant
	if stagnant {
		consec++
	} else {
		consec = 0
	}
	if consec > 12 {
		consec = 12
	}

	overall := Overall(in.Skills, in.Position)
	headroom := float64(0)
	if in.Potential > 0 {
		headroom = float64(in.Potential) - overall
	}

	potential := in.Potential
	changed := false
	locked := in.Locked
	lockedWeek := int64(0)

	// Potential flex: only a young elite performer who has reached their
	// ceiling unlocks hidden headroom. Budget exhausted or age past the flex
	// window locks the ceiling permanently.
	if in.Potential > 0 && !in.Locked {
		if in.Age <= 26 && headroom <= 2 && in.ExpansionsLeft > 0 &&
			in.SeasonAvgRating >= 7.0 && in.PlayingTimePct >= 0.4 {
			bump := 1
			if in.SeasonAvgRating >= 9.0 {
				bump = 2
			}
			if in.Age <= 20 {
				bump++
			}
			next := in.Potential + bump
			if next > 100 {
				next = 100
			}
			if next > potential {
				potential = next
				changed = true
				if in.ExpansionsLeft-1 == 0 {
					locked = true
					lockedWeek = in.WeekTick
				}
			}
		}
	}
	if in.Age > 26 && !locked {
		locked = true
		lockedWeek = in.WeekTick
	}
	if !locked {
		lockedWeek = 0
	}

	fac := 0.80 + 0.04*float64(in.FacilityLevel)
	mult := func(key string) float64 {
		return ageFactor(in.Age, key) * minutesFactor(in.PlayingTimePct, consec) *
			disciplineFactor(in.Professionalism) * fac * potentialFactor(potential, overall, changed)
	}

	return Outcome{
		mult:             mult,
		Potential:        potential,
		PotentialChanged: changed,
		Locked:           locked,
		LockedWeek:       lockedWeek,
		State: State{
			ExpansionsLeft:      remaining(in.ExpansionsLeft, changed),
			ConsecutiveStagnant: consec,
			LastEvaluatedWeek:   in.WeekTick,
			Locked:              locked,
			LockedWeek:          lockedWeek,
		},
		Stagnating:  consec >= StagnatingThreshold,
		Explanation: explain(in, overall, potential, changed, locked, consec, stagnant, fac),
	}
}

// remaining is the expansion budget that carries into the stored state.
func remaining(expansionsLeft int, expanded bool) int {
	if expanded {
		return expansionsLeft - 1
	}
	return expansionsLeft
}

// clampInts normalises potential/expansions/facility inputs to valid ranges.
func clampInts(potential, expansions, facility *int) {
	if *potential > 100 {
		*potential = 100
	}
	if *potential < 0 {
		*potential = 0
	}
	if *expansions < 0 {
		*expansions = 0
	}
	if *expansions > ExpansionBudget {
		*expansions = ExpansionBudget
	}
	if *facility < 1 {
		*facility = 5
	}
	if *facility > 10 {
		*facility = 10
	}
}

// ageFactor is the age-curve multiplier per key category: youth grow faster,
// veterans preserve mental attributes while their physical/technical ceiling
// stops rising (their physical coming-down is the training pass's veteran
// decay).
func ageFactor(age int, key string) float64 {
	if playergen.CategoryForKey(key) == "mental" {
		switch {
		case age >= 16 && age <= 20:
			return 1.8
		case age >= 30:
			return 1.15
		default:
			return 1.0
		}
	}
	switch {
	case age >= 16 && age <= 20:
		return 1.6
	case age >= 30:
		return 0.55
	default:
		return 1.0
	}
}

// minutesFactor rewards playing time and atrophies the neglected: a player
// below 20% season share for four or more consecutive weeks is "stagnating"
// and each key's growth drops accordingly.
func minutesFactor(pct float64, consecStagnant int) float64 {
	m := 0.5 + 1.0*pct
	if consecStagnant >= StagnatingThreshold {
		m *= 0.85
	}
	return m
}

// disciplineFactor is professionalism: the professional train reliably, the
// undisciplined drift.
func disciplineFactor(pro int) float64 {
	return 0.75 + 0.55*(float64(pro)/100)
}

// facilityFactor is the academy/training-ground level: 5 is neutral.
func facilityFactor(level int) float64 {
	return 0.80 + 0.04*float64(level)
}

// potentialFactor throttles growth as the player approaches their hidden
// ceiling: headroom of 2 points or less leaves a trickle unless the ceiling
// just flexed this week (a fresh headroom window opens). No ceiling data
// (Potential 0) → no throttle.
func potentialFactor(potential int, overall float64, expanded bool) float64 {
	if potential <= 0 {
		return 1.0
	}
	if expanded {
		return 1.2
	}
	if float64(potential)-overall <= 2 {
		return 0.25
	}
	return 1.0
}

// Overall is the blended current ability (weighted category means,
// renormalised over the categories the player actually has). This is the value
// compared to the hidden potential ceiling.
func Overall(skills map[string]int, position string) float64 {
	weights := fieldWeights
	if position == "GK" {
		weights = gkWeights
	}
	var sum, total float64
	for cat, w := range weights {
		keys := playergen.KeysForCategory(cat)
		var s, n float64
		for _, k := range keys {
			if v, ok := skills[k]; ok {
				s += float64(v)
				n++
			}
		}
		if n == 0 {
			continue
		}
		sum += s / n * w
		total += w
	}
	if total == 0 {
		return 0
	}
	return sum / total
}

// fieldWeights / gkWeights are the overall blend per position group (data).
var (
	fieldWeights = map[string]float64{
		"technical": 0.35, "physical": 0.20, "mental": 0.25,
		"tactical": 0.10, "positional": 0.10,
	}
	gkWeights = map[string]float64{
		"goalkeeping": 0.55, "mental": 0.20, "physical": 0.10,
		"tactical": 0.10, "positional": 0.05,
	}
)

// explain builds the auditable §54 explanation for the week. The score stays 0
// (narrative): the factors carry the actual multipliers and decisions; the
// sweep's attribute writes are separately recorded in player_attribute_changes.
func explain(in Input, overall float64, potential int, expanded, locked bool, consec int, stagnant bool, fac float64) *explanation.Explanation {
	e := explanation.New("player_development", 0)

	switch {
	case in.Age >= 30:
		e.Add("Age 30+ veteran curve", 0)
	case in.Age >= 21:
		e.Add("Prime age band (x1.0)", 0)
	default:
		e.Add("Youth growth band (16-20)", 0)
	}

	if in.PlayingTimePct > 0 {
		e.Add(fmt.Sprintf("Minutes share %d%%", int(in.PlayingTimePct*100)), 0)
	}
	if stagnant && consec >= 4 {
		e.Add("Stagnating: minutes under 20% for consecutive weeks (x0.85)", 0)
	}
	e.Add(fmt.Sprintf("Academy level %d (x%.2f)", in.FacilityLevel, fac), 0)
	e.Add(fmt.Sprintf("Professionalism %d", in.Professionalism), 0)

	if in.Potential > 0 {
		e.Add(fmt.Sprintf("Potential %d vs overall %.0f (headroom %.0f pts)", potential, overall, float64(potential)-overall), 0)
	}
	if expanded {
		e.Add(fmt.Sprintf("Potential expanded to %d (elite form)", potential), 0)
	}
	if locked {
		e.Add("Potential ceiling locked", 0)
	}
	return e
}
