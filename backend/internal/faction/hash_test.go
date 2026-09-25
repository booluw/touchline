package faction

import (
	"testing"

	"github.com/touchline/backend/internal/squad"
)

const pid = "00000000-0000-4000-8000-000000000001"

func baseMember(id string) MemberProfile {
	return MemberProfile{
		PlayerID:          id,
		Name:              "N" + id,
		Nationality:       "ENG",
		SecondNationality: "IRE",
		Age:               24,
		Position:          "CM",
		AcademyProduct:    false,
		Leadership:        60,
		Sociability:       70,
		Volatility:        40,
		Loyalty:           80,
		Status:            "active",
		SquadRole:         "key_player",
		Attributes:        PlayerAttributes{Technical: 70, Physical: 60, Mental: 75},
	}
}

func TestMemberHashDeterministicAndOrderInvariant(t *testing.T) {
	a, b := baseMember("a"), baseMember("b")
	c := baseMember("c")
	b.Age, b.Position, b.AcademyProduct = 30, "CB", true
	c.SecondNationality = ""

	want := memberHash(42, []MemberProfile{b, a, c})
	if got := memberHash(42, []MemberProfile{a, b, c}); got != want {
		t.Fatalf("hash must be order-invariant: %d vs %d", got, want)
	}
	if got := memberHash(42, []MemberProfile{a, b, c, baseMember("a")}); got == want {
		t.Fatal("hash must change when membership changes")
	}
	if got := memberHash(43, []MemberProfile{a, b, c}); got == want {
		t.Fatal("hash must change when the world seed changes")
	}
	// Read-model enrichment must never re-roll the graph: attributes, uniform
	// and role are excluded from the fingerprint.
	enriched := a
	enriched.Attributes.Technical = 99
	enriched.SquadNumber = intPtr(7)
	enriched.SquadRole = "squad_player"
	if got := memberHash(42, []MemberProfile{b, enriched, c}); got != want {
		t.Fatal("read-model enrichment must not affect the fingerprint")
	}
	// Generation inputs must: a sociability drift changes affinity.
	bumped := a
	bumped.Sociability = 71
	if got := memberHash(42, []MemberProfile{b, bumped, c}); got == want {
		t.Fatal("generation input (sociability) must affect the fingerprint")
	}
}

func intPtr(v int) *int { return &v }

func TestProfileOfBuildsFullProfile(t *testing.T) {
	num := 7
	m := baseMember(pid)
	m.Name = "Maya"
	m.SecondNationality = "ITA"
	m.Position = "ST"
	m.Age = 22
	m.AcademyProduct = true
	m.Leadership, m.Sociability, m.Volatility, m.Loyalty = 55, 65, 45, 85
	m.SquadNumber = &num
	m.Status = "active"
	m.SquadRole = "rotation"
	m.Attributes = PlayerAttributes{Technical: 70, Physical: 80, Mental: 75, Tactical: 65, Goalkeeping: 10, Positional: 60}

	p := profileOf(m)
	if p == nil {
		t.Fatal("profile must build")
	}
	if p.ID.String() != pid {
		t.Fatalf("id = %s, want %s", p.ID, pid)
	}
	if p.Name != "Maya" || p.Position != "ST" || p.Age != 22 {
		t.Fatalf("identity mismatch: %+v", p)
	}
	if p.Nationality != "ENG" || p.SecondNationality != "ITA" {
		t.Fatalf("nationality mismatch: %+v", p)
	}
	if p.SquadNumber == nil || *p.SquadNumber != 7 || p.SquadRole != "rotation" || p.Status != "active" {
		t.Fatalf("uniform mismatch: %+v", p)
	}
	if !p.AcademyProduct {
		t.Fatal("academy flag must survive")
	}
	if p.Leadership != 55 || p.Sociability != 65 || p.EmotionalVolatility != 45 || p.Loyalty != 85 {
		t.Fatalf("personality mismatch: %+v", p)
	}
	if p.Attributes.Goalkeeping != 10 || p.Attributes.Positional != 60 {
		t.Fatalf("attributes mismatch: %+v", p.Attributes)
	}
	wantOverall := squad.PositionalOverall("ST", squad.AttributeSnapshot{
		Technical: 70, Physical: 80, Mental: 75, Tactical: 65, Goalkeeping: 10, Positional: 60,
	})
	if p.Overall != wantOverall {
		t.Fatalf("overall = %d, want %d", p.Overall, wantOverall)
	}
}

func TestInfluencerTiersDropsOtherAndAttachesProfiles(t *testing.T) {
	ids := []string{
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
		"00000000-0000-4000-8000-000000000004",
		"00000000-0000-4000-8000-000000000005",
	}
	tiers := map[string]Tier{
		ids[0]: TierTeamLeader,
		ids[1]: TierHighlyInfluential,
		ids[2]: TierInfluential,
		ids[3]: TierOther,
	}
	profiles := []MemberProfile{
		baseMember(ids[0]),
		baseMember(ids[1]),
		baseMember(ids[2]),
		baseMember(ids[3]),
	}
	names := map[string]string{
		ids[0]: "A", ids[1]: "B", ids[2]: "C", ids[3]: "D",
	}

	got := influencerTiers(tiers, names, profiles)
	if len(got) != 3 {
		t.Fatalf("tiers = %d, want 3 (influencers only)", len(got))
	}
	if got[0].Tier != TierTeamLeader {
		t.Fatalf("tiers must stay rank-sorted, got %s first", got[0].Tier)
	}
	for _, tv := range got {
		if tv.Tier == TierOther {
			t.Fatalf("'other' must be dropped, got %+v", tv)
		}
		if tv.Profile == nil {
			t.Fatalf("%s must carry a profile", tv.PlayerID)
		}
		if tv.Profile.ID.String() != tv.PlayerID {
			t.Fatalf("profile id %s must match tier %s", tv.Profile.ID, tv.PlayerID)
		}
	}
}
