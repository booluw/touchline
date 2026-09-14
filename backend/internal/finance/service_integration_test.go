//go:build integration

package finance_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/explanation"
)

// bootstrappedClub provisions a world, bootstraps a starter AI club, and
// launches it so the finance module sees a playable world. It returns the
// pool, the finance service, the world id, the club id, and the club's
// policy-bot manager id.
func bootstrappedClub(t *testing.T) (*pgxpool.Pool, *finance.Service, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, "fin-boot")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := internalbootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Finance United", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}
	return pool, finance.NewService(pool, nil), w.ID, res.ClubID, res.ManagerID
}

func TestBootstrapGenesisMintsLedgerBudgetsAndContracts(t *testing.T) {
	pool, svc, _, clubID, _ := bootstrappedClub(t)
	ctx := context.Background()

	var accountID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM finance.accounts WHERE club_id = $1`, clubID).Scan(&accountID); err != nil {
		t.Fatalf("finance account missing: %v", err)
	}

	var cash int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint, 0)
		FROM finance.ledger_entries l WHERE l.account_id = $1`, accountID).Scan(&cash); err != nil {
		t.Fatalf("ledger cash: %v", err)
	}
	if cash != finance.OpeningCapital {
		t.Fatalf("cash = %d, want %d", cash, finance.OpeningCapital)
	}

	// The genesis credit must be a single, dedup-keyed posting.
	var genesisRows int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM finance.ledger_entries
		WHERE account_id = $1 AND dedup_key = 'genesis:opening_capital'`, accountID).Scan(&genesisRows); err != nil {
		t.Fatalf("genesis rows: %v", err)
	}
	if genesisRows != 1 {
		t.Fatalf("genesis postings = %d, want 1", genesisRows)
	}

	for budgetType, want := range map[string]int64{"transfer": finance.TransferBudget, "wage": finance.WageBudget} {
		var got int64
		if err := pool.QueryRow(ctx, `
			SELECT allocated_amount::bigint FROM finance.budgets
			WHERE club_id = $1 AND budget_type = $2`, clubID, budgetType).Scan(&got); err != nil {
			t.Fatalf("budget %s: %v", budgetType, err)
		}
		if got != want {
			t.Fatalf("budget %s = %d, want %d", budgetType, got, want)
		}
	}

	for _, table := range []string{"player.contracts", "finance.wage_commitments"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM `+table+` WHERE club_id = $1`, clubID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 24 {
			t.Fatalf("%s = %d, want 24", table, n)
		}
	}

	// The summary composes the same genesis into explained numbers.
	sum, err := svc.GetSummary(ctx, clubID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Cash != finance.OpeningCapital {
		t.Fatalf("summary cash = %d, want %d", sum.Cash, finance.OpeningCapital)
	}
	if sum.OperatingProfit != 0 {
		t.Fatalf("operating profit = %d, want 0 (genesis capital is excluded)", sum.OperatingProfit)
	}
	if sum.TransferBudget.Allocated != finance.TransferBudget ||
		sum.TransferBudget.Available != finance.TransferBudget {
		t.Fatalf("transfer budget wrong: %+v", sum.TransferBudget)
	}
	if sum.WageBudget.Allocated != finance.WageBudget {
		t.Fatalf("wage budget wrong: %+v", sum.WageBudget)
	}
	if sum.WageCommitments.Count != 24 || sum.WageCommitments.WeeklyWage <= 0 {
		t.Fatalf("wage commitments wrong: %+v", sum.WageCommitments)
	}
	if sum.WageCommitments.AnnualWage != sum.WageCommitments.WeeklyWage*finance.WeeksPerSeason {
		t.Fatalf("annual wage = %d, want weekly*52", sum.WageCommitments.AnnualWage)
	}
	dsum := int64(0)
	for _, f := range sum.Factors {
		dsum += f.Amount
	}
	if dsum != sum.Cash {
		t.Fatalf("factors sum to %d, want cash %d (%+v)", dsum, sum.Cash, sum.Factors)
	}
	if sum.CommittedSpending != sum.WageCommitments.AnnualWage {
		t.Fatalf("committed spending = %d, want annualized wages %d", sum.CommittedSpending, sum.WageCommitments.AnnualWage)
	}
	if sum.ProjectedRevenue != 0 || sum.Debt != 0 || sum.FutureInstallments != 0 {
		t.Fatalf("phase-1 absent rows must be zero: %+v", sum)
	}
	if sum.ProjectedYearEndBalance != sum.Cash-sum.CommittedSpending {
		t.Fatalf("projected year end = %d, want %d", sum.ProjectedYearEndBalance, sum.Cash-sum.CommittedSpending)
	}
}

func TestApplyMonthlyWagesPostsEventsAndIsIdempotent(t *testing.T) {
	pool, svc, worldID, clubID, _ := bootstrappedClub(t)
	ctx := context.Background()

	var weeklySum int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(w.weekly_wage)::bigint, 0)
		FROM finance.wage_commitments w
		JOIN player.contracts c ON c.id = w.contract_id
		WHERE w.club_id = $1 AND c.status = 'active'`, clubID).Scan(&weeklySum); err != nil {
		t.Fatalf("sum weekly wages: %v", err)
	}
	wantMonthly := finance.WeeksPerMonth * weeklySum

	res, err := svc.ApplyMonthlyWages(ctx, worldID, 17)
	if err != nil {
		t.Fatalf("apply monthly wages: %v", err)
	}
	if res.Clubs != 1 || res.Postings != 24 || res.Total != wantMonthly {
		t.Fatalf("wage run = %+v, want clubs=1 postings=24 total=%d", res, wantMonthly)
	}

	var cash int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1`, clubID).Scan(&cash); err != nil {
		t.Fatalf("ledger cash: %v", err)
	}
	if cash != finance.OpeningCapital-wantMonthly {
		t.Fatalf("cash = %d, want %d", cash, finance.OpeningCapital-wantMonthly)
	}

	// One WAGE_POSTED event, with a self-consistent explanation.
	var eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = 'WAGE_POSTED' AND world_tick = 17`, worldID).Scan(&eventCount); err != nil {
		t.Fatalf("count wage events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("WAGE_POSTED events = %d, want 1", eventCount)
	}
	var expJSON []byte
	if err := pool.QueryRow(ctx, `
		SELECT explanation FROM world.events
		WHERE world_id = $1 AND event_type = 'WAGE_POSTED' AND world_tick = 17`, worldID).Scan(&expJSON); err != nil {
		t.Fatalf("read explanation: %v", err)
	}
	var exp explanation.Explanation
	if err := json.Unmarshal(expJSON, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if len(exp.Factors) != 24 {
		t.Fatalf("explanation factors = %d, want 24", len(exp.Factors))
	}
	if err := exp.Validate(); err != nil {
		t.Fatalf("explanation validation: %v", err)
	}
	if exp.Score != -int(wantMonthly) {
		t.Fatalf("explanation score = %d, want %d", exp.Score, -int(wantMonthly))
	}

	// Redelivering the same tick is a no-op: no entries, no events.
	res2, err := svc.ApplyMonthlyWages(ctx, worldID, 17)
	if err != nil {
		t.Fatalf("replay monthly wages: %v", err)
	}
	if res2.Clubs != 0 || res2.Postings != 0 || res2.Total != 0 {
		t.Fatalf("redelivered tick mutated state: %+v", res2)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = 'WAGE_POSTED' AND world_tick = 17`, worldID).Scan(&eventCount); err != nil {
		t.Fatalf("recount wage events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("WAGE_POSTED events after replay = %d, want 1", eventCount)
	}

	// A fresh tick posts again and the ledger moves exactly once more.
	res3, err := svc.ApplyMonthlyWages(ctx, worldID, 18)
	if err != nil {
		t.Fatalf("apply next month: %v", err)
	}
	if res3.Postings != 24 || res3.Total != wantMonthly {
		t.Fatalf("second month = %+v", res3)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1`, clubID).Scan(&cash); err != nil {
		t.Fatalf("ledger cash 2: %v", err)
	}
	if cash != finance.OpeningCapital-2*wantMonthly {
		t.Fatalf("cash after two months = %d, want %d", cash, finance.OpeningCapital-2*wantMonthly)
	}
}

// TestReconcileCashEqualsFactorSum is the AC-5 accounting-identity property:
// no engine operation may silently create or destroy money. Every posting is
// a real credit or debit, so cash must always equal the real ledger and the
// summary's explained factors must always sum back to it.
func TestReconcileCashEqualsFactorSum(t *testing.T) {
	pool, svc, worldID, clubID, _ := bootstrappedClub(t)
	ctx := context.Background()

	for tick := int64(1); tick <= 3; tick++ {
		if _, err := svc.ApplyMonthlyWages(ctx, worldID, tick); err != nil {
			t.Fatalf("wage run %d: %v", tick, err)
		}
	}

	var ledgerCash int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1`, clubID).Scan(&ledgerCash); err != nil {
		t.Fatalf("ledger cash: %v", err)
	}

	sum, err := svc.GetSummary(ctx, clubID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Cash != ledgerCash {
		t.Fatalf("summary cash = %d, ledger cash = %d", sum.Cash, ledgerCash)
	}
	factorSum := int64(0)
	for _, f := range sum.Factors {
		factorSum += f.Amount
	}
	if factorSum != ledgerCash {
		t.Fatalf("factors sum to %d, want ledger cash %d (%+v)", factorSum, ledgerCash, sum.Factors)
	}
}

func TestApplyMonthlyWagesIsWorldScoped(t *testing.T) {
	pool, svc, worldID, clubID, _ := bootstrappedClub(t)
	ctx := context.Background()

	otherWorld, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "fin-other")
	if err != nil {
		t.Fatalf("create other world: %v", err)
	}
	other, err := internalbootstrap.NewService(pool, nil).BootstrapWorld(ctx, otherWorld.ID, "Other Utd", "")
	if err != nil {
		t.Fatalf("bootstrap other world: %v", err)
	}
	if _, err := internalworld.NewService(pool, nil).SetStatus(ctx, otherWorld.ID, "active"); err != nil {
		t.Fatalf("launch other world: %v", err)
	}

	if _, err := svc.ApplyMonthlyWages(ctx, worldID, 9); err != nil {
		t.Fatalf("wage run world 1: %v", err)
	}

	var otherWages int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'WAGE_POSTED'`,
		otherWorld.ID).Scan(&otherWages); err != nil {
		t.Fatalf("count other-world wages: %v", err)
	}
	if otherWages != 0 {
		t.Fatalf("other world received %d wage events", otherWages)
	}
	var otherCash int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1`, other.ClubID).Scan(&otherCash); err != nil {
		t.Fatalf("other-world cash: %v", err)
	}
	if otherCash != finance.OpeningCapital {
		t.Fatalf("other world cash = %d, want untouched %d", otherCash, finance.OpeningCapital)
	}
	if clubID == other.ClubID {
		t.Fatal("clubs must differ")
	}
}

func TestRegisterContractCreatesCommitmentAndEvent(t *testing.T) {
	pool, svc, worldID, clubID, botID := bootstrappedClub(t)
	ctx := context.Background()

	var playerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 ORDER BY id LIMIT 1`, clubID).Scan(&playerID); err != nil {
		t.Fatalf("pick player: %v", err)
	}

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2029, 6, 30, 0, 0, 0, 0, time.UTC)
	vc, err := svc.RegisterContract(ctx, finance.Actor{ManagerID: botID, IsPolicyBot: true}, clubID,
		finance.ContractInput{PlayerID: playerID, WeeklyWage: 12345, StartDate: start, EndDate: end})
	if err != nil {
		t.Fatalf("register contract: %v", err)
	}
	if vc.WeeklyWage != 12345 || vc.PlayerID != playerID || vc.Status != "active" {
		t.Fatalf("contract view wrong: %+v", vc)
	}

	for _, table := range []string{"player.contracts", "finance.wage_commitments"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM `+table+` WHERE club_id = $1`, clubID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 25 {
			t.Fatalf("%s = %d, want 25 (24 seeded + 1)", table, n)
		}
	}

	var eventCount int
	var actorType string
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), MAX(actor_type) FROM world.events
		WHERE world_id = $1 AND event_type = 'CONTRACT_COMMITTED'`, worldID).Scan(&eventCount, &actorType); err != nil {
		t.Fatalf("count contract events: %v", err)
	}
	if eventCount != 1 || actorType != "policy_bot" {
		t.Fatalf("CONTRACT_COMMITTED count=%d actor=%s, want 1 policy_bot", eventCount, actorType)
	}

	// Registering a contract never touches the ledger (no signing bonus).
	cashAfter, err := svc.GetSummary(ctx, clubID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if cashAfter.Cash != finance.OpeningCapital {
		t.Fatalf("cash = %d, want %d (contract alone moves nothing)", cashAfter.Cash, finance.OpeningCapital)
	}

	// The contract endpoint lists the new contract.
	contracts, err := svc.GetContracts(ctx, clubID)
	if err != nil {
		t.Fatalf("get contracts: %v", err)
	}
	if len(contracts) != 25 {
		t.Fatalf("contracts = %d, want 25", len(contracts))
	}
}

func TestRegisterContractGuards(t *testing.T) {
	pool, svc, _, clubID, botID := bootstrappedClub(t)
	ctx := context.Background()

	var playerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 ORDER BY id LIMIT 1`, clubID).Scan(&playerID); err != nil {
		t.Fatalf("pick player: %v", err)
	}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2029, 6, 30, 0, 0, 0, 0, time.UTC)

	// A manager who doesn't own the club is rejected.
	if _, err := svc.RegisterContract(ctx, finance.Actor{ManagerID: uuid.New()}, clubID,
		finance.ContractInput{PlayerID: playerID, WeeklyWage: 5000, StartDate: start, EndDate: end}); err != finance.ErrNotOwned {
		t.Fatalf("foreign manager = %v, want ErrNotOwned", err)
	}

	// A player not registered to the club is rejected.
	if _, err := svc.RegisterContract(ctx, finance.Actor{ManagerID: botID, IsPolicyBot: true}, clubID,
		finance.ContractInput{PlayerID: uuid.New(), WeeklyWage: 5000, StartDate: start, EndDate: end}); err != finance.ErrPlayerMismatch {
		t.Fatalf("foreign player = %v, want ErrPlayerMismatch", err)
	}

	// Invalid terms.
	if _, err := svc.RegisterContract(ctx, finance.Actor{ManagerID: botID, IsPolicyBot: true}, clubID,
		finance.ContractInput{PlayerID: playerID, WeeklyWage: 0, StartDate: start, EndDate: end}); err != finance.ErrInvalidContract {
		t.Fatalf("zero wage = %v, want ErrInvalidContract", err)
	}
	if _, err := svc.RegisterContract(ctx, finance.Actor{ManagerID: botID, IsPolicyBot: true}, clubID,
		finance.ContractInput{PlayerID: playerID, WeeklyWage: 5000, StartDate: end, EndDate: start}); err != finance.ErrInvalidContract {
		t.Fatalf("inverted dates = %v, want ErrInvalidContract", err)
	}
}

func TestGetLedgerOrdersEntriesNewestFirst(t *testing.T) {
	pool, svc, worldID, clubID, _ := bootstrappedClub(t)
	ctx := context.Background()

	if _, err := svc.ApplyMonthlyWages(ctx, worldID, 1); err != nil {
		t.Fatalf("wage run: %v", err)
	}

	entries, err := svc.GetLedger(ctx, clubID, 100)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if len(entries) != 25 {
		t.Fatalf("ledger entries = %d, want 25 (genesis + 24 wages)", len(entries))
	}
	prev := entries[0].OccurredAt
	for i, e := range entries[1:] {
		if e.OccurredAt.After(prev) {
			t.Fatalf("ledger not newest-first at index %d", i+1)
		}
		prev = e.OccurredAt
	}

	var wages int
	for _, e := range entries {
		if e.EntryType == "debit" && e.Category == "wages" {
			wages++
		}
	}
	if wages != 24 {
		t.Fatalf("wage debits = %d, want 24", wages)
	}

	// A club without a finance account (raw, non-bootstrapped) yields an
	// empty ledger rather than an error.
	otherWorld := testdb.CreateWorld(t, pool, "fin-ledger-empty")
	otherClub := testdb.CreateClub(t, pool, otherWorld)
	empty, err := svc.GetLedger(ctx, otherClub, 10)
	if err != nil {
		t.Fatalf("empty ledger: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty ledger entries = %d, want 0", len(empty))
	}

	// And a summary for an account-less club is zeroed and valid.
	plain, err := svc.GetSummary(ctx, otherClub)
	if err != nil {
		t.Fatalf("plain summary: %v", err)
	}
	if plain.Cash != 0 || plain.CommittedSpending != 0 || plain.Currency != "USD" {
		t.Fatalf("plain summary not zeroed: %+v", plain)
	}
}
