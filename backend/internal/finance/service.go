package finance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// Service is the finance engine facade: commands that mutate the ledger and
// contract tables and read models that explain a club's financial position.
type Service struct {
	pool  *pgxpool.Pool
	bus   eventbus.Publisher
	store *Store
}

// NewService wires a Service on top of the given pool. bus may be nil in
// tests; when wired, events are published through the transactional outbox.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher) *Service {
	return &Service{pool: pool, bus: bus, store: NewStore(pool)}
}

// clubRef identifies the world a club lives in.
type clubRef struct {
	WorldID uuid.UUID
}

// RequireOwnership verifies that managerID currently manages clubID in a
// playable world and returns the club's world id. It is the gate for both
// finance reads and the contract command.
func (s *Service) RequireOwnership(ctx context.Context, managerID, clubID uuid.UUID) (uuid.UUID, error) {
	ref, err := s.requireClub(ctx, clubID)
	if err != nil {
		return uuid.Nil, err
	}
	var current uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`, managerID,
	).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) || current != clubID {
		return uuid.Nil, ErrNotOwned
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("finance: ownership: %w", err)
	}
	return ref.WorldID, nil
}

// requireClub verifies the club exists in a playable world.
func (s *Service) requireClub(ctx context.Context, clubID uuid.UUID) (clubRef, error) {
	var ref clubRef
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT c.world_id, w.status
		FROM club.clubs c
		JOIN world.worlds w ON w.id = c.world_id
		WHERE c.id = $1`, clubID,
	).Scan(&ref.WorldID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ref, ErrClubNotFound
	}
	if err != nil {
		return ref, fmt.Errorf("finance: load club: %w", err)
	}
	if !world.Playable(status) {
		return ref, ErrWorldNotActive
	}
	return ref, nil
}

// ---- reads ----

// GetSummary builds the club's full financial picture. A club with no
// finance account yet (a manually seeded test club) returns a zeroed,
// valid summary — it simply never hit the ledger.
func (s *Service) GetSummary(ctx context.Context, clubID uuid.UUID) (*FinanceSummary, error) {
	if _, err := s.requireClub(ctx, clubID); err != nil {
		return nil, err
	}
	sum := &FinanceSummary{Currency: "USD"}

	acct, err := s.store.AccountID(ctx, clubID)
	if err != nil {
		return nil, err
	}
	if acct == uuid.Nil {
		return sum, nil
	}

	factors, err := s.store.LedgerCategories(ctx, clubID)
	if err != nil {
		return nil, err
	}
	sum.Factors = factors

	cash, err := s.store.Cash(ctx, clubID)
	if err != nil {
		return nil, err
	}
	sum.Cash = cash

	season, budgets, err := s.store.Budgets(ctx, clubID)
	if err != nil {
		return nil, err
	}
	if tb, ok := budgets["transfer"]; ok {
		sum.TransferBudget = tb
	}
	if wb, ok := budgets["wage"]; ok {
		sum.WageBudget = wb
	}

	profit, err := s.store.OperatingProfit(ctx, clubID, season)
	if err != nil {
		return nil, err
	}
	sum.OperatingProfit = profit

	commit, err := s.store.WageCommitments(ctx, clubID)
	if err != nil {
		return nil, err
	}
	if commit != nil {
		sum.WageCommitments = *commit
	}

	future, err := s.store.FutureInstallments(ctx, clubID)
	if err != nil {
		return nil, err
	}
	sum.FutureInstallments = future

	// No phase-1 revenue sources: projected revenue stays 0 and the
	// summary factors explain the absence (finance-numerics.md).
	sum.ProjectedRevenue = 0
	sum.Debt = 0

	budgetCommitted := sum.TransferBudget.Committed + sum.WageBudget.Committed
	sum.CommittedSpending = sum.WageCommitments.AnnualWage + budgetCommitted + future
	sum.ProjectedYearEndBalance = sum.Cash - sum.CommittedSpending + sum.ProjectedRevenue

	return sum, nil
}

// GetLedger returns the most recent ledger entries for a club.
func (s *Service) GetLedger(ctx context.Context, clubID uuid.UUID, limit int) ([]LedgerEntry, error) {
	if _, err := s.requireClub(ctx, clubID); err != nil {
		return nil, err
	}
	return s.store.Ledger(ctx, clubID, limit)
}

// GetContracts returns every contract currently on the club's books,
// newest expiry first.
func (s *Service) GetContracts(ctx context.Context, clubID uuid.UUID) ([]ContractView, error) {
	if _, err := s.requireClub(ctx, clubID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.player_id, p.display_name,
		       c.weekly_wage::bigint, c.signing_bonus::bigint,
		       c.start_date::text, c.end_date::text, c.release_clause::bigint, c.status
		FROM player.contracts c
		JOIN player.players pl ON pl.id = c.player_id
		JOIN person.people p ON p.id = pl.person_id
		WHERE c.club_id = $1
		ORDER BY c.end_date DESC, c.id DESC`, clubID)
	if err != nil {
		return nil, fmt.Errorf("contracts query: %w", err)
	}
	defer rows.Close()

	var contracts []ContractView
	for rows.Next() {
		var c ContractView
		if err := rows.Scan(&c.ID, &c.PlayerID, &c.PlayerName,
			&c.WeeklyWage, &c.SigningBonus, &c.StartDate, &c.EndDate,
			&c.ReleaseClause, &c.Status); err != nil {
			return nil, fmt.Errorf("contracts scan: %w", err)
		}
		c.Player = &apiref.PlayerRef{ID: c.PlayerID, Name: c.PlayerName}
		contracts = append(contracts, c)
	}
	if contracts == nil {
		contracts = []ContractView{}
	}
	return contracts, rows.Err()
}

// ---- writes ----

// RegisterContract records a new player contract for a club plus its wage
// commitment mirror, and emits the auditable CONTRACT_COMMITTED event in
// the same transaction. Ownership of the club by actor.ManagerID is
// required — future transfer/negotiation code will drive this as S06 lands.
func (s *Service) RegisterContract(ctx context.Context, actor Actor, clubID uuid.UUID, in ContractInput) (*ContractView, error) {
	worldID, err := s.RequireOwnership(ctx, actor.ManagerID, clubID)
	if err != nil {
		return nil, err
	}
	if in.WeeklyWage <= 0 || in.SigningBonus < 0 {
		return nil, ErrInvalidContract
	}
	if !in.EndDate.After(in.StartDate) {
		return nil, ErrInvalidContract
	}

	playerClub, err := s.store.PlayerClubID(ctx, in.PlayerID)
	if err != nil {
		return nil, err
	}
	if playerClub != clubID {
		return nil, ErrPlayerMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin contract tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var contractID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO player.contracts
			(player_id, club_id, weekly_wage, signing_bonus, start_date, end_date,
			 release_clause, playing_time_promise, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active')
		RETURNING id`,
		in.PlayerID, clubID, in.WeeklyWage, in.SigningBonus,
		in.StartDate, in.EndDate, in.ReleaseClause, in.PlayingTimePromise,
	).Scan(&contractID); err != nil {
		return nil, fmt.Errorf("insert contract: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO finance.wage_commitments (contract_id, club_id, weekly_wage, start_date, end_date)
		VALUES ($1, $2, $3, $4, $5)`,
		contractID, clubID, in.WeeklyWage, in.StartDate, in.EndDate); err != nil {
		return nil, fmt.Errorf("insert wage commitment: %w", err)
	}

	payload := mustJSON(map[string]any{
		"contract_id": contractID,
		"club_id":     clubID,
		"player_id":   in.PlayerID,
		"weekly_wage": in.WeeklyWage,
		"start_date":  in.StartDate.Format("2006-01-02"),
		"end_date":    in.EndDate.Format("2006-01-02"),
	})
	if err := s.recordEvent(ctx, tx, worldID, EventContractCommitted, actor, payload); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit contract: %w", err)
	}

	name := s.store.PlayerName(ctx, in.PlayerID)
	return &ContractView{
		ID:            contractID,
		PlayerID:      in.PlayerID,
		PlayerName:    name,
		Player:        &apiref.PlayerRef{ID: in.PlayerID, Name: name},
		WeeklyWage:    in.WeeklyWage,
		SigningBonus:  in.SigningBonus,
		StartDate:     in.StartDate.Format("2006-01-02"),
		EndDate:       in.EndDate.Format("2006-01-02"),
		ReleaseClause: in.ReleaseClause,
		Status:        "active",
	}, nil
}

// ApplyMonthlyWagesResult reports one monthly wage run.
type ApplyMonthlyWagesResult struct {
	Clubs    int   `json:"clubs"`
	Postings int   `json:"postings"`
	Total    int64 `json:"total"`
}

// ApplyMonthlyWages posts every active wage commitment in a world for the
// given world tick (4 × weekly_wage), once per club, and stamps the
// WAGE_POSTED event for clubs that actually received a posting. The lead
// dedup_key 'wage:<tick>' makes the whole run idempotent under river's
// at-least-once redelivery: a redelivered tick posts nothing new and
// emits no duplicate events.
func (s *Service) ApplyMonthlyWages(ctx context.Context, worldID uuid.UUID, tick int64) (*ApplyMonthlyWagesResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("monthly wages: list clubs: %w", err)
	}
	defer rows.Close()

	res := &ApplyMonthlyWagesResult{}
	for rows.Next() {
		var clubID uuid.UUID
		if err := rows.Scan(&clubID); err != nil {
			return nil, fmt.Errorf("monthly wages: scan club: %w", err)
		}
		posted, total, err := s.applyClubWages(ctx, clubID, tick)
		if err != nil {
			return nil, fmt.Errorf("world %s monthly wages club %s: %w", worldID, clubID, err)
		}
		if posted > 0 {
			res.Clubs++
			res.Postings += posted
			res.Total += total
		}
	}
	return res, rows.Err()
}

// applyClubWages posts and commits the monthly wage debits for one club.
func (s *Service) applyClubWages(ctx context.Context, clubID uuid.UUID, tick int64) (int, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin wage tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var worldID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT world_id FROM club.clubs WHERE id = $1`, clubID).Scan(&worldID); err != nil {
		return 0, 0, fmt.Errorf("wage: load club world: %w", err)
	}

	accountID, err := EnsureAccount(ctx, tx, worldID, clubID)
	if err != nil {
		return 0, 0, err
	}
	wages, err := s.store.ActiveWageCommitments(ctx, tx, clubID)
	if err != nil {
		return 0, 0, err
	}
	if len(wages) == 0 {
		return 0, 0, nil // no commitments: nothing to post, nothing to commit
	}

	exp := explanation.New("monthly_wages", 0)
	postings := 0
	var total int64
	for _, w := range wages {
		monthly := WeeksPerMonth * w.WeeklyWage
		inserted, err := Post(ctx, tx, accountID, "debit", "wages", monthly,
			fmt.Sprintf("Monthly wages (4x weekly) for %s", s.store.PlayerName(ctx, w.PlayerID)),
			nil, time.Now().UTC(), fmt.Sprintf("wage:%d:%s", tick, w.ContractID))
		if err != nil {
			return 0, 0, err
		}
		if !inserted {
			continue // already posted by an earlier application of this tick
		}
		postings++
		total += monthly
		exp.Add(s.store.PlayerName(ctx, w.PlayerID), -int(monthly))
	}
	if postings == 0 {
		return 0, 0, nil // redelivered tick: ledger untouched, no duplicate event
	}

	exp.Score = -int(total)
	exJSON, err := json.Marshal(exp)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal wage explanation: %w", err)
	}
	payload := mustJSON(map[string]any{
		"club_id":    clubID,
		"world_tick": tick,
		"entries":    postings,
		"total":      total,
	})
	if err := s.recordSystemEvent(ctx, tx, worldID, tick, EventWagePosted, payload, exJSON); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit wages: %w", err)
	}
	return postings, total, nil
}

// ---- event helpers ----

func (a Actor) actorTypeAndID() (string, uuid.UUID) {
	if a.IsPolicyBot {
		return "policy_bot", a.ManagerID
	}
	return "manager", a.ManagerID
}

// recordEvent appends one world.events row inside the caller's transaction,
// stamped with the actor that caused it.
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType string, actor Actor, payload []byte) error {
	actorType, actorID := actor.actorTypeAndID()
	e := eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		ActorType: &actorType,
		ActorID:   &actorID,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// recordSystemEvent appends an automated world.events row (system actor),
// carrying the world tick and an optional explanation.
func (s *Service) recordSystemEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, eventType string, payload, explanationJSON []byte) error {
	actorSystem := "system"
	e := eventbus.Event{
		WorldID:     worldID,
		WorldTick:   worldTick,
		EventType:   eventType,
		ActorType:   &actorSystem,
		Payload:     payload,
		Explanation: explanationJSON,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// mustJSON marshals a value without a second thought — callers pass only
// structs/slices of primitives that cannot fail to encode.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("finance: marshal event payload: %v", err))
	}
	return b
}
