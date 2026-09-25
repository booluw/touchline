// Package faction implements S09-02 — graph-derived squad dynamics and dressing
// room factions.
//
// The package has two halves, mirroring internal/injury:
//
//   - a pure, deterministic, DB-free engine (engine.go, generate.go, stream.go)
//     that turns a caller-supplied SquadSnapshot into hierarchy tiers, social
//     factions, cohesion, morale contagion and unrest; and
//   - the Postgres read/generation layer (store.go, service.go) that loads the
//     snapshot from the social.relationships graph with recursive CTEs and
//     persists generated player↔player edges.
//
// Hierarchy, factions and cohesion are always derived from the graph at read
// time — never denormalized onto players. Every outcome carries an
// explanation.Explanation tracing the causal chain (PRD §15; technical plan
// §§6, 16).
package faction

import (
	"github.com/google/uuid"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/explanation"
)

// Action is one management action the dressing room responds to. The catalogue
// mirrors S09-01's management-action matrix so squad dynamics resolve for the
// whole surface, not just the headline "sold a leader" case.
type Action string

const (
	// ActionInspect computes structure only (tiers + factions), no contagion.
	ActionInspect Action = "inspect"
	// Management was late on / broke a promise to a player.
	ActionMissedPromise Action = "missed_promise"
	// Manager dropped a player from the starting XI (or the squad).
	ActionDroppedFromStartingXI Action = "dropped_from_starting_xi"
	// Manager benched a player long-term.
	ActionBenchedLongTerm Action = "benched_long_term"
	// Manager offered a wage cut.
	ActionWageCutOffered Action = "wage_cut_offered"
	// Manager approved/offered a new contract (bonding).
	ActionContractApproved Action = "contract_approved"
	// A club queried a player's availability (bid query).
	ActionTransferBidQuery Action = "transfer_bid_query"
	// Management blocked a transfer bid.
	ActionTransferBidBlocked Action = "transfer_bid_blocked"
	// Management released/sold a squad member.
	ActionReleasedFromSquad Action = "released_from_squad"
	// Management raised the training load.
	ActionTrainingLoadIncreased Action = "training_load_increased"
	// Management gave a motivational team talk (bonding).
	ActionTeamTalkMotivational Action = "team_talk_motivational"
	// Management gave a critical team talk.
	ActionTeamTalkCritical Action = "team_talk_critical"
)

// SquadMember is one node of the dressing-room graph snapshot.
type SquadMember struct {
	PlayerID   string
	Name       string
	Leadership int // player_personality.leadership, 1..100
	Loyalty    int // player_personality.loyalty, 1..100
	Volatility int // player_personality.emotional_volatility, 1..100
}

// RelationshipEdge is one undirected bond in the dressing-room graph. Strength
// and Trust mirror social.relationships.strength / trust (−100..100).
type RelationshipEdge struct {
	From     string
	To       string
	Strength int
	Trust    int
	Kind     string // social.relationships.relationship_type
}

// SquadSnapshot is the deterministic graph the engine runs on. Roots is the
// connected-component root per player as computed by the graph CTE; when empty
// the engine falls back to its own union-find so the engine stays usable as a
// pure function in unit tests.
type SquadSnapshot struct {
	Members []SquadMember
	Edges   []RelationshipEdge
	Roots   map[string]string
}

// Tier is a dressing-room hierarchy rung.
type Tier string

const (
	TierTeamLeader        Tier = "team_leader"
	TierHighlyInfluential Tier = "highly_influential"
	TierInfluential       Tier = "influential"
	TierOther             Tier = "other"
)

// Faction labels (PRD §15 group archetypes).
const (
	LabelVeteranCore   = "veteran_core"
	LabelForeignCohort = "foreign_cohort"
	LabelYouthAlliance = "youth_alliance"
	LabelNeutralRoom   = "neutral_room"
)

// Generated relationship kinds written to social.relationships.
const (
	KindFriendship     = "friendship"
	KindRivalry        = "rivalry"
	KindMentorship     = "mentorship"
	KindNationalTeam   = "national_team"
	KindAcademy        = "academy"
	KindFormerTeammate = "former_teammate"
)

// Unrest demands the dressing room raises at the severe end.
const (
	DemandBoardMeeting        = "board_meeting"
	DemandEnMasseTransferReqs = "en_masse_transfer_requests"
)

// Faction is one deterministic dressing-room cluster: a connected component of
// the relationship graph with a leader, bounded cohesion and a causal
// explanation. Flat leader/member ids stay internal; the wire carries nested
// player refs.
type Faction struct {
	Label       string                   `json:"label"`
	LeaderID    string                   `json:"-"`
	Members     []string                 `json:"-"`
	Leader      *apiref.PlayerRef        `json:"leader,omitempty"`
	MembersRefs []*apiref.PlayerRef      `json:"members"`
	Cohesion    int                      `json:"cohesion"`
	Explanation *explanation.Explanation `json:"explanation,omitempty"`
}

// Contagion is a bounded, gradual morale spill across the graph. Confidence
// starts well under 100 and hardens hop by hop — never instant, never 100.
// Flat source/affected ids stay internal; the wire carries nested refs.
type Contagion struct {
	Source       string                   `json:"-"`
	SourceRef    *apiref.PlayerRef        `json:"source,omitempty"`
	Confidence   int                      `json:"confidence"`
	Affected     []string                 `json:"-"`
	AffectedRefs []*apiref.PlayerRef      `json:"affected"`
	Explanation  *explanation.Explanation `json:"explanation,omitempty"`
}

// Unrest is the severe end-state: contagion that crossed the dressing room's
// threshold becomes a delegation demanding a board meeting or en-masse
// transfer requests, with an explanation tracing the causal chain. Flat
// affected ids stay internal; the wire carries nested refs.
type Unrest struct {
	Severity     int                      `json:"severity"`
	Demand       string                   `json:"demand"`
	Affected     []string                 `json:"-"`
	AffectedRefs []*apiref.PlayerRef      `json:"affected"`
	Explanation  *explanation.Explanation `json:"explanation,omitempty"`
}

// SquadDynamics is the full deterministic outcome of one action.
type SquadDynamics struct {
	Action      Action
	Tiers       map[string]Tier
	Factions    []Faction
	Contagion   *Contagion
	Unrest      *Unrest
	Explanation *explanation.Explanation
}

// AttachNames decorates the faction/contagion player ids with their display
// names, producing the nested refs the wire carries. names maps player-id
// strings to display names; unknown ids yield nameless refs.
func (d *SquadDynamics) AttachNames(names map[string]string) {
	if d == nil {
		return
	}
	for i := range d.Factions {
		d.Factions[i].Leader = playerRef(d.Factions[i].LeaderID, names)
		d.Factions[i].MembersRefs = playerRefs(d.Factions[i].Members, names)
	}
	if d.Contagion != nil {
		d.Contagion.SourceRef = playerRef(d.Contagion.Source, names)
		d.Contagion.AffectedRefs = playerRefs(d.Contagion.Affected, names)
	}
}

// playerRef renders one player id as a display ref (nameless when unresolved).
func playerRef(id string, names map[string]string) *apiref.PlayerRef {
	if id == "" {
		return nil
	}
	u, err := uuid.Parse(id)
	if err != nil {
		u = uuid.Nil
	}
	return &apiref.PlayerRef{ID: u, Name: names[id]}
}

// playerRefs renders a list of player ids as display refs, preserving order.
func playerRefs(ids []string, names map[string]string) []*apiref.PlayerRef {
	out := make([]*apiref.PlayerRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, playerRef(id, names))
	}
	return out
}

// TierView is one hierarchy entry in the API read model. The flat player id
// stays internal; the wire carries a player ref and, for influencers, the full
// player profile (no follow-up roster request needed).
type TierView struct {
	PlayerID string            `json:"-"`
	Player   *apiref.PlayerRef `json:"player"`
	Name     string            `json:"-"`
	Tier     Tier              `json:"tier"`
	Profile  *PlayerProfile    `json:"profile,omitempty"`
}

// PlayerAttributes is one player's six persisted attribute-category means
// ([1,100] each; 0 until seeded), rolled up round-half-up from the EAV exactly
// like internal/squad.
type PlayerAttributes struct {
	Technical   int `json:"technical"`
	Physical    int `json:"physical"`
	Mental      int `json:"mental"`
	Tactical    int `json:"tactical"`
	Goalkeeping int `json:"goalkeeping"`
	Positional  int `json:"positional"`
}

// PlayerProfile is the full player read-model attached to each returned
// hierarchy tier: identity, uniform, behavioural personality and the six
// attribute-category means plus the position-weighted overall rating.
type PlayerProfile struct {
	ID                  uuid.UUID        `json:"id"`
	Name                string           `json:"name"`
	Position            string           `json:"position"`
	Age                 int              `json:"age"`
	Nationality         string           `json:"nationality"`
	SecondNationality   string           `json:"second_nationality,omitempty"`
	SquadNumber         *int             `json:"squad_number,omitempty"`
	Status              string           `json:"status"`
	SquadRole           string           `json:"squad_role,omitempty"`
	AcademyProduct      bool             `json:"is_academy_product"`
	Leadership          int              `json:"leadership"`
	Sociability         int              `json:"sociability"`
	EmotionalVolatility int              `json:"emotional_volatility"`
	Loyalty             int              `json:"loyalty"`
	Attributes          PlayerAttributes `json:"attributes"`
	Overall             int              `json:"overall"`
}

// profileOf renders MemberProfile into the wire profile, computing the
// position-weighted overall (squad.PositionalOverall, capped at 99).
func profileOf(m MemberProfile) *PlayerProfile {
	att := m.Attributes
	return &PlayerProfile{
		ID:                  mustUUID(m.PlayerID),
		Name:                m.Name,
		Position:            m.Position,
		Age:                 m.Age,
		Nationality:         m.Nationality,
		SecondNationality:   m.SecondNationality,
		SquadNumber:         m.SquadNumber,
		Status:              m.Status,
		SquadRole:           m.SquadRole,
		AcademyProduct:      m.AcademyProduct,
		Leadership:          m.Leadership,
		Sociability:         m.Sociability,
		EmotionalVolatility: m.Volatility,
		Loyalty:             m.Loyalty,
		Attributes:          att,
		Overall: squad.PositionalOverall(m.Position, squad.AttributeSnapshot{
			Technical:   att.Technical,
			Physical:    att.Physical,
			Mental:      att.Mental,
			Tactical:    att.Tactical,
			Goalkeeping: att.Goalkeeping,
			Positional:  att.Positional,
		}),
	}
}

func mustUUID(id string) uuid.UUID {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil
	}
	return u
}

// Dynamics is the club read model for GET /api/clubs/:id/dynamics: computed
// structure plus club-level aggregates, all derived from the graph.
type Dynamics struct {
	ClubID           uuid.UUID                `json:"-"`
	Club             *apiref.ClubRef          `json:"club"`
	Action           Action                   `json:"action"`
	Cohesion         int                      `json:"cohesion"`
	ManagerSupport   int                      `json:"manager_support"`
	DressingRoomMood int                      `json:"dressing_room_mood"`
	Tiers            []TierView               `json:"tiers"`
	Factions         []Faction                `json:"factions"`
	Contagion        *Contagion               `json:"contagion,omitempty"`
	Unrest           *Unrest                  `json:"unrest,omitempty"`
	Explanation      *explanation.Explanation `json:"explanation,omitempty"`
}
