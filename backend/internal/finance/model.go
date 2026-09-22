package finance

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// Event kinds emitted by the finance module.
const (
	EventWagePosted        = "WAGE_POSTED"
	EventContractCommitted = "CONTRACT_COMMITTED"
)

// Genesis constants used during world bootstrap (finance-numerics.md).
const (
	OpeningCapital = int64(40_000_000) // one-off other credit
	TransferBudget = int64(10_000_000) // per-season transfer budget allocation
	WageBudget     = int64(25_000_000) // per-season wage budget allocation
	WeeksPerMonth  = 4                 // monthly posting = 4 × weekly_wage
	WeeksPerSeason = 52                // annualization factor for commitments

	// genesisDedupKey marks the one-off opening-capital credit so derived
	// metrics (operating profit) can exclude owner capital injections.
	genesisDedupKey = "genesis:opening_capital"
)

// Sentinel errors.
var (
	ErrClubNotFound    = errors.New("club not found in a playable world")
	ErrWorldNotActive  = errors.New("world is not active")
	ErrNotOwned        = errors.New("manager does not own this club")
	ErrPlayerMismatch  = errors.New("player is not registered to this club")
	ErrInvalidContract = errors.New("invalid contract terms")
	ErrNoAccount       = errors.New("finance account does not exist for this club")
)

// Actor identifies the caller of a finance mutation.
type Actor struct {
	ManagerID   uuid.UUID
	IsPolicyBot bool
}

// ---------- summary ----------

// FinanceSummary is the top-level read model returned by the finances endpoint.
type FinanceSummary struct {
	Currency                string         `json:"currency"`
	Cash                    int64          `json:"cash"`
	OperatingProfit         int64          `json:"operating_profit"`
	TransferBudget          BudgetLine     `json:"transfer_budget"`
	WageBudget              BudgetLine     `json:"wage_budget"`
	WageCommitments         CommitmentLine `json:"wage_commitments"`
	CommittedSpending       int64          `json:"committed_spending"`
	ProjectedRevenue        int64          `json:"projected_revenue"`
	ProjectedYearEndBalance int64          `json:"projected_year_end_balance"`
	FutureInstallments      int64          `json:"future_installments"`
	Debt                    int64          `json:"debt"`
	Factors                 []Factor       `json:"factors"`
}

// Factor is a labeled delta that explains where a financial total came from.
type Factor struct {
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
}

// BudgetLine describes one seasonal budget envelope.
type BudgetLine struct {
	Season    int   `json:"season"`
	Allocated int64 `json:"allocated"`
	Committed int64 `json:"committed"`
	Available int64 `json:"available"`
}

// CommitmentLine summarizes wage obligations for the club.
type CommitmentLine struct {
	Count      int   `json:"count"`
	WeeklyWage int64 `json:"weekly_wage"`
	AnnualWage int64 `json:"annual_wage"`
}

// ---------- ledger ----------

// LedgerEntry is one row of the append-only finance.ledger_entries table,
// returned as JSON by the ledger endpoint. Amount is always positive; the
// sign is carried by EntryType.
type LedgerEntry struct {
	ID             uuid.UUID  `json:"id"`
	EntryType      string     `json:"entry_type"`
	Category       string     `json:"category"`
	Amount         int64      `json:"amount"`
	Description    string     `json:"description"`
	RelatedEventID *uuid.UUID `json:"related_event_id,omitempty"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

// ---------- contracts ----------

// ContractInput carries the terms of a contract to register for a player.
type ContractInput struct {
	PlayerID           uuid.UUID
	WeeklyWage         int64
	SigningBonus       int64
	StartDate          time.Time
	EndDate            time.Time
	ReleaseClause      *int64
	PlayingTimePromise string
}

// ContractView is a read-only representation of a player contract for the API.
// Flat ids stay internal; the wire carries a nested player ref.
type ContractView struct {
	ID            uuid.UUID         `json:"id"`
	PlayerID      uuid.UUID         `json:"-"`
	PlayerName    string            `json:"-"`
	Player        *apiref.PlayerRef `json:"player"`
	WeeklyWage    int64             `json:"weekly_wage"`
	SigningBonus  int64             `json:"signing_bonus"`
	StartDate     string            `json:"start_date"`
	EndDate       string            `json:"end_date"`
	ReleaseClause *int64            `json:"release_clause,omitempty"`
	Status        string            `json:"status"`
}

// ---------- bootstrap ----------

// ContractSeed provides the data the wage formula needs for one player at
// world bootstrap time.
type ContractSeed struct {
	PlayerID   uuid.UUID
	Position   string
	Age        int
	Attributes map[string]int
}

// activeWage is the internal representation of a live wage commitment used
// by ApplyMonthlyWages (not exposed via JSON).
type activeWage struct {
	ContractID uuid.UUID
	PlayerID   uuid.UUID
	WeeklyWage int64
}
