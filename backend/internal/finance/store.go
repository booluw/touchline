package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the persistence layer for the finance module. It exposes two
// styles of method:
//
//   - Write helpers (EnsureAccount, Post) that accept a pgx.Tx managed by
//     the caller so they can participate in the same transaction as the
//     service's event writes.
//
//   - Read helpers that query the pool directly — safe for GET endpoints
//     that never need a write transaction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ---- write helpers (caller manages the transaction) ----

// EnsureAccount creates the finance account for a club if it does not
// exist yet and returns the account id. It is safe to call repeatedly.
func EnsureAccount(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO finance.accounts (world_id, club_id)
		 VALUES ($1, $2)
		 ON CONFLICT (club_id) DO UPDATE SET club_id = finance.accounts.club_id
		 RETURNING id`, worldID, clubID,
	).Scan(&id)
	if err != nil {
		return id, fmt.Errorf("ensure finance account: %w", err)
	}
	return id, nil
}

// Post appends a single ledger entry. dedupKey is optional; when non-empty
// a row with the same (account_id, dedup_key) is silently skipped (ON
// CONFLICT DO NOTHING). The returned bool indicates whether a new row was
// actually inserted — useful for idempotent wage runs that should only
// emit an event on first application.
func Post(ctx context.Context, tx pgx.Tx, accountID uuid.UUID,
	entryType, category string, amount int64,
	description string, relatedEventID *uuid.UUID,
	occurred time.Time, dedupKey string,
) (bool, error) {
	if dedupKey != "" {
		tag, err := tx.Exec(ctx,
			`INSERT INTO finance.ledger_entries
				(account_id, entry_type, category, amount, description, related_event_id, occurred_at, dedup_key)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (account_id, dedup_key) DO NOTHING`,
			accountID, entryType, category, amount, description, relatedEventID, occurred, dedupKey)
		if err != nil {
			return false, fmt.Errorf("post ledger (deduped): %w", err)
		}
		return tag.RowsAffected() > 0, nil
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO finance.ledger_entries
			(account_id, entry_type, category, amount, description, related_event_id, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		accountID, entryType, category, amount, description, relatedEventID, occurred)
	if err != nil {
		return false, fmt.Errorf("post ledger: %w", err)
	}
	return true, nil
}

// ---- read helpers (standalone queries via pool) ----

// Cash returns the current cash balance for a club, or 0 if no account exists.
func (s *Store) Cash(ctx context.Context, clubID uuid.UUID) (int64, error) {
	var cash int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT SUM(CASE WHEN entry_type = 'credit' THEN amount ELSE -amount END)::bigint
			 FROM finance.ledger_entries l
			 JOIN finance.accounts a ON a.id = l.account_id
			 WHERE a.club_id = $1), 0)`, clubID).Scan(&cash)
	if err != nil {
		return 0, fmt.Errorf("cash balance: %w", err)
	}
	return cash, nil
}

// Ledger returns the most recent entries for a club's ledger, ordered by
// occurred_at descending, each with its running balance (IM42). A non-empty
// category narrows the rows; balances still count every entry. limit caps the
// result set.
func (s *Store) Ledger(ctx context.Context, clubID uuid.UUID, category string, limit int) ([]LedgerEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, entry_type, category, amount, description, related_event_id, occurred_at, balance_after
		FROM (
			SELECT l.id, l.entry_type, l.category, l.amount, l.description,
			       l.related_event_id, l.occurred_at,
			       SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)
			           OVER (ORDER BY l.occurred_at, l.id)::bigint AS balance_after
			FROM finance.ledger_entries l
			JOIN finance.accounts a ON a.id = l.account_id
			WHERE a.club_id = $1
		) t
		WHERE $2 = '' OR category = $2
		ORDER BY occurred_at DESC, id DESC
		LIMIT $3`, clubID, category, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger query: %w", err)
	}
	defer rows.Close()

	entries := make([]LedgerEntry, 0, limit)
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.EntryType, &e.Category, &e.Amount,
			&e.Description, &e.RelatedEventID, &e.OccurredAt, &e.BalanceAfter); err != nil {
			return nil, fmt.Errorf("ledger scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// OpenCrisis returns the club's unresolved financial_crisis_states row, or nil
// when none is open (IM42).
func (s *Store) OpenCrisis(ctx context.Context, clubID uuid.UUID) (*HealthState, error) {
	var h HealthState
	err := s.pool.QueryRow(ctx, `
		SELECT stage, started_at FROM finance.financial_crisis_states
		WHERE club_id = $1 AND resolved_at IS NULL
		ORDER BY started_at DESC LIMIT 1`, clubID).Scan(&h.Stage, &h.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open crisis: %w", err)
	}
	return &h, nil
}

// SeasonBreakdown totals season-to-date credits and debits by category, using
// the same window and genesis exclusion as OperatingProfit (IM42).
func (s *Store) SeasonBreakdown(ctx context.Context, clubID uuid.UUID, seasonYear int) (SeasonBreakdown, error) {
	out := SeasonBreakdown{Season: seasonYear, Revenue: []Factor{}, Expenses: []Factor{}}
	if seasonYear == 0 {
		return out, nil
	}
	seasonStart := time.Date(seasonYear, 1, 1, 0, 0, 0, 0, time.UTC)
	rows, err := s.pool.Query(ctx, `
		SELECT l.entry_type, l.category, SUM(l.amount)::bigint
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1 AND l.occurred_at >= $2
		  AND (l.dedup_key IS NULL OR l.dedup_key <> $3)
		GROUP BY l.entry_type, l.category
		ORDER BY 3 DESC, l.category`, clubID, seasonStart, genesisDedupKey)
	if err != nil {
		return out, fmt.Errorf("season breakdown: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			entryType string
			f         Factor
		)
		if err := rows.Scan(&entryType, &f.Label, &f.Amount); err != nil {
			return out, fmt.Errorf("season breakdown scan: %w", err)
		}
		if entryType == "credit" {
			out.Revenue = append(out.Revenue, f)
		} else {
			out.Expenses = append(out.Expenses, f)
		}
	}
	return out, rows.Err()
}

// CashHistory returns the month-end cash balance for every month with ledger
// activity, oldest first (IM42).
func (s *Store) CashHistory(ctx context.Context, clubID uuid.UUID) ([]CashHistoryRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(m, 'YYYY-MM'),
		       SUM(net) OVER (ORDER BY m)::bigint
		FROM (
			SELECT date_trunc('month', l.occurred_at AT TIME ZONE 'UTC') AS m,
			       SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END) AS net
			FROM finance.ledger_entries l
			JOIN finance.accounts a ON a.id = l.account_id
			WHERE a.club_id = $1
			GROUP BY 1
		) t
		ORDER BY m`, clubID)
	if err != nil {
		return nil, fmt.Errorf("cash history: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CashHistoryRow, error) {
		var c CashHistoryRow
		err := r.Scan(&c.Month, &c.Balance)
		return c, err
	})
}

// LedgerCategories returns the net cash flow per category for a club
// (credit − debit), used to compose the summary factors.
func (s *Store) LedgerCategories(ctx context.Context, clubID uuid.UUID) ([]Factor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.category,
		       SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1
		GROUP BY l.category
		ORDER BY l.category`, clubID)
	if err != nil {
		return nil, fmt.Errorf("ledger categories: %w", err)
	}
	defer rows.Close()

	var factors []Factor
	for rows.Next() {
		var f Factor
		if err := rows.Scan(&f.Label, &f.Amount); err != nil {
			return nil, fmt.Errorf("ledger categories scan: %w", err)
		}
		factors = append(factors, f)
	}
	return factors, rows.Err()
}

// OperatingProfit returns the season-to-date operating profit for a club.
// seasonYear is the year that defines the season start (1 Jan). When
// seasonYear is 0 the method returns 0 (no budget seeded yet).
func (s *Store) OperatingProfit(ctx context.Context, clubID uuid.UUID, seasonYear int) (int64, error) {
	if seasonYear == 0 {
		return 0, nil
	}
	seasonStart := time.Date(seasonYear, 1, 1, 0, 0, 0, 0, time.UTC)
	var profit int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint
			 FROM finance.ledger_entries l
			 JOIN finance.accounts a ON a.id = l.account_id
			 WHERE a.club_id = $1 AND l.occurred_at >= $2
			   AND (l.dedup_key IS NULL OR l.dedup_key <> $3)), 0)`,
		clubID, seasonStart, genesisDedupKey).Scan(&profit)
	if err != nil {
		return 0, fmt.Errorf("operating profit: %w", err)
	}
	return profit, nil
}

// Budgets returns the transfer and wage budgets for a club's highest
// seeded season. The map key is the budget_type ('transfer' or 'wage').
func (s *Store) Budgets(ctx context.Context, clubID uuid.UUID) (int, map[string]BudgetLine, error) {
	var season int
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(season), 0) FROM finance.budgets WHERE club_id = $1`, clubID).Scan(&season)
	if err != nil {
		return 0, nil, fmt.Errorf("max budget season: %w", err)
	}
	if season == 0 {
		return 0, nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT budget_type, allocated_amount::bigint, committed_amount::bigint
		FROM finance.budgets WHERE club_id = $1 AND season = $2
		ORDER BY budget_type`, clubID, season)
	if err != nil {
		return season, nil, fmt.Errorf("budgets query: %w", err)
	}
	defer rows.Close()

	out := make(map[string]BudgetLine)
	for rows.Next() {
		var bt string
		var bl BudgetLine
		if err := rows.Scan(&bt, &bl.Allocated, &bl.Committed); err != nil {
			return season, nil, fmt.Errorf("budgets scan: %w", err)
		}
		bl.Season = season
		bl.Available = bl.Allocated - bl.Committed
		out[bt] = bl
	}
	return season, out, rows.Err()
}

// WageCommitments returns the aggregated wage commitment summary for
// currently active contracts. A contract is active when its end_date has
// not passed.
func (s *Store) WageCommitments(ctx context.Context, clubID uuid.UUID) (*CommitmentLine, error) {
	var c CommitmentLine
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(w.weekly_wage)::bigint, 0),
		       COALESCE(SUM(w.weekly_wage)::bigint, 0) * $2
		FROM finance.wage_commitments w
		JOIN player.contracts c ON c.id = w.contract_id
		WHERE w.club_id = $1 AND c.status = 'active' AND w.end_date >= world.club_world_date($1)`,
		clubID, WeeksPerSeason).Scan(&c.Count, &c.WeeklyWage, &c.AnnualWage)
	if err != nil {
		return nil, fmt.Errorf("wage commitments: %w", err)
	}
	return &c, nil
}

// ActiveWageCommitments returns every live commitment in its raw form so
// ApplyMonthlyWages can iterate over it inside a write transaction.
func (s *Store) ActiveWageCommitments(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) ([]activeWage, error) {
	rows, err := tx.Query(ctx, `
		SELECT w.contract_id, c.player_id, w.weekly_wage
		FROM finance.wage_commitments w
		JOIN player.contracts c ON c.id = w.contract_id
		WHERE w.club_id = $1 AND c.status = 'active' AND w.end_date >= world.club_world_date($1)
		ORDER BY c.player_id`, clubID)
	if err != nil {
		return nil, fmt.Errorf("active wage commitments: %w", err)
	}
	defer rows.Close()
	var out []activeWage
	for rows.Next() {
		var a activeWage
		if err := rows.Scan(&a.ContractID, &a.PlayerID, &a.WeeklyWage); err != nil {
			return nil, fmt.Errorf("active wage scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FutureInstallments sums the outstanding installments that a club still
// owes on incoming transfers. It parses transfer.completed_transfers.installments
// (JSONB array of {amount, due_date}) and sums amounts whose due_date is
// in the future. Phase 1 has no rows, so this always returns 0 — the query
// is forward-compatible and will activate automatically when S16 completes.
func (s *Store) FutureInstallments(ctx context.Context, clubID uuid.UUID) (int64, error) {
	// The JSONB array elements are of the form {"amount": <number>, "due_date": "<date>"}.
	// We iterate the arrays in SQL, skipping non-array installments. The amount
	// is cast per element before summing (IM25: SUM over the raw text failed on
	// every call and the swallowed error always reported 0), and due dates are
	// compared with the world's calendar date, not the server's.
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(SUM((elem->>'amount')::bigint), 0)::bigint
		FROM transfer.completed_transfers t
		CROSS JOIN LATERAL jsonb_array_elements(
		    CASE WHEN jsonb_typeof(t.installments) = 'array' THEN t.installments ELSE '[]'::jsonb END) elem
		WHERE t.to_club_id = $1
		  AND (elem->>'due_date')::date > world.club_world_date($1)`, clubID)
	if err != nil {
		// Defensive: malformed JSON in installments or missing columns
		// should not crash the summary.
		return 0, nil
	}
	defer rows.Close()
	var total int64
	if rows.Next() {
		_ = rows.Scan(&total)
	}
	return total, rows.Err()
}

// PlayerDisplayName returns the display_name for a person. It is used
// to build the per-player explanation factors in monthly wage runs.
func (s *Store) PlayerDisplayName(ctx context.Context, playerID uuid.UUID) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `
		SELECT p.display_name
		FROM person.people p
		JOIN player.players pl ON pl.person_id = p.id
		WHERE pl.id = $1`, playerID).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("player display name: %w", err)
	}
	return name, nil
}

// PlayerClubID returns the club_id a player currently belongs to, or
// (uuid.Nil, nil) when no such player row exists — a nonexistent player has
// no club, which the caller maps to ErrPlayerMismatch.
func (s *Store) PlayerClubID(ctx context.Context, playerID uuid.UUID) (uuid.UUID, error) {
	var clubID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(club_id, '00000000-0000-0000-0000-000000000000'::uuid) FROM player.players WHERE id = $1`,
		playerID).Scan(&clubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("player club: %w", err)
	}
	return clubID, nil
}

// AccountID returns the finance account id for a club, or (uuid.Nil, nil) if
// the club has no account yet (pre-bootstrap worlds).
func (s *Store) AccountID(ctx context.Context, clubID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM finance.accounts WHERE club_id = $1`, clubID).Scan(&id)
	if err == pgx.ErrNoRows {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("account id: %w", err)
	}
	return id, nil
}

// PlayerName is a convenience wrapper that returns the display_name for
// a player or "Unknown" on error.
func (s *Store) PlayerName(ctx context.Context, playerID uuid.UUID) string {
	name, err := s.PlayerDisplayName(ctx, playerID)
	if err != nil {
		return "Unknown"
	}
	return name
}
