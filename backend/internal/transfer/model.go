// Package transfer is the S06-01 transfer market engine: listings, bids,
// counter-negotiation rounds, the AI counterpart policy, and the atomic
// completion that moves player/contract/ledger/history in one transaction.
//
// The persistence layout ships in migrations/0008_transfer; one bids row is a
// negotiation thread and every round (initial offer + each counter) appends a
// transfer.negotiations row. The latest round is always the live offer.
package transfer

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/touchline/backend/pkg/apiref"
)

// Event kinds emitted by the transfer module (world.events.event_type).
const (
	EventListed              = "PLAYER_LISTED"
	EventListingWithdrawn    = "PLAYER_LISTING_WITHDRAWN"
	EventBidPlaced           = "BID_PLACED"
	EventBidAccepted         = "BID_ACCEPTED"
	EventBidRejected         = "BID_REJECTED"
	EventBidCountered        = "BID_COUNTERED"
	EventBidWithdrawn        = "BID_WITHDRAWN"
	EventBidExpired          = "BID_EXPIRED"
	EventTransferCompleted   = "TRANSFER_COMPLETED"
	EventValuationsRefreshed = "MARKET_VALUATIONS_REFRESHED"
)

// Sentinel errors.
var (
	ErrListingNotFound       = errors.New("listing not found")
	ErrBidNotFound           = errors.New("bid not found")
	ErrPlayerNotFound        = errors.New("player not found")
	ErrClubNotFound          = errors.New("club not found")
	ErrWorldNotPlayable      = errors.New("world is not playable")
	ErrNoActiveClub          = errors.New("manager has no active club")
	ErrNotClubMember         = errors.New("actor is not the club's manager")
	ErrPlayerNotTransferable = errors.New("player cannot be listed or sold")
	ErrDuplicateListing      = errors.New("player already has an active listing")
	ErrSelfBid               = errors.New("a club cannot bid on its own player")
	ErrInvalidTerms          = errors.New("invalid transfer terms")
	ErrInsufficientFunds     = errors.New("club has insufficient funds for the offer")
	ErrBidResolved           = errors.New("bid already resolved")
	ErrBidExpired            = errors.New("bid expired")
	ErrNotBidParticipant     = errors.New("actor is not a participant in this bid")
	ErrNotListingClub        = errors.New("action allowed only by the selling club")
	ErrCounterLimitReached   = errors.New("negotiation rounds exhausted")
	ErrPlayerWorldMismatch   = errors.New("player and caller are in different worlds")
	ErrWorldMismatch         = errors.New("cross-world operation rejected")
	ErrDuplicateOpenBid      = errors.New("club already has an open bid on this player")
	ErrInvalidActionForRole  = errors.New("action not allowed for this participant")
)

// Actor identifies the caller of a transfer mutation. AI club activity stamps
// its policy-bot manager (actor_type 'policy_bot') so the audit trail always
// knows who acted.
type Actor struct {
	ManagerID   uuid.UUID
	IsPolicyBot bool
}

func (a Actor) actorTypeAndID() (string, uuid.UUID) {
	if a.IsPolicyBot {
		return "policy_bot", a.ManagerID
	}
	return "manager", a.ManagerID
}

// Terms is the player-contract tail of an offer ("terms ride with the bid",
// S06-01 decision). It is stored as transfer.negotiations.terms JSONB and the
// single source of truth for the completion transaction.
type Terms struct {
	Fee                  int64  `json:"fee"`
	WeeklyWage           int64  `json:"weekly_wage"`
	ContractLengthMonths int    `json:"contract_length_months"`
	SigningBonus         int64  `json:"signing_bonus,omitempty"`
	ReleaseClause        *int64 `json:"release_clause,omitempty"`
	SellOnPercentage     *int   `json:"sell_on_percentage,omitempty"`
	BuyBackAmount        *int64 `json:"buy_back_amount,omitempty"`
}

// Clause mirrors one row of transfer.clauses, derived from an agreed Terms
// on completion.
type Clause struct {
	ClauseType        string          `json:"clause_type"` // sell_on | buy_back
	Percentage        *int            `json:"percentage,omitempty"`
	Amount            *int64          `json:"amount,omitempty"`
	BeneficiaryClubID *uuid.UUID      `json:"-"`
	BeneficiaryClub   *apiref.ClubRef `json:"beneficiary_club,omitempty"`
}

// Listing is a listing row with the player context a market screen needs.
type Listing struct {
	ID              uuid.UUID         `json:"id"`
	WorldID         uuid.UUID         `json:"world_id"`
	PlayerID        uuid.UUID         `json:"-"`
	PlayerName      string            `json:"-"`
	Player          *apiref.PlayerRef `json:"player"`
	Position        string            `json:"position"`
	Age             int               `json:"age"`
	MarketValue     int64             `json:"market_value"`
	ListingClubID   uuid.UUID         `json:"-"`
	ListingClubName string            `json:"-"`
	ListingClub     *apiref.ClubRef   `json:"listing_club"`
	AskingPrice     *int64            `json:"asking_price,omitempty"`
	ListingType     string            `json:"listing_type"`
	Status          string            `json:"status"`
	BidCount        int               `json:"bid_count"`
	LatestBid       *Bid              `json:"latest_bid,omitempty"`
	ListedAt        time.Time         `json:"listed_at"`
}

// ListingType values (transfer.listings.listing_type CHECK).
const (
	ListingOpenToOffers    = "open_to_offers"
	ListingActivelyShopped = "actively_shopped"
	ListingLoanAvailable   = "loan_available"
)

// ReasonableListingTypes is the accepted listing_type vocabulary.
var ReasonableListingTypes = map[string]bool{
	ListingOpenToOffers:    true,
	ListingActivelyShopped: true,
	ListingLoanAvailable:   true,
}

// Bid is the read model of one negotiation thread: the current offer (latest
// round) plus the lifecycle status.
type Bid struct {
	ID                   uuid.UUID         `json:"id"`
	WorldID              uuid.UUID         `json:"world_id"`
	ListingID            *uuid.UUID        `json:"listing_id,omitempty"`
	PlayerID             uuid.UUID         `json:"-"`
	PlayerName           string            `json:"-"`
	Player               *apiref.PlayerRef `json:"player"`
	BiddingClubID        uuid.UUID         `json:"-"`
	BiddingClubName      string            `json:"-"`
	BiddingClub          *apiref.ClubRef   `json:"bidding_club"`
	SellingClubID        uuid.UUID         `json:"-"`
	SellingClubName      string            `json:"-"`
	SellingClub          *apiref.ClubRef   `json:"selling_club"`
	Fee                  int64             `json:"fee"`
	WeeklyWage           int64             `json:"weekly_wage"`
	ContractLengthMonths int               `json:"contract_length_months"`
	SigningBonus         int64             `json:"signing_bonus,omitempty"`
	ReleaseClause        *int64            `json:"release_clause,omitempty"`
	Round                int               `json:"round"`
	ProposedBy           string            `json:"proposed_by"` // buying_club | selling_club
	Status               string            `json:"status"`
	CreatedAt            time.Time         `json:"created_at"`
	RespondedAt          *time.Time        `json:"responded_at,omitempty"`
}

// Bid status values (transfer.bids.status CHECK).
const (
	BidStatusPending   = "pending"
	BidStatusAccepted  = "accepted"
	BidStatusRejected  = "rejected"
	BidStatusCountered = "countered"
	BidStatusWithdrawn = "withdrawn"
	BidStatusExpired   = "expired"
)

// NegotiationOpen reports whether a bid is awaiting a response (still on the
// market). Before the expiry sweep they count for AI activity and completion.
func (b *Bid) NegotiationOpen() bool {
	return b.Status == BidStatusPending || b.Status == BidStatusCountered
}

// Proprietor names used in transfer.negotiations.proposed_by.
const (
	ProposedByBuyingClub  = "buying_club"
	ProposedBySellingClub = "selling_club"
)

// CompletedTransfer is one executed permanent transfer.
type CompletedTransfer struct {
	ID             uuid.UUID         `json:"id"`
	WorldID        uuid.UUID         `json:"world_id"`
	BidID          uuid.UUID         `json:"bid_id"`
	PlayerID       uuid.UUID         `json:"-"`
	PlayerName     string            `json:"-"`
	Player         *apiref.PlayerRef `json:"player"`
	FromClubID     uuid.UUID         `json:"-"`
	FromClubName   string            `json:"-"`
	FromClub       *apiref.ClubRef   `json:"from_club"`
	ToClubID       uuid.UUID         `json:"-"`
	ToClubName     string            `json:"-"`
	ToClub         *apiref.ClubRef   `json:"to_club"`
	Fee            int64             `json:"fee"`
	Clauses        []Clause          `json:"clauses,omitempty"`
	RelatedEventID uuid.UUID         `json:"related_event_id"`
	CompletedAt    time.Time         `json:"completed_at"`
}

// BidInput is the payload of POST /api/transfers/bids. A bid targets either a
// specific listing or a player directly.
type BidInput struct {
	ListingID *uuid.UUID `json:"listing_id,omitempty"`
	PlayerID  *uuid.UUID `json:"player_id,omitempty"`
	Terms     Terms      `json:"terms"`
}

// RespondAction of POST /api/transfers/bids/:id/respond.
const (
	RespondAccept  = "accept"
	RespondReject  = "reject"
	RespondCounter = "counter"
)

// ValidRespondActions is the accepted action vocabulary.
var ValidRespondActions = map[string]bool{
	RespondAccept:  true,
	RespondReject:  true,
	RespondCounter: true,
}
