package player

import (
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// Squad role agreed on the active contract (player.contracts.squad_role).
const (
	SquadRoleKeyPlayer   = "key_player"
	SquadRoleRotation    = "rotation"
	SquadRoleSquadPlayer = "squad_player"
	SquadRoleDevelopment = "development"
)

// Player transfer-request statuses (player.player_transfer_requests.status).
const (
	TransferRequestPending    = "pending"
	TransferRequestApproved   = "approved"
	TransferRequestDenied     = "denied"
	TransferRequestReassured  = "reassured"
	TransferRequestAutoListed = "auto_listed"
	TransferRequestWithdrawn  = "withdrawn"
)

// Transfer-request reasons (player.player_transfer_requests.reason).
const (
	TransferReasonPlayingTime  = "playing_time"
	TransferReasonWage         = "wage"
	TransferReasonAmbition     = "ambition"
	TransferReasonHomesickness = "homesickness"
)

// Relationship-journal event types (social.relationship_events.event_type).
const (
	RelationshipTransferApproved = "transfer_approved"
	RelationshipTransferDenied   = "transfer_denied"
	RelationshipReassured        = "reassured"
	RelationshipPromiseKept      = "playing_time_promise_kept"
	RelationshipPromiseBroken    = "playing_time_promise_broken"
)

type Player struct {
	ID              uuid.UUID       `json:"id"`
	WorldID         uuid.UUID       `json:"world_id"`
	ClubID          *uuid.UUID      `json:"-"`
	Club            *apiref.ClubRef `json:"club,omitempty"`
	PersonID        uuid.UUID       `json:"person_id"`
	FirstName       string          `json:"first_name"`
	LastName        string          `json:"last_name"`
	DisplayName     string          `json:"display_name"`
	Nationality     string          `json:"nationality"`
	DateOfBirth     string          `json:"date_of_birth"`
	PrimaryPosition string          `json:"primary_position"`
	SquadNumber     *int            `json:"squad_number"`
}

// PlayerAttributesRecord is the S05 placeholder for a per-player attribute
// record: identity only, because GetPlayerAttributes still returns the
// zero-value stub. The real attribute data is the six-category block below,
// derived from the EAV at read time.
type PlayerAttributesRecord struct {
	ID       uuid.UUID `json:"id"`
	PlayerID uuid.UUID `json:"player_id"`
}

// PlayerAttributes is one roster player's six persisted attribute-category
// means ([1,100] each; 0 until seeded), rolled up round-half-up from the
// player_attributes EAV exactly like internal/squad. The shape mirrors the
// dynamics profile block so the roster and dressing-room payloads agree.
type PlayerAttributes struct {
	Technical   int `json:"technical"`
	Physical    int `json:"physical"`
	Mental      int `json:"mental"`
	Tactical    int `json:"tactical"`
	Goalkeeping int `json:"goalkeeping"`
	Positional  int `json:"positional"`
}

// HiddenAttributes is player.player_hidden_traits ([1,100] each) minus the
// potential ceiling and its lock (OPD-59, widened by OPD-60). Exact values,
// and only ever for the caller's own players. nil when the player has no
// hidden-traits row (unseeded).
type HiddenAttributes struct {
	Professionalism      int `json:"professionalism"`
	Temperament          int `json:"temperament"`
	Adaptability         int `json:"adaptability"`
	Consistency          int `json:"consistency"`
	InjurySusceptibility int `json:"injury_susceptibility"`
	Ambition             int `json:"ambition"`
	Loyalty              int `json:"loyalty"`
	PressureHandling     int `json:"pressure_handling"`
	LearningSpeed        int `json:"learning_speed"`
}

// hiddenColumns is the shared SELECT list behind HiddenAttributes, in field order.
const hiddenColumns = `professionalism, temperament, adaptability, consistency,
	injury_susceptibility, ambition, loyalty, pressure_handling, learning_speed`

func (h *HiddenAttributes) scanTargets() []any {
	return []any{&h.Professionalism, &h.Temperament, &h.Adaptability, &h.Consistency,
		&h.InjurySusceptibility, &h.Ambition, &h.Loyalty, &h.PressureHandling, &h.LearningSpeed}
}

type PlayerPersonality struct {
	ID                  uuid.UUID `json:"id"`
	PlayerID            uuid.UUID `json:"player_id"`
	Professionalism     int       `json:"professionalism"`
	Ambition            int       `json:"ambition"`
	Loyalty             int       `json:"loyalty"`
	Ego                 int       `json:"ego"`
	Sociability         int       `json:"sociability"`
	Adaptability        int       `json:"adaptability"`
	Patience            int       `json:"patience"`
	Leadership          int       `json:"leadership"`
	EmotionalVolatility int       `json:"emotional_volatility"`
}

type EmotionalState struct {
	PlayerID uuid.UUID `json:"player_id"`
	State    string    `json:"state"` // happy, content, motivated, frustrated, anxious, angry, homesick, excited, betrayed, ambitious, confident, isolated
	Cause    string    `json:"cause"`
}

// ---- morale & playing time (S06-03) ----

// Appearance is one player's playing-time contribution to a completed match,
// including the engine-derived v1.6 match rating and event tallies. Rating is
// nil for matches completed before the v1.6 attribution pass existed.
type Appearance struct {
	PlayerID uuid.UUID `json:"player_id"`
	Started  bool      `json:"started"`
	Minutes  int       `json:"minutes"`
	Rating   *int      `json:"rating,omitempty"`
	Goals    int       `json:"goals"`
	Assists  int       `json:"assists"`
}

// PlayerMoraleRow is one roster player's morale/role/request read model
// (GET /api/clubs/:id/players). Attributes holds the six category means and
// Overall the position-weighted rating (1..99), both derived by the roster
// read path from the player_attributes EAV.
type PlayerMoraleRow struct {
	Player          *apiref.PlayerRef `json:"player"`
	FirstName       string            `json:"first_name"`
	LastName        string            `json:"last_name"`
	Position        string            `json:"position"`
	SquadRole       string            `json:"squad_role"`
	Morale          float64           `json:"morale"`
	PlayingTimePct  float64           `json:"playing_time_pct"`
	TransferRequest string            `json:"transfer_request_status,omitempty"`
	SquadNumber     *int              `json:"squad_number,omitempty"`
	Attributes      PlayerAttributes  `json:"attributes"`
	Overall         int               `json:"overall"`
	Hidden          *HiddenAttributes `json:"hidden_attributes,omitempty"`
	Dossier         *PlayerDossier    `json:"dossier"`
	// IM40: squad-table columns. Age is in the world's calendar (IM25).
	Nationality *apiref.CountryRef `json:"nationality"`
	DateOfBirth string             `json:"date_of_birth"`
	Age         int                `json:"age"`
	Contract    *RosterContract    `json:"contract"`
	// IM62: lineup-picker columns. Fitness is player_condition.fitness in
	// [0,1] (1 when no row). Available is the same gate SetLineup enforces;
	// UnavailableReason is "injured" or "ineligible" (status, contract, age).
	Fitness           float64 `json:"fitness"`
	Available         bool    `json:"available"`
	UnavailableReason string  `json:"unavailable_reason,omitempty"`
	// IM63: the last five rated appearances (1–10), oldest first. Never nil.
	RecentRatings []int `json:"recent_ratings"`
}

// RosterContract is the active contract summary on a roster row (IM40). The
// roster is own-club only, so this is never a rival's private data.
type RosterContract struct {
	WeeklyWage int64  `json:"weekly_wage"`
	EndDate    string `json:"end_date"`
}

// TransferRequest is the read model of one player transfer request. Internal
// flags (PlayerID/ClubID) stay for engine logic; the wire exposes the nested
// player + club refs (manager_id is the caller's own subject identity).
type TransferRequest struct {
	ID             uuid.UUID          `json:"id"`
	PlayerID       uuid.UUID          `json:"-"`
	ClubID         uuid.UUID          `json:"-"`
	ManagerID      uuid.UUID          `json:"-"`
	Player         *apiref.PlayerRef  `json:"player,omitempty"`
	Club           *apiref.ClubRef    `json:"club,omitempty"`
	Manager        *apiref.ManagerRef `json:"manager,omitempty"`
	Status         string             `json:"status"`
	Reason         string             `json:"reason"`
	CreatedAt      time.Time          `json:"created_at"`
	ResolvedAt     *time.Time         `json:"resolved_at,omitempty"`
	ReassuredUntil *time.Time         `json:"reassured_until,omitempty"`
}

// RelationshipEventRow is one journal row on the player↔manager memory
// (history only; surfaced by S06-04).
type RelationshipEventRow struct {
	EventType      string    `json:"event_type"`
	SentimentDelta int       `json:"sentiment_delta"`
	CreatedAt      time.Time `json:"created_at"`
}

// PlayerMoraleDetail is one player's full morale picture with the "why"
// (GET /api/clubs/:id/players/:playerID).
type PlayerMoraleDetail struct {
	Player             *apiref.PlayerRef      `json:"player"`
	FirstName          string                 `json:"first_name"`
	LastName           string                 `json:"last_name"`
	Position           string                 `json:"position"`
	SquadRole          string                 `json:"squad_role"`
	Morale             float64                `json:"morale"`
	PlayingTimePct     float64                `json:"playing_time_pct"`
	Expectations       []MoraleExpectation    `json:"expectations"`
	Explanation        map[string]any         `json:"explanation"`
	TransferRequest    *TransferRequest       `json:"transfer_request,omitempty"`
	RelationshipEvents []RelationshipEventRow `json:"relationship_history"`
	Hidden             *HiddenAttributes      `json:"hidden_attributes,omitempty"`
	Dossier            *PlayerDossier         `json:"dossier"`
}

// MoraleExpectation contrasts the agreed role vs the whole-season share.
type MoraleExpectation struct {
	Label    string  `json:"label"`
	Expected string  `json:"expected"`
	Current  float64 `json:"current"`
	Status   string  `json:"status"` // satisfied | neutral | unhappy | free
}
