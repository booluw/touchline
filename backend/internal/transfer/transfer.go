package transfer

import "github.com/google/uuid"

type TransferListing struct {
	ID            uuid.UUID `json:"id"`
	WorldID       uuid.UUID `json:"world_id"`
	PlayerID      uuid.UUID `json:"player_id"`
	SellerClubID  uuid.UUID `json:"seller_club_id"`
	AskingPrice   int64     `json:"asking_price"`
	Status        string    `json:"status"` // active, accepted, rejected, expired
}

type Bid struct {
	ID            uuid.UUID `json:"id"`
	WorldID       uuid.UUID `json:"world_id"`
	ListingID     uuid.UUID `json:"listing_id"`
	BuyerClubID   uuid.UUID `json:"buyer_club_id"`
	SellerClubID  uuid.UUID `json:"seller_club_id"`
	PlayerID      uuid.UUID `json:"player_id"`
	Amount        int64     `json:"amount"`
	Status        string    `json:"status"` // pending, accepted, rejected, countered, withdrawn
}

type Negotiation struct {
	ID      uuid.UUID `json:"id"`
	BidID   uuid.UUID `json:"bid_id"`
	Status  string    `json:"status"` // active, completed, collapsed
}

type Clause struct {
	ID           uuid.UUID `json:"id"`
	TransferID   uuid.UUID `json:"transfer_id"`
	Type         string    `json:"type"` // sell_on, buy_back, release
	Percentage   int       `json:"percentage,omitempty"`
	Amount       int64     `json:"amount,omitempty"`
	TriggerAfter string    `json:"trigger_after,omitempty"`
}

type Loan struct {
	ID              uuid.UUID `json:"id"`
	WorldID         uuid.UUID `json:"world_id"`
	PlayerID        uuid.UUID `json:"player_id"`
	ParentClubID    uuid.UUID `json:"parent_club_id"`
	LoanClubID      uuid.UUID `json:"loan_club_id"`
	StartDate       string    `json:"start_date"`
	EndDate         string    `json:"end_date"`
	LoanFee         int64     `json:"loan_fee"`
	WageContribution int      `json:"wage_contribution"` // percentage
}

type Service interface {
	GetActiveListings(worldID uuid.UUID) ([]*TransferListing, error)
	PlaceBid(bid *Bid) error
	RespondToBid(bidID uuid.UUID, response string) error
}
