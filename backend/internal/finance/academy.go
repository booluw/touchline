package finance

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AcademyProspectSeed is one youth intake player to be signed to the club's
// 3-year academy contract during intake. WeeklyWage already carries the
// tier-based academy wage (docs/design/academy-numerics.md §2); the caller
// (academy service) derives it, finance just lands the contract.
type AcademyProspectSeed struct {
	PlayerID      uuid.UUID
	WeeklyWage    int
	ContractYears int
}

// SignAcademyProspects signs every intake prospect to an academy youth
// contract inside the caller's transaction: the active player.contracts row
// plus its finance.wage_commitments twin, mirroring BootstrapClub's starter
// contracts (S04-05). The operation is idempotent per player via the
// contracts_one_active_youth_per_player partial unique index (0044) — a
// redelivered intake cannot double-sign. It emits no events of its own; the
// caller's ACADEMY_INTAKE row records the whole intake.
func SignAcademyProspects(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID, seasonStart time.Time, seeds []AcademyProspectSeed) error {
	if _, err := EnsureAccount(ctx, tx, worldID, clubID); err != nil {
		return err
	}
	if len(seeds) == 0 {
		return nil
	}
	for _, seed := range seeds {
		if seed.ContractYears <= 0 {
			seed.ContractYears = YouthContractYears
		}
		end := seasonStart.AddDate(seed.ContractYears, 0, 0)

		var contractID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO player.contracts
				(player_id, club_id, contract_type, weekly_wage, signing_bonus, start_date, end_date, status)
			VALUES ($1, $2, 'youth', $3, 0, $4, $5, 'active')
			ON CONFLICT (player_id) WHERE contract_type = 'youth' DO UPDATE SET
				club_id = EXCLUDED.club_id,
				weekly_wage = EXCLUDED.weekly_wage,
				start_date = EXCLUDED.start_date,
				end_date = EXCLUDED.end_date,
				status = 'active'
			RETURNING id`,
			seed.PlayerID, clubID, seed.WeeklyWage, seasonStart, end,
		).Scan(&contractID); err != nil {
			return fmt.Errorf("insert youth contract (player %s): %w", seed.PlayerID, err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO finance.wage_commitments (contract_id, club_id, weekly_wage, start_date, end_date)
			VALUES ($1, $2, $3, $4, $5)`,
			contractID, clubID, seed.WeeklyWage, seasonStart, end,
		); err != nil {
			return fmt.Errorf("insert youth wage commitment (player %s): %w", seed.PlayerID, err)
		}
	}
	return nil
}
