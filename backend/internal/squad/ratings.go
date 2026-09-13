// Position-weighted squad rating aggregation (matchsim addendum v1.4
// Part 7/8, + v1.5 style profiles). The per-position Attack/Defense weighting
// formula is PROPOSAL data (PM open item 4, not yet signed off): it is exposed
// as versioned PositionWeights so a recalibration is a data change only.
// Attribute categories mirror the player.player_attributes EAV categories.
package squad

import (
	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/matchsim"
)

// AttributeSnapshot is one player's persisted attribute categories as the
// aggregation layer consumes them ([1,100] each). Loaded by internal/match
// from the player_attributes EAV (categories: technical, physical, mental,
// tactical, goalkeeping, positional).
type AttributeSnapshot struct {
	Technical   int
	Physical    int
	Mental      int
	Tactical    int
	Goalkeeping int
	Positional  int
}

// CategoryWeights maps attribute categories to their contribution to one side
// of a player's rating. A zero CategoryWeights value means that side is
// irrelevant for the position (e.g. goalkeeping weight on a striker's
// attacking output).
type CategoryWeights struct {
	Technical   float64
	Physical    float64
	Mental      float64
	Tactical    float64
	Goalkeeping float64
	Positional  float64
}

// PositionProfile is a position's two-sided rating recipe.
type PositionProfile struct {
	Attack  CategoryWeights
	Defense CategoryWeights
}

// PositionWeights is a complete per-position recipe table.
type PositionWeights map[string]PositionProfile

// ValidPositions is the schema's primary_position enumeration
// (migration 0002_player, player.players.primary_position).
var ValidPositions = []string{
	"GK", "CB", "LB", "RB",
	"DM", "CM", "AM", "LM", "RM",
	"ST", "LW", "RW",
}

// DefaultPositionWeights is the PROPOSAL recipe table (PM open item 4). It is
// seeded, not final: every number here is a candidate for PM recalibration.
// Sanity rules baked in: a GK's attacking side is goalkeeping/mental-led and
// cheap; centre-backs and full-backs weight defence heavily; wide/attacking
// players weight technique and pace; the mental category is the cheapest
// shared floor because personnel rarely penalise from it.
var DefaultPositionWeights = PositionWeights{
	"GK": {Attack: catW(0, .4, .6, .2, .8, .4), Defense: catW(0, .6, .7, .6, 1.0, .6)},
	"CB": {Attack: catW(.2, .4, .4, .4, 0, .5), Defense: catW(.6, 1.0, .6, .8, 0, .7)},
	"LB": {Attack: catW(.5, .75, .4, .4, 0, .5), Defense: catW(.5, .85, .4, .6, 0, .5)},
	"RB": {Attack: catW(.5, .75, .4, .4, 0, .5), Defense: catW(.5, .85, .4, .6, 0, .5)},
	"DM": {Attack: catW(.5, .5, .5, .5, 0, .4), Defense: catW(.7, .7, .5, .7, 0, .5)},
	"CM": {Attack: catW(.8, .5, .6, .4, 0, .4), Defense: catW(.6, .6, .5, .6, 0, .4)},
	"AM": {Attack: catW(.9, .4, .7, .4, 0, .6), Defense: catW(.4, .3, .4, .6, 0, .5)},
	"LM": {Attack: catW(.7, .75, .5, .3, 0, .5), Defense: catW(.4, .6, .3, .5, 0, .4)},
	"RM": {Attack: catW(.7, .75, .5, .3, 0, .5), Defense: catW(.4, .6, .3, .5, 0, .4)},
	"ST": {Attack: catW(1.0, .7, .6, .2, 0, .5), Defense: catW(.3, .5, .3, .4, 0, .6)},
	"LW": {Attack: catW(.8, .85, .5, .3, 0, .6), Defense: catW(.4, .6, .3, .5, 0, .4)},
	"RW": {Attack: catW(.8, .85, .5, .3, 0, .6), Defense: catW(.4, .6, .3, .5, 0, .4)},
}

func catW(tech, phys, mental, tact, gk, pos float64) CategoryWeights {
	return CategoryWeights{Technical: tech, Physical: phys, Mental: mental, Tactical: tact, Goalkeeping: gk, Positional: pos}
}

const defaultAttackDefense = 50.0

// BuildSquadRatings aggregates the starting XI into one Attack/Defense pair by
// averaging each member's position-weighted attribute score (weighted mean,
// so a zero-weight category — e.g. Goalkeeping for outfielders — never
// dilutes). When a per-player performance factor is supplied for a member it
// scales that member's contribution — this is where ComputePlayerPerformanceFactor
// and the key-player set reach the team ratings. Outputs are clamped to
// [1, 100] and rounded.
//
// members missing from the weight table fall back to a balanced recipe rather
// than erroring — the table is data, and new positions must not break a world.
//
// NOTE (PM open item 4): the numbers themselves are proposals; only the
// aggregation method is agreed.
func BuildSquadRatings(xi []SquadMember, weights PositionWeights, factors map[uuid.UUID]float64) (attack, defense int) {
	if len(xi) == 0 {
		return int(defaultAttackDefense), int(defaultAttackDefense)
	}
	var attSum, defSum float64
	for _, m := range xi {
		prof, ok := weights[m.Position]
		if !ok {
			prof = PositionProfile{Attack: balancedWeights, Defense: balancedWeights}
		}
		a := weightedScore(m.Attributes, prof.Attack)
		d := weightedScore(m.Attributes, prof.Defense)
		if f, ok := factors[m.PlayerID]; ok {
			a *= f
			d *= f
		}
		attSum += a
		defSum += d
	}
	return int(mathRound(clamp(attSum/float64(len(xi)), 1, 100))),
		int(mathRound(clamp(defSum/float64(len(xi)), 1, 100)))
}

var balancedWeights = CategoryWeights{
	Technical: .5, Physical: .5, Mental: .5, Tactical: .5, Goalkeeping: .1, Positional: .5,
}

// StyleProfile is the per-style category-weight multiplier table layered over
// DefaultPositionWeights when a side kicks off in that style (S05-01,
// docs/design/tactics-training-numerics.md §1.4). A 0 in a scale slot means
// "unchanged", so a style can tag only the categories it wants (proposal data —
// a PM tuning pass can drop, add, or renumber any style).
type StyleProfile struct {
	AttackScale  CategoryWeights
	DefenseScale CategoryWeights
}

// StyleProfiles maps each non-balanced style to its recipe skew. balanced has
// no entry: identity = DefaultPositionWeights. (Keys surface matchsim's
// canonical style constants.)
var StyleProfiles = map[string]StyleProfile{
	matchsim.StylePossession: {
		AttackScale:  CategoryWeights{Technical: 1.10, Physical: 0.92, Mental: 1.10},
		DefenseScale: CategoryWeights{Technical: 1.10, Physical: 0.92, Mental: 1.10},
	},
	matchsim.StyleGegenpress: {
		AttackScale:  CategoryWeights{Physical: 1.12, Tactical: 1.10},
		DefenseScale: CategoryWeights{Physical: 1.05},
	},
	matchsim.StyleLowBlock: {
		AttackScale:  CategoryWeights{Physical: 1.08},
		DefenseScale: CategoryWeights{Physical: 1.05, Tactical: 1.12},
	},
	matchsim.StyleDirect: {
		AttackScale:  CategoryWeights{Technical: 1.06, Physical: 1.10},
		DefenseScale: CategoryWeights{},
	},
}

// WeightsForStyle returns the per-position recipe table a side plays in, i.e.
// base (DefaultPositionWeights) with the style's category scales applied. An
// unknown/empty style yields base unchanged. The returned table is a fresh
// copy — callers may safely hand it to BuildSquadRatings without mutating the
// shared default.
func WeightsForStyle(style string, base PositionWeights) PositionWeights {
	prof, ok := StyleProfiles[style]
	if !ok {
		return base
	}
	out := make(PositionWeights, len(base))
	for pos, p := range base {
		out[pos] = PositionProfile{
			Attack:  scaleWeights(p.Attack, prof.AttackScale),
			Defense: scaleWeights(p.Defense, prof.DefenseScale),
		}
	}
	return out
}

// scaleWeights multiplies every category weight by its style scale; a 0 scale
// (unset) is treated as 1 so partial profiles never zero a category.
func scaleWeights(w, scale CategoryWeights) CategoryWeights {
	return CategoryWeights{
		Technical:   w.Technical * styleScale(scale.Technical),
		Physical:    w.Physical * styleScale(scale.Physical),
		Mental:      w.Mental * styleScale(scale.Mental),
		Tactical:    w.Tactical * styleScale(scale.Tactical),
		Goalkeeping: w.Goalkeeping * styleScale(scale.Goalkeeping),
		Positional:  w.Positional * styleScale(scale.Positional),
	}
}

func styleScale(f float64) float64 {
	if f == 0 {
		return 1
	}
	return f
}

// weightedScore is the category-weighted mean of an attribute snapshot. A
// zero-total recipe degenerates to the neutral 50 rather than dividing by
// zero.
func weightedScore(a AttributeSnapshot, w CategoryWeights) float64 {
	sum := float64(a.Technical)*w.Technical +
		float64(a.Physical)*w.Physical +
		float64(a.Mental)*w.Mental +
		float64(a.Tactical)*w.Tactical +
		float64(a.Goalkeeping)*w.Goalkeeping +
		float64(a.Positional)*w.Positional
	den := w.Technical + w.Physical + w.Mental + w.Tactical + w.Goalkeeping + w.Positional
	if den == 0 {
		return defaultAttackDefense
	}
	return sum / den
}

// TakerPenaltyConversionRate computes the designated taker's penalty
// conversion chance for this matchday (addendum Part 7 §3), centred on the
// approved 78% baseline: pressure_handling and consistency trim outward, and
// severe crowd-pressure sentiment drags the rate down. Bounded to the
// negotiated [0.65, 0.88] band so Sunday-league composure stays inside the
// engine's contract.
func TakerPenaltyConversionRate(taker PlayerHiddenTraitsSnapshot, sentiment int) float64 {
	rate := 0.78 +
		(float64(clampScore(taker.PressureHandling))-50)/50*0.10 +
		(float64(clampScore(taker.Consistency))-50)/50*0.03 +
		float64(clampSentiment(sentiment))/100*0.03
	return clamp(rate, 0.65, 0.88)
}
