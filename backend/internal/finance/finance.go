package finance

import "github.com/google/uuid"

type LedgerEntry struct {
	ID          uuid.UUID `json:"id"`
	WorldID     uuid.UUID `json:"world_id"`
	ClubID      uuid.UUID `json:"club_id"`
	Category    string    `json:"category"` // revenue, expense
	Type        string    `json:"type"`     // ticket_sales, sponsorship, wages, transfer_fee, etc.
	Amount      int64     `json:"amount"`   // positive = income, negative = expense
	Description string    `json:"description"`
	EventID     *uuid.UUID `json:"event_id,omitempty"` // links to world.events for explainability
	CreatedAt   string    `json:"created_at"`
}

type Budget struct {
	ID               uuid.UUID `json:"id"`
	ClubID           uuid.UUID `json:"club_id"`
	WorldID          uuid.UUID `json:"world_id"`
	TransferBudget   int64     `json:"transfer_budget"`
	WageBudget       int64     `json:"wage_budget"`
	AvailableCash    int64     `json:"available_cash"` // derived from SUM(ledger_entries)
}

type WageCommitment struct {
	ID        uuid.UUID `json:"id"`
	PlayerID  uuid.UUID `json:"player_id"`
	ClubID    uuid.UUID `json:"club_id"`
	WeeklyWage int64    `json:"weekly_wage"`
}

type Service interface {
	GetLedger(clubID uuid.UUID) ([]*LedgerEntry, error)
	GetBudget(clubID uuid.UUID) (*Budget, error)
	AddEntry(entry *LedgerEntry) error
	GetCashBalance(clubID uuid.UUID) (int64, error)
}
