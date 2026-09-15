package bootstrap

import (
	"hash/fnv"
	"math/rand"

	"github.com/google/uuid"

	internalboard "github.com/touchline/backend/internal/board"
)

// clubArchetypes is every club archetype the seeding can produce, weighted so
// fixture worlds skew plausible (community and survival clubs more common than
// giants).
var clubArchetypes = []struct {
	archetype string
	weight    int
}{
	{"community_club", 30},
	{"survival_club", 25},
	{"academy_club", 15},
	{"fallen_giant", 10},
	{"moneyball_club", 10},
	{"giant", 5},
	{"investor_club", 5},
}

// archetypePersonality maps a club archetype to the board personality that
// most plausibly owns it (numerics source of truth: docs/design/board-numerics.md).
var archetypePersonality = map[string]internalboard.Persona{
	"giant":          internalboard.PersonaPrestigeOwner,
	"academy_club":   internalboard.PersonaAcademyOwner,
	"moneyball_club": internalboard.PersonaFinancialConservative,
	"community_club": internalboard.PersonaPatientOwner,
	"fallen_giant":   internalboard.PersonaDemandingOwner,
	"investor_club":  internalboard.PersonaFinancialConservative,
	"survival_club":  internalboard.PersonaPatientOwner,
}

type clubProfile struct {
	archetype string

	ambition              int
	patience              int
	academyImportance     int
	managerialControl     int
	starPowerPreference   int
	wageTolerance         int
	financialPhilosophy   string
	recruitmentPhilosophy string
	sellingPhilosophy     string
	tacticalIdentity      string
	culturalIdentity      string

	supporterLoyalty              int
	supporterIdentity             string
	supporterFinancialSensitivity int
}

func jittered(rng *rand.Rand, base, spread int) int {
	v := base + rng.Intn(2*spread+1) - spread
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// profileFor derives a deterministic club DNA, board persona and supporter
// profile from the club's identity (peer of the draft's seeded draw; the seed
// is derived from clubName+clubID so it is stable for the club's lifetime).
func profileFor(clubName string, clubID uuid.UUID) (clubProfile, internalboard.Persona, bool) {
	h := fnv.New64a()
	h.Write([]byte("profile:" + clubName + ":" + clubID.String()))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))

	total := 0
	for _, a := range clubArchetypes {
		total += a.weight
	}
	pick := rng.Intn(total)
	picked := clubArchetypes[0].archetype
	for _, a := range clubArchetypes {
		if pick < a.weight {
			picked = a.archetype
			break
		}
		pick -= a.weight
	}

	persona, ok := archetypePersonality[picked]
	if !ok {
		persona = internalboard.PersonaPatientOwner
	}

	base := map[string][2]int{ // ambition, patience per archetype
		"giant":          {90, 40},
		"academy_club":   {70, 75},
		"moneyball_club": {55, 70},
		"community_club": {45, 85},
		"fallen_giant":   {85, 35},
		"investor_club":  {60, 60},
		"survival_club":  {35, 80},
	}[picked]
	profile := clubProfile{
		archetype: picked,
		ambition:  jittered(rng, base[0], 10),
		patience:  jittered(rng, base[1], 10),
		academyImportance: jittered(rng,
			map[string]int{"giant": 60, "academy_club": 95, "moneyball_club": 40,
				"community_club": 70, "fallen_giant": 55, "investor_club": 30, "survival_club": 65}[picked], 10),
		managerialControl: jittered(rng, 50, 10),
		starPowerPreference: jittered(rng,
			map[string]int{"giant": 80, "academy_club": 35, "moneyball_club": 30,
				"community_club": 40, "fallen_giant": 75, "investor_club": 25, "survival_club": 45}[picked], 10),
		wageTolerance: jittered(rng,
			map[string]int{"giant": 70, "academy_club": 55, "moneyball_club": 45,
				"community_club": 60, "fallen_giant": 65, "investor_club": 40, "survival_club": 50}[picked], 10),
		financialPhilosophy: map[string]string{
			"giant": "aggressive", "academy_club": "balanced", "moneyball_club": "self_sustaining",
			"community_club": "conservative", "fallen_giant": "debt_tolerant",
			"investor_club": "investor_funded", "survival_club": "conservative",
		}[picked],
		recruitmentPhilosophy: map[string]string{
			"giant": "superstar_recruitment", "academy_club": "academy_first",
			"moneyball_club": "undervalued_players", "community_club": "domestic_youth",
			"fallen_giant": "free_transfers", "investor_club": "international_scouting",
			"survival_club": "free_transfers",
		}[picked],
		sellingPhilosophy: map[string]string{
			"giant": "never_sell_stars", "academy_club": "sell_when_replacement_exists",
			"moneyball_club": "financially_driven", "community_club": "player_driven",
			"fallen_giant": "sell_for_large_profit", "investor_club": "financially_driven",
			"survival_club": "financially_driven",
		}[picked],
		culturalIdentity: map[string]string{
			"giant": "prestigious", "academy_club": "youth_oriented", "moneyball_club": "local",
			"community_club": "working_class", "fallen_giant": "prestigious",
			"investor_club": "international", "survival_club": "working_class",
		}[picked],
		supporterLoyalty: jittered(rng, 75, 15),
		supporterIdentity: map[string]string{
			"giant": "prestigious", "academy_club": "youth_oriented", "moneyball_club": "local",
			"community_club": "working_class", "fallen_giant": "working_class",
			"investor_club": "international", "survival_club": "local",
		}[picked],
		supporterFinancialSensitivity: jittered(rng, 55, 15),
	}
	tactical := []string{"possession", "counterattack", "pressing", "defensive", "direct", "adaptable"}
	profile.tacticalIdentity = tactical[rng.Intn(len(tactical))]
	return profile, persona, ok
}
