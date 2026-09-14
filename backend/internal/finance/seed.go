package finance

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BootstrapClub mints a club's financial genesis inside the caller's
// transaction: the one-off opening-capital credit, the season's transfer
// and wage budgets, and a starter contract + wage commitment for every
// player in the squad. It emits no events of its own — the numbers fold
// into the CLUB_CREATED audit trail (see GenerateAIClub).
//
// The operation is idempotent: ON CONFLICT guards make re-running it (a
// redelivered bootstrap event) a no-op rather than a double-opening.
func BootstrapClub(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID, seasonStart time.Time, seeds []ContractSeed) error {
	accountID, err := EnsureAccount(ctx, tx, worldID, clubID)
	if err != nil {
		return err
	}

	if _, err := Post(ctx, tx, accountID, "credit", "other", OpeningCapital,
		"Opening capital (world genesis)", nil, seasonStart, genesisDedupKey); err != nil {
		return fmt.Errorf("opening capital: %w", err)
	}

	for _, spec := range []struct {
		kind   string
		amount int64
	}{
		{"transfer", TransferBudget},
		{"wage", WageBudget},
	} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO finance.budgets (club_id, season, budget_type, allocated_amount)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (club_id, season, budget_type) DO NOTHING`,
			clubID, seasonStart.Year(), spec.kind, spec.amount); err != nil {
			return fmt.Errorf("budget %s: %w", spec.kind, err)
		}
	}

	for _, seed := range seeds {
		weekly := WeeklyWage(seed.Position, MeanAttribute(seed.Attributes))

		var contractID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO player.contracts
				(player_id, club_id, weekly_wage, signing_bonus, start_date, end_date, status)
			VALUES ($1, $2, $3, 0, $4, $5, 'active')
			RETURNING id`,
			seed.PlayerID, clubID, weekly, seasonStart,
			seasonStart.AddDate(ContractSeasons(seed.Age), 0, 0),
		).Scan(&contractID); err != nil {
			return fmt.Errorf("insert starter contract (player %s): %w", seed.PlayerID, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO finance.wage_commitments (contract_id, club_id, weekly_wage, start_date, end_date)
			VALUES ($1, $2, $3, $4, $5)`,
			contractID, clubID, weekly, seasonStart,
			seasonStart.AddDate(ContractSeasons(seed.Age), 0, 0),
		); err != nil {
			return fmt.Errorf("insert wage commitment (player %s): %w", seed.PlayerID, err)
		}
	}

	return nil
}
