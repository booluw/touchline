package squad

// Player overall rating & curated headline attributes (S08-01).
//
// One canonical, user-facing "overall" per player — computed from the same
// per-position recipe matchsim consumes (DefaultPositionWeights) on a
// "one-member XI" — replaces the three ad-hoc overalls that drifted between
// pool, draft and valuation. Attributes run [1,100], but the displayed overall
// is capped at 99 to match a FIFA/EA-FC style rating ceiling.
//
// Headline keys are the EA-style condensed stat block shown in read models and
// the dashboard: a compact per-position subset of player.player_attributes
// keys (proposal data — see docs/design/academy-numerics.md).

// OverallCap is the user-facing rating ceiling (display only; raw attributes
// stay [1,100]).
const OverallCap = 99

// PositionalOverall collapses one player's six category averages into the
// position-weighted "overall" shown to managers. It clamps to [1,99]: 99 is
// the display ceiling, and a 100-attribute player still shows 99.
func PositionalOverall(position string, attrs AttributeSnapshot) int {
	prof := profileFor(position)
	att := weightedScore(attrs, prof.Attack)
	def := weightedScore(attrs, prof.Defense)
	ovr := int(mathRound((att + def) / 2))
	if ovr > OverallCap {
		ovr = OverallCap
	}
	if ovr < 1 {
		ovr = 1
	}
	return ovr
}

// OverallDelta is the position-weighted net movement of a player's categories
// since their last recorded weekly change (sum of per-key deltas collapsed to
// the six categories). It is signed — regression is a negative number — and is
// deliberately NOT clamped to the [1,100] band.
func OverallDelta(position string, catDeltas AttributeSnapshot) int {
	prof := profileFor(position)
	att := weightedDelta(catDeltas, prof.Attack)
	def := weightedDelta(catDeltas, prof.Defense)
	return int(mathRound((att + def) / 2))
}

// profileFor resolves the position recipe, falling back to the balanced table
// for positions absent from the data (data must never break a read).
func profileFor(position string) PositionProfile {
	if prof, ok := DefaultPositionWeights[position]; ok {
		return prof
	}
	return PositionProfile{Attack: balancedWeights, Defense: balancedWeights}
}

// weightedDelta is weightedScore for a signed snapshot: a delta category with
// a zero recipe weight contributes nothing, and an all-zero snapshot yields 0
// (not the 50 neutral the attribute path returns).
func weightedDelta(d AttributeSnapshot, w CategoryWeights) float64 {
	den := w.Technical + w.Physical + w.Mental + w.Tactical + w.Goalkeeping + w.Positional
	if den == 0 {
		return 0
	}
	sum := float64(d.Technical)*w.Technical +
		float64(d.Physical)*w.Physical +
		float64(d.Mental)*w.Mental +
		float64(d.Tactical)*w.Tactical +
		float64(d.Goalkeeping)*w.Goalkeeping +
		float64(d.Positional)*w.Positional
	return sum / den
}

// headlineKeysForPosition is the condensed per-position stat block. Keys are
// the stable player.player_attributes attribute_key values (see playergen's
// catalogue). Proposal data: tuning = editing this table only.
var headlineKeysForPosition = map[string][]string{
	"GK": {"handling", "reflexes", "diving", "agility", "composure", "kicking"},
	"CB": {"heading", "tackling", "marking", "strength", "jumping", "composure"},
	"LB": {"pace", "crossing", "tackling", "stamina", "passing", "dribbling"},
	"RB": {"pace", "crossing", "tackling", "stamina", "passing", "dribbling"},
	"DM": {"tackling", "passing", "defensive_awareness", "stamina", "anticipation", "pressing"},
	"CM": {"passing", "vision", "dribbling", "decision_making", "stamina", "first_touch"},
	"AM": {"passing", "vision", "dribbling", "finishing", "composure", "creativity"},
	"LM": {"pace", "crossing", "dribbling", "passing", "acceleration", "first_touch"},
	"RM": {"pace", "crossing", "dribbling", "passing", "acceleration", "first_touch"},
	"LW": {"pace", "dribbling", "crossing", "acceleration", "finishing", "passing"},
	"RW": {"pace", "dribbling", "crossing", "acceleration", "finishing", "passing"},
	"ST": {"finishing", "heading", "composure", "pace", "off_the_ball", "first_touch"},
}

// HeadlineKeysForPosition returns the condensed per-position stat block for a
// primary position. An unknown position falls back to the striker block so a
// new row never renders empty. The caller must not mutate the returned slice.
func HeadlineKeysForPosition(position string) []string {
	if keys, ok := headlineKeysForPosition[position]; ok {
		return keys
	}
	return headlineKeysForPosition["ST"]
}

// HeadlineKeysToDeltas collapses a per-key delta map into the six-category
// AttributeSnapshot the overall delta math consumes. Missing/unknown keys are
// ignored; each key's delta is summed into its category.
func HeadlineKeysToDeltas(deltas map[string]int) AttributeSnapshot {
	var out AttributeSnapshot
	for key, delta := range deltas {
		switch key {
		case "finishing", "passing", "dribbling", "crossing", "long_shots",
			"heading", "free_kicks", "penalties", "first_touch", "tackling":
			out.Technical += delta
		case "pace", "acceleration", "strength", "jumping", "agility",
			"stamina", "balance", "natural_fitness":
			out.Physical += delta
		case "composure", "anticipation", "vision", "work_rate",
			"concentration", "decision_making", "positioning", "off_the_ball",
			"teamwork":
			out.Mental += delta
		case "pressing", "creativity", "tempo_control", "defensive_awareness":
			out.Tactical += delta
		case "versatility", "positional_instinct", "space_reading",
			"man_awareness", "marking":
			out.Positional += delta
		case "handling", "reflexes", "diving", "one_on_ones", "aerial_control",
			"kicking", "throwing", "penalty_stopping":
			out.Goalkeeping += delta
		}
	}
	return out
}
