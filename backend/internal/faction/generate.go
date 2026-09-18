package faction

import "sort"

// Edge-generation constants (proposal values, docs/design/squad-dynamics-numerics.md).
const (
	// Affinity thresholds on the 0..100 pair-affinity score.
	FriendshipThreshold = 65
	RivalryThreshold    = 25

	// Mentorship needs a genuine age gap, the same position group and an elder
	// with real leadership presence.
	MentorshipAgeGap     = 8
	MentorshipLeadership = 65

	NationalTeamStrength = 45
	NationalTeamTrust    = 20
	AcademyStrength      = 35
	AcademyTrust         = 15
	MentorshipStrength   = 40
	MentorshipTrust      = 25
	FriendshipBase       = 20
	RivalryBase          = 20
)

// MemberProfile is the persistence-free input to edge generation: the identity
// and behavioural attributes that decide who bonds with whom.
type MemberProfile struct {
	PlayerID          string
	Name              string
	Nationality       string
	SecondNationality string
	Age               int
	Position          string // player.players.primary_position
	AcademyProduct    bool
	Leadership        int // 1..100
	Sociability       int // 1..100
	Volatility        int // 1..100
	Loyalty           int // 1..100
}

// GenerateEdges deterministically derives a club's player↔player relationship
// edges from the world seed and the members' attributes. It is a pure function:
// the same inputs always produce the same edges in the same order, independent
// of squad composition (a pair's affinity depends only on the pair).
func GenerateEdges(worldSeed int64, members []MemberProfile) []RelationshipEdge {
	sorted := make([]MemberProfile, len(members))
	copy(sorted, members)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PlayerID < sorted[j].PlayerID })

	var out []RelationshipEdge
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			a, b := sorted[i], sorted[j]
			from, to := a.PlayerID, b.PlayerID
			if from > to {
				from, to = to, from
			}

			if sharesNationality(a, b) {
				out = append(out, RelationshipEdge{
					From: from, To: to, Kind: KindNationalTeam,
					Strength: NationalTeamStrength, Trust: NationalTeamTrust,
				})
			}
			if a.AcademyProduct && b.AcademyProduct {
				out = append(out, RelationshipEdge{
					From: from, To: to, Kind: KindAcademy,
					Strength: AcademyStrength, Trust: AcademyTrust,
				})
			}
			elder, younger := a, b
			if b.Age > a.Age {
				elder, younger = b, a
			}
			if elder.Age-younger.Age >= MentorshipAgeGap &&
				elder.Position == younger.Position && elder.Position != "" &&
				elder.Leadership >= MentorshipLeadership {
				out = append(out, RelationshipEdge{
					From: from, To: to, Kind: KindMentorship,
					Strength: MentorshipStrength, Trust: MentorshipTrust,
				})
			}

			affinity := pairAffinity(worldSeed, a, b)
			switch {
			case affinity >= FriendshipThreshold:
				s := FriendshipBase + (affinity - FriendshipThreshold)
				out = append(out, RelationshipEdge{
					From: from, To: to, Kind: KindFriendship,
					Strength: clampInt(s, 1, 100),
					Trust:    clampInt(s/2, 1, 100),
				})
			case affinity <= RivalryThreshold:
				s := RivalryBase + (RivalryThreshold - affinity)
				out = append(out, RelationshipEdge{
					From: from, To: to, Kind: KindRivalry,
					Strength: -clampInt(s, 1, 100),
					Trust:    -clampInt(s/2, 1, 100),
				})
			}
		}
	}
	return out
}

// FormerTeammateEdges builds the edges written when player leaves a club: a
// modest positive bond to each remaining squad member.
func FormerTeammateEdges(playerID string, formerTeammates []string) []RelationshipEdge {
	out := make([]RelationshipEdge, 0, len(formerTeammates))
	for _, mate := range formerTeammates {
		if mate == playerID {
			continue
		}
		from, to := playerID, mate
		if from > to {
			from, to = to, from
		}
		out = append(out, RelationshipEdge{
			From: from, To: to, Kind: KindFormerTeammate,
			Strength: 20, Trust: 10,
		})
	}
	return out
}

// pairAffinity is the deterministic 0..100 compatibility of a pair, nudged by
// how sociable the two are. It depends only on (world seed, pair id), so squad
// changes never re-roll an existing bond.
func pairAffinity(worldSeed int64, a, b MemberProfile) int {
	base := int(PairStream(worldSeed, a.PlayerID, b.PlayerID) % 101)
	adj := (a.Sociability+b.Sociability)/2 - 50
	return clampInt(base+adj/2, 0, 100)
}

func sharesNationality(a, b MemberProfile) bool {
	if a.Nationality == "" || b.Nationality == "" {
		return false
	}
	if a.Nationality == b.Nationality {
		return true
	}
	return a.SecondNationality == b.Nationality ||
		b.SecondNationality == a.Nationality ||
		(a.SecondNationality != "" && a.SecondNationality == b.SecondNationality)
}
