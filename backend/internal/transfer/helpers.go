package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// ---------- internal write helpers ----------

func (s *Service) writeBidThread(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, listingID *uuid.UUID, playerID, buyerClub, sellerClub uuid.UUID, terms Terms, initialBy string) (uuid.UUID, error) {
	var bidID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO transfer.bids (world_id, listing_id, player_id, bidding_club_id, selling_club_id, fee)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		worldID, listingID, playerID, buyerClub, sellerClub, terms.Fee,
	).Scan(&bidID); err != nil {
		return uuid.Nil, fmt.Errorf("insert bid: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transfer.negotiations (bid_id, round, proposed_by, terms)
		VALUES ($1, 1, $2, $3)`,
		bidID, initialBy, mustJSON(terms)); err != nil {
		return uuid.Nil, fmt.Errorf("insert negotiation round: %w", err)
	}
	return bidID, nil
}

func (s *Service) lockPlayer(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) (playerLock, error) {
	var p playerLock
	err := tx.QueryRow(ctx,
		`SELECT id, world_id, club_id, status FROM player.players WHERE id = $1 FOR UPDATE`,
		playerID).Scan(&p.ID, &p.WorldID, &p.ClubID, &p.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrPlayerNotFound
	}
	if err != nil {
		return p, fmt.Errorf("lock player: %w", err)
	}
	return p, nil
}

type playerLock struct {
	ID      uuid.UUID
	WorldID uuid.UUID
	ClubID  uuid.UUID
	Status  string
}

func (s *Service) actorClub(ctx context.Context, managerID uuid.UUID) (uuid.UUID, error) {
	return s.store.ManagerClub(ctx, managerID)
}

func (s *Service) worldTick(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (int64, error) {
	var tick int64
	if err := tx.QueryRow(ctx,
		`SELECT current_tick FROM world.worlds WHERE id = $1`, worldID).Scan(&tick); err != nil {
		return 0, fmt.Errorf("world tick: %w", err)
	}
	return tick, nil
}

func (s *Service) requireFunds(ctx context.Context, tx pgx.Tx, buyerClub uuid.UUID, fee int64) error {
	if fee <= 0 {
		return ErrInvalidTerms
	}
	acct, err := ensureAccount(ctx, tx, uuid.Nil, buyerClub)
	if err != nil {
		return err
	}
	cash, err := cashInTx(ctx, tx, acct)
	if err != nil {
		return err
	}
	if cash < fee {
		return ErrInsufficientFunds
	}
	return nil
}

func ensureAccount(ctx context.Context, tx pgx.Tx, worldID, clubID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT id FROM finance.accounts WHERE club_id = $1`, clubID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("account lookup: %w", err)
	}
	if worldID == uuid.Nil {
		return uuid.Nil, ErrInsufficientFunds // no account and no ability to mint one
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO finance.accounts (world_id, club_id)
		VALUES ($1, $2) RETURNING id`, worldID, clubID).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("create account: %w", err)
	}
	return id, nil
}

func cashInTx(ctx context.Context, tx pgx.Tx, accountID uuid.UUID) (int64, error) {
	var cash int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(
			(SELECT SUM(CASE WHEN entry_type = 'credit' THEN amount ELSE -amount END)::bigint
			 FROM finance.ledger_entries WHERE account_id = $1), 0)`, accountID).Scan(&cash)
	if err != nil {
		return 0, fmt.Errorf("cash in tx: %w", err)
	}
	return cash, nil
}

func postLedger(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, entryType, category string, amount int64,
	description string, relatedEventID *uuid.UUID, occurred time.Time, dedupKey string) (bool, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO finance.ledger_entries
			(account_id, entry_type, category, amount, description, related_event_id, occurred_at, dedup_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (account_id, dedup_key) DO NOTHING`,
		accountID, entryType, category, amount, description, relatedEventID, occurred, dedupKey)
	if err != nil {
		return false, fmt.Errorf("post ledger (deduped): %w", err)
	}
	return true, nil
}

// termsToClauses converts agreed terms into transfer.clauses rows (only the
// whitelisted clause shapes are produced).
func termsToClauses(terms Terms, sellerClub uuid.UUID) []Clause {
	var out []Clause
	if terms.SellOnPercentage != nil {
		out = append(out, Clause{ClauseType: "sell_on", Percentage: terms.SellOnPercentage, BeneficiaryClubID: &sellerClub})
	}
	if terms.BuyBackAmount != nil {
		out = append(out, Clause{ClauseType: "buy_back", Amount: terms.BuyBackAmount, BeneficiaryClubID: &sellerClub})
	}
	return out
}

func validateTerms(t Terms) error {
	if t.Fee <= 0 {
		return ErrInvalidTerms
	}
	if t.WeeklyWage <= 0 {
		return ErrInvalidTerms
	}
	if t.ContractLengthMonths < 1 || t.ContractLengthMonths > 120 {
		return ErrInvalidTerms
	}
	if t.SigningBonus < 0 {
		return ErrInvalidTerms
	}
	if t.SellOnPercentage != nil && (*t.SellOnPercentage < 0 || *t.SellOnPercentage > 100) {
		return ErrInvalidTerms
	}
	if t.BuyBackAmount != nil && *t.BuyBackAmount <= 0 {
		return ErrInvalidTerms
	}
	return nil
}

// ---------- helpers ----------

func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType string, actor Actor, payload []byte) error {
	actorType, actorID := actor.actorTypeAndID()
	e := eventbus.Event{
		WorldID:   worldID,
		EventType: eventType,
		ActorType: &actorType,
		ActorID:   &actorID,
		Payload:   payload,
	}
	return s.recordEventInner(ctx, tx, worldID, &e)
}

func (s *Service) recordSystemEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, eventType string, payload []byte) error {
	actorSystem := "system"
	e := eventbus.Event{
		WorldID:   worldID,
		WorldTick: worldTick,
		EventType: eventType,
		ActorType: &actorSystem,
		Payload:   payload,
	}
	return s.recordEventInner(ctx, tx, worldID, &e)
}

func (s *Service) recordEventWithExplanation(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64,
	eventType string, actor Actor, payload, explanationJSON []byte) (uuid.UUID, error) {
	actorType, actorID := actor.actorTypeAndID()
	e := eventbus.Event{
		ID:          uuid.New(),
		WorldID:     worldID,
		WorldTick:   worldTick,
		EventType:   eventType,
		ActorType:   &actorType,
		ActorID:     &actorID,
		Payload:     payload,
		Explanation: explanationJSON,
	}
	if err := s.recordEventInner(ctx, tx, worldID, &e); err != nil {
		return uuid.Nil, err
	}
	return e.ID, nil
}

func (s *Service) recordEventInner(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, e *eventbus.Event) error {
	if e.WorldTick == 0 {
		var tick int64
		if err := tx.QueryRow(ctx,
			`SELECT current_tick FROM world.worlds WHERE id = $1`, worldID).Scan(&tick); err != nil {
			return fmt.Errorf("world tick: %w", err)
		}
		e.WorldTick = tick
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, e); err != nil {
		return fmt.Errorf("record %s: %w", e.EventType, err)
	}
	return nil
}

// bidClubs loads the two sides of a bid thread. Every bid event carries both
// club ids so the dashboard hook can notify the counterparty (IM26).
func bidClubs(ctx context.Context, tx pgx.Tx, bidID uuid.UUID) (buyer, seller uuid.UUID, err error) {
	if err := tx.QueryRow(ctx,
		`SELECT bidding_club_id, selling_club_id FROM transfer.bids WHERE id = $1`, bidID).Scan(&buyer, &seller); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("bid clubs: %w", err)
	}
	return buyer, seller, nil
}

func bidTarget(ctx context.Context, tx pgx.Tx, in BidInput) uuid.UUID {
	if in.ListingID != nil {
		var pid uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT player_id FROM transfer.listings WHERE id = $1`, *in.ListingID).Scan(&pid); err == nil {
			return pid
		}
	}
	if in.PlayerID != nil {
		return *in.PlayerID
	}
	return uuid.Nil
}

func askingForPlayer(ctx context.Context, tx pgx.Tx, listingID *uuid.UUID) *int64 {
	if listingID == nil {
		return nil
	}
	var asking *int64
	if err := tx.QueryRow(ctx,
		`SELECT asking_price FROM transfer.listings WHERE id = $1`, *listingID).Scan(&asking); err != nil {
		return nil
	}
	return asking
}

func bidAsking(asking *int64) int64 {
	if asking == nil {
		return 0
	}
	return *asking
}

func (s *Service) bidExpired(ctx context.Context, tx pgx.Tx, bidID uuid.UUID) (bool, error) {
	var created time.Time
	var worldID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT created_at, world_id FROM transfer.bids WHERE id = $1`, bidID).Scan(&created, &worldID); err != nil {
		return false, fmt.Errorf("bid created at: %w", err)
	}
	ttl, err := bidTTL(ctx, tx, worldID)
	if err != nil {
		return false, err
	}
	return time.Since(created) > ttl, nil
}

// bidTTL is BidTTLWorldDays converted to real time at the world's clock scale
// (tick.day_length, IM16): three *world* days, which is three real days only at
// the default one-game-day-per-real-day scale (IM26).
func bidTTL(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, worldID uuid.UUID) (time.Duration, error) {
	_, _, dayLength, err := world.LoadScale(ctx, q, worldID)
	if err != nil {
		return 0, fmt.Errorf("bid ttl: world clock: %w", err)
	}
	return time.Duration(BidTTLWorldDays) * dayLength, nil
}

func roundCount(ctx context.Context, tx pgx.Tx, bidID uuid.UUID) int {
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM transfer.negotiations WHERE bid_id = $1`, bidID).Scan(&n); err != nil {
		return 0
	}
	return n
}

func seasonFor(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) int {
	var s int
	if err := tx.QueryRow(ctx,
		`SELECT current_season FROM world.worlds WHERE id = $1`, worldID).Scan(&s); err != nil {
		return 1
	}
	return s
}

func clubNameTx(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) string {
	var name string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(name, short_name) FROM club.clubs WHERE id = $1`, clubID).Scan(&name); err != nil {
		return "Unknown"
	}
	return name
}

func playerNameTx(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) string {
	var name string
	if err := tx.QueryRow(ctx,
		`SELECT pe.display_name
		 FROM player.players p JOIN person.people pe ON pe.id = p.person_id
		 WHERE p.id = $1`, playerID).Scan(&name); err != nil {
		return "Unknown"
	}
	return name
}

// completionExplanation scores an agreed transfer at its fee, broken down as
// the player's market value plus the premium (or discount) the fee paid over
// it — factor deltas sum to the score, as the explanation contract requires
// (pkg/explanation Validate; IM26).
func completionExplanation(valuation int64, terms Terms) *explanation.Explanation {
	exp := explanation.New("transfer_value", int(terms.Fee))
	exp.Add("market_value", int(valuation))
	exp.Add("fee_vs_market_value", int(terms.Fee-valuation))
	return exp
}

// counterExplanation scores a counter-offer at its asked fee: the valuation
// plus the premium demanded over it.
func counterExplanation(subject string, valuation, target int64) (*explanation.Explanation, error) {
	exp := explanation.New(subject, int(target))
	exp.Add("valuation", int(valuation))
	exp.Add("counter_premium", int(target-valuation))
	return exp, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("transfer: marshal event payload: %v", err))
	}
	return b
}
