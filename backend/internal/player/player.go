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

type PlayerAttributes struct {
	ID       uuid.UUID `json:"id"`
	PlayerID uuid.UUID `json:"player_id"`
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

type PlayerHiddenTraits struct {
	ID                   uuid.UUID `json:"id"`
	PlayerID             uuid.UUID `json:"player_id"`
	Potential            int       `json:"potential"`
	Consistency          int       `json:"consistency"`
	InjurySusceptibility int       `json:"injury_susceptibility"`
	Adaptability         int       `json:"adaptability"`
	Professionalism      int       `json:"professionalism"`
	Ambition             int       `json:"ambition"`
	Loyalty              int       `json:"loyalty"`
	Temperament          int       `json:"temperament"`
	PressureHandling     int       `json:"pressure_handling"`
	LearningSpeed        int       `json:"learning_speed"`
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
// (GET /api/clubs/:id/players).
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
}

// MoraleExpectation contrasts the agreed role vs the whole-season share.
type MoraleExpectation struct {
	Label    string  `json:"label"`
	Expected string  `json:"expected"`
	Current  float64 `json:"current"`
	Status   string  `json:"status"` // satisfied | neutral | unhappy | free
}
