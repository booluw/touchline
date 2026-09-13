package playergen

import "math/rand"

// Player attribute catalogue.
//
// The player.player_attributes table is an EAV: attribute_category ×
// attribute_key → value (1..100). This file owns the deterministic catalogue —
// which keys exist in which category — plus the archetype table that maps a
// primary position to plausible starting values. Both are data, not logic:
// rebalancing is a data change. Keys are stable; removing one is a contract
// change for anything that reads them.
//
// Role bias: an outfielder never receives goalkeeping keys (a striker whose
// "reflexes" is 40 is noise, not data) and a goalkeeper never receives the
// technical ball-skill keys. The five outfield categories are otherwise always
// present, keeping the aggregation maths (weighted category means) uniform.

// CatalogOrder is the persistence order of attribute categories (deterministic
// insert iteration).
var CatalogOrder = []string{
	"technical", "physical", "mental", "tactical", "positional", "goalkeeping",
}

// attributeKeys is the key set per category, in display order.
var attributeKeys = map[string][]string{
	"technical": {
		"finishing", "passing", "dribbling", "crossing",
		"long_shots", "heading", "free_kicks", "penalties", "first_touch",
	},
	"physical": {
		"pace", "acceleration", "strength", "jumping",
		"agility", "stamina", "balance", "natural_fitness",
	},
	"mental": {
		"composure", "anticipation", "vision", "work_rate",
		"concentration", "decision_making", "positioning", "off_the_ball",
	},
	"tactical": {
		"pressing", "creativity", "tempo_control", "defensive_awareness",
	},
	"positional": {
		"versatility", "positional_instinct", "space_reading", "man_awareness",
	},
	"goalkeeping": {
		"handling", "reflexes", "diving", "one_on_ones",
		"aerial_control", "kicking", "throwing", "penalty_stopping",
	},
}

// keyAdjust nudges specific skills relative to their category mean. A striker
// finishes above the technical mean; a centre-half reading jumps higher.
var keyAdjust = map[string]int{
	// technical
	"finishing": 10, "first_touch": 4, "passing": 4, "dribbling": 6,
	"crossing": 6, "long_shots": 2, "heading": 5, "penalties": 3,
	// physical
	"pace": 8, "acceleration": 10, "stamina": 4, "strength": 4, "jumping": 4,
	// mental
	"vision": 4, "off_the_ball": 4,
	// tactical
	"pressing": 8, "tempo_control": 6, "defensive_awareness": 6,
	// goalkeeping
	"reflexes": 8, "handling": 6, "diving": 5, "kicking": 2,
}

// categoryMeans anchors a position's starting category averages. Values sit in
// the mid-band so first-season squads are competitive without being star-lined.
var categoryMeans = map[string]map[string]int{
	"GK": {"goalkeeping": 68, "physical": 60, "mental": 62, "tactical": 55, "positional": 58},
	"CB": {"technical": 50, "physical": 68, "mental": 60, "tactical": 62, "positional": 60},
	"LB": {"technical": 58, "physical": 62, "mental": 58, "tactical": 58, "positional": 56},
	"RB": {"technical": 58, "physical": 62, "mental": 58, "tactical": 58, "positional": 56},
	"DM": {"technical": 56, "physical": 62, "mental": 62, "tactical": 65, "positional": 55},
	"CM": {"technical": 62, "physical": 56, "mental": 64, "tactical": 60, "positional": 55},
	"AM": {"technical": 68, "physical": 50, "mental": 64, "tactical": 55, "positional": 54},
	"LM": {"technical": 63, "physical": 60, "mental": 58, "tactical": 52, "positional": 52},
	"RM": {"technical": 63, "physical": 60, "mental": 58, "tactical": 52, "positional": 52},
	"ST": {"technical": 66, "physical": 62, "mental": 60, "tactical": 48, "positional": 56},
	"LW": {"technical": 66, "physical": 64, "mental": 56, "tactical": 50, "positional": 52},
	"RW": {"technical": 66, "physical": 64, "mental": 56, "tactical": 50, "positional": 52},
}

// attrJitter is the per-key spread around archetype+adjust.
const attrJitter = 15

// CategoryForKey maps a catalogue key back to its category.
func CategoryForKey(key string) string {
	for cat, keys := range attributeKeys {
		for _, k := range keys {
			if k == key {
				return cat
			}
		}
	}
	return ""
}

// KeysForCategory returns the category's key list (display order, stable). The
// caller must not mutate the returned slice.
func KeysForCategory(category string) []string {
	return attributeKeys[category]
}

// categoriesForPosition is the category set persisted for a position: the
// schema's EAV is populated only where the row is meaningful.
func categoriesForPosition(pos string) []string {
	if pos == "GK" {
		return []string{"physical", "mental", "tactical", "positional", "goalkeeping"}
	}
	return []string{"technical", "physical", "mental", "tactical", "positional"}
}

// generateAttributes produces the position-anchored attribute set for a
// GeneratedPlayer from the factory's seeded rng. The output is deterministic
// for a fixed seed and always within [1, 100].
func generateAttributes(rng *rand.Rand, pos string) map[string]int {
	out := make(map[string]int)
	for _, cat := range categoriesForPosition(pos) {
		mean := 50
		if m, ok := categoryMeans[pos][cat]; ok {
			mean = m
		}
		for _, key := range attributeKeys[cat] {
			base := mean + keyAdjust[key]
			out[key] = clampInt(base+attrJitterOffset(rng, mean), 1, 100)
		}
	}
	return out
}

// attrJitterOffset skews a key around its archetype so a full squad has
// believable spread; the draw widens as the archetype mean rises (stars vary
// more than plodders).
func attrJitterOffset(rng *rand.Rand, mean int) int {
	width := 8 + mean/8
	return rng.Intn(2*width+1) - width
}

// CategoryAverage is the mean value of a category across the keys present in
// attrs (0 when the category has no present keys). Matchday aggregation reads
// category means, so this is also the canonical "how good is this player here"
// roll-up.
func CategoryAverage(attrs map[string]int, category string) int {
	keys := attributeKeys[category]
	sum := 0
	n := 0
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return (sum + n/2) / n // integer mean, round-half-up
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
