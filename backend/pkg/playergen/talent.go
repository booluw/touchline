// Talent-rarity generation (S08-01). A deterministic rarity roll at creation
// decides whether a prospect is a future star or a squad-filler, separate from
// the route's baseline quality (QualityOffset). Rarity sets the potential
// FLOOR — the tail of player_generation hidden-trait potential — so a
// wonderkid born in the same pool as a journeyman is measurably more likely to
// catch the growth multipliers (16-21 x1.8) and top out at an elite ceiling.
//
// The class label itself is intentionally NOT persisted: potential is what
// the engine reads. Visible "wonderkid" labelling, scouting reveals and
// year-over-year potential flex are the S08-02 surface, layered on the same
// potential value this roll sets.
package playergen

import "math/rand"

// TalentClass ranks how exceptional a generated prospect's ceiling is.
type TalentClass int

const (
	// TalentJourneyman is the default tier: a solid professional, nothing more.
	TalentJourneyman TalentClass = iota
	// TalentTopProspect has a clearly above-average ceiling (squad starter).
	TalentTopProspect
	// TalentWonderkid has a very high ceiling (club star / regular international).
	TalentWonderkid
	// TalentGenerational is the once-in-a-few-worlds ceiling (Ballon d'Or tail).
	TalentGenerational
)

// String returns the wire label for the class (used by read models/dashboards).
func (t TalentClass) String() string {
	switch t {
	case TalentTopProspect:
		return "top_prospect"
	case TalentWonderkid:
		return "wonderkid"
	case TalentGenerational:
		return "generational"
	default:
		return "journeyman"
	}
}

// TalentOdds is the rarity weight table for a generation profile. Weights are
// relative; any profile with all-zero weights falls back to the world default
// (WorldTalentOdds). Profiles let the academy tiers and the street intake skew
// rarity independently of each other.
type TalentOdds struct {
	// Journeyman / TopProspect / Wonderkid / Generational weight bands.
	Journeyman   int
	TopProspect  int
	Wonderkid    int
	Generational int
}

// WorldTalentOdds is the default rarity profile used for the free-agent pool
// and world seeding: ~1 in 1000 generationals, ~0.4% wonderkids so a freshly
// bootstrapped world is never barren of early future stars.
var WorldTalentOdds = TalentOdds{
	Journeyman:   940,
	TopProspect:  55,
	Wonderkid:    4,
	Generational: 1,
}

// potentialBonus is the potential-FLOOR lift each class guarantees, threaded
// into generateTraitsAndPersonality. It raises the bottom of the roll, not the
// roll width, so a high class never collides with the [1,100] clamp.
func (t TalentClass) potentialBonus() int {
	switch t {
	case TalentTopProspect:
		return 12
	case TalentWonderkid:
		return 22
	case TalentGenerational:
		return 32
	default:
		return 0
	}
}

// rollTalent draws a class for one prospect from the given rarity profile using
// the caller's seeded rng. A zero/empty profile falls back to WorldTalentOdds.
func rollTalent(rng *rand.Rand, odds TalentOdds) TalentClass {
	if odds.Journeyman <= 0 && odds.TopProspect <= 0 && odds.Wonderkid <= 0 && odds.Generational <= 0 {
		odds = WorldTalentOdds
	}
	total := odds.Journeyman + odds.TopProspect + odds.Wonderkid + odds.Generational
	if total <= 0 {
		return TalentJourneyman
	}
	roll := rng.Intn(total)
	threshold := odds.Journeyman
	if roll < threshold {
		return TalentJourneyman
	}
	threshold += odds.TopProspect
	if roll < threshold {
		return TalentTopProspect
	}
	threshold += odds.Wonderkid
	if roll < threshold {
		return TalentWonderkid
	}
	return TalentGenerational
}
