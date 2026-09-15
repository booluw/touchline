//go:build integration

package transfer_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/internal/transfertest"
	"github.com/touchline/backend/pkg/explanation"
)

// newTransferFixture boots a playable transfer-market world: two AI clubs (the
// bootstrapped starter plus one drafted after) and one human-owned club, all
// fully financed. Returns the service, the pool, and the fixture ids.
func newTransferFixture(t *testing.T, name string) (*transfer.Service, *pgxpool.Pool, transfertest.World) {
	t.Helper()
	pool := testdb.New(t)
	tw := transfertest.Provision(t, pool, name, name+"-owner@example.com")
	return transfer.NewService(pool, nil), pool, tw
}

// pickPlayer returns one active player of a club.
func pickPlayer(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		clubID).Scan(&id); err != nil {
		t.Fatalf("pick player of %s: %v", clubID, err)
	}
	return id
}

// valuation loads the attribute rows the engine reads and re-runs the exported
// formula, so tests can compute the exact AI thresholds.
func valuation(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID) int64 {
	t.Helper()
	var a transfer.PlayerAttrs
	if err := pool.QueryRow(context.Background(), `
		SELECT pl.id, pl.primary_position,
		       (CURRENT_DATE - pp.date_of_birth) / 365,
		       COALESCE((SELECT (ct.end_date - CURRENT_DATE) FROM player.contracts ct
		                  WHERE ct.player_id = pl.id AND ct.status = 'active'
		                  ORDER BY ct.start_date DESC LIMIT 1), 0),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'technical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'physical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'mental'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'tactical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'goalkeeping'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'positional'), 50),
		       COALESCE((SELECT w.weekly_wage::bigint FROM finance.wage_commitments w
		                  JOIN player.contracts ct ON ct.id = w.contract_id AND ct.status = 'active'
		                  WHERE ct.player_id = pl.id ORDER BY ct.start_date DESC LIMIT 1), 0)
		FROM player.players pl
		JOIN person.people pp ON pp.id = pl.person_id
		WHERE pl.id = $1`, playerID).Scan(
		&a.PlayerID, &a.Position, &a.Age, &a.ContractEndDays,
		&a.Attributes.Technical, &a.Attributes.Physical, &a.Attributes.Mental,
		&a.Attributes.Tactical, &a.Attributes.Goalkeeping, &a.Attributes.Positional,
		&a.Wage,
	); err != nil {
		t.Fatalf("read attrs of %s: %v", playerID, err)
	}
	return transfer.Valuation(a)
}

func fullTerms(fee int64) transfer.Terms {
	sellOn := 15
	buyBack := int64(500_000_000)
	return transfer.Terms{
		Fee: fee, WeeklyWage: 250_000, ContractLengthMonths: 24,
		SigningBonus: 1_000_000, SellOnPercentage: &sellOn, BuyBackAmount: &buyBack,
	}
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestAISellerAcceptsFullPriceBidAtomically(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-accept")
	ctx := context.Background()
	player := pickPlayer(t, pool, tw.AIOneClub)
	target := int64(float64(valuation(t, pool, player)) * 1.10)

	bid, ct, exp, err := svc.PlaceBid(ctx, transfer.Actor{ManagerID: tw.HumanMgr},
		tw.WorldID, transfer.BidInput{PlayerID: &player, Terms: fullTerms(target)})
	if err != nil {
		t.Fatalf("place bid: %v", err)
	}
	if bid.Status != transfer.BidStatusAccepted {
		t.Fatalf("bid status = %s, want accepted", bid.Status)
	}
	if ct == nil {
		t.Fatal("full-price bid must complete the transfer")
	}
	if ct.FromClubID != tw.AIOneClub || ct.ToClubID != tw.HumanClub || ct.Fee != target {
		t.Fatalf("completed transfer wrong: %+v", ct)
	}
	if bid.Fee != target {
		t.Fatalf("bid fee = %d, want target %d", bid.Fee, target)
	}

	// Ownership flip.
	if got := count(t, pool, `SELECT COUNT(*) FROM player.players WHERE id = $1 AND club_id = $2`, player, tw.HumanClub); got != 1 {
		t.Fatalf("player ownership: %d, want moved to human club", got)
	}
	if got := count(t, pool, `SELECT COUNT(*) FROM transfer.bids WHERE id = $1 AND status = 'accepted'`, bid.ID); got != 1 {
		t.Fatalf("bid not accepted: %d", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.completed_transfers ct
		WHERE ct.bid_id = $1 AND ct.player_id = $2 AND ct.from_club_id = $3
		  AND ct.to_club_id = $4 AND ct.fee = $5 AND ct.related_event_id IS NOT NULL`,
		bid.ID, player, tw.AIOneClub, tw.HumanClub, target); got != 1 {
		t.Fatalf("completed_transfers row: %d", got)
	}
	var ctID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM transfer.completed_transfers WHERE bid_id = $1`, bid.ID).Scan(&ctID); err != nil {
		t.Fatalf("completed transfer id: %v", err)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.clauses
		WHERE completed_transfer_id = $1 AND clause_type IN ('sell_on','buy_back')`, ctID); got != 2 {
		t.Fatalf("clause rows = %d, want sell_on + buy_back", got)
	}

	// Ledger: buyer debited, seller credited, dedup-keyed.
	if got := count(t, pool, `
		SELECT COUNT(*) FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1 AND l.entry_type = 'debit' AND l.category = 'transfer_fee' AND l.amount = $2`,
		tw.HumanClub, target); got != 1 {
		t.Fatalf("buyer transfer_fee ledger rows = %d, want 1", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1 AND l.entry_type = 'credit' AND l.category = 'player_sale' AND l.amount = $2`,
		tw.AIOneClub, target); got != 1 {
		t.Fatalf("seller player_sale ledger rows = %d, want 1", got)
	}

	// Contract swap: seller's released, buyer's active with the agreed wage.
	if got := count(t, pool, `
		SELECT COUNT(*) FROM player.contracts c
		JOIN finance.wage_commitments w ON w.contract_id = c.id
		WHERE c.player_id = $1 AND c.club_id = $2 AND c.status = 'terminated'
		  AND w.end_date <= CURRENT_DATE`, player, tw.AIOneClub); got < 1 {
		t.Fatalf("seller contract not terminated: %d", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM player.contracts c
		JOIN finance.wage_commitments w ON w.contract_id = c.id
		WHERE c.player_id = $1 AND c.club_id = $2 AND c.status = 'active' AND c.weekly_wage = $3`,
		player, tw.HumanClub, int64(250_000)); got != 1 {
		t.Fatalf("buyer contract rows = %d, want 1", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM player.player_history h
		WHERE h.player_id = $1 AND h.event_type = 'transfer' AND h.club_id = $2
		  AND h.related_event_id IS NOT NULL`, player, tw.HumanClub); got != 1 {
		t.Fatalf("history rows = %d, want 1", got)
	}

	// The border event carries a valid, self-consistent explanation.
	var eventID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT related_event_id FROM transfer.completed_transfers WHERE id = $1`, ctID).Scan(&eventID); err != nil {
		t.Fatalf("related event: %v", err)
	}
	var expJSON []byte
	if err := pool.QueryRow(ctx,
		`SELECT explanation FROM world.events WHERE id = $1 AND event_type = 'BID_ACCEPTED'`, eventID).Scan(&expJSON); err != nil {
		t.Fatalf("bid accepted event: %v", err)
	}
	if exp == nil {
		t.Fatal("accepted bid must return an explanation")
	}
	var decoded explanation.Explanation
	if err := json.Unmarshal(expJSON, &decoded); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("explanation validation: %v", err)
	}
	if decoded.Subject != "transfer_value" {
		t.Fatalf("explanation subject = %q, want transfer_value", decoded.Subject)
	}
}

func TestAISellerCountersThenHumanAccepts(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-counter")
	ctx := context.Background()
	player := pickPlayer(t, pool, tw.AIOneClub)
	val := valuation(t, pool, player)
	target := int64(float64(val) * 1.10)

	bid, ct, exp, err := svc.PlaceBid(ctx, transfer.Actor{ManagerID: tw.HumanMgr},
		tw.WorldID, transfer.BidInput{PlayerID: &player, Terms: fullTerms(int64(float64(val) * 0.95))})
	if err != nil {
		t.Fatalf("place counterable bid: %v", err)
	}
	if ct != nil {
		t.Fatal("under-target bid must not complete")
	}
	if bid.Status != transfer.BidStatusCountered || bid.ProposedBy != transfer.ProposedBySellingClub {
		t.Fatalf("bid = %s/%s, want countered by selling club", bid.Status, bid.ProposedBy)
	}
	if bid.Fee != target {
		t.Fatalf("countered fee = %d, want AI target %d", bid.Fee, target)
	}
	if exp == nil || exp.Subject != "ai_counter" {
		t.Fatalf("counter explanation missing: %+v", exp)
	}

	// A rejected offer below the floor.
	low := int64(float64(val) * 0.80)
	rejected, ct2, _, err := svc.PlaceBid(ctx, transfer.Actor{ManagerID: tw.HumanMgr},
		tw.WorldID, transfer.BidInput{PlayerID: &player, Terms: fullTerms(low)})
	if err != nil {
		t.Fatalf("place below-floor bid: %v", err)
	}
	if ct2 != nil || rejected.Status != transfer.BidStatusRejected {
		t.Fatalf("below-floor bid must reject outright: %+v", rejected)
	}

	// The human accepts the AI's counter and the transfer completes.
	done, completed, _, err := svc.RespondToBid(ctx, transfer.Actor{ManagerID: tw.HumanMgr},
		tw.WorldID, bid.ID, transfer.RespondAccept, nil)
	if err != nil {
		t.Fatalf("accept counter: %v", err)
	}
	if done.Status != transfer.BidStatusAccepted || completed == nil {
		t.Fatalf("accepted counter not completed: %+v", done)
	}
	if got := count(t, pool, `SELECT COUNT(*) FROM player.players WHERE id = $1 AND club_id = $2`, player, tw.HumanClub); got != 1 {
		t.Fatalf("player not moved to human club: %d", got)
	}
}

func TestHumanListingsDrawAIBidsAndResolve(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-sell")
	ctx := context.Background()
	humanActor := transfer.Actor{ManagerID: tw.HumanMgr}

	player := pickPlayer(t, pool, tw.HumanClub)
	listed, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: player, ListingType: transfer.ListingOpenToOffers})
	if err != nil {
		t.Fatalf("create listing: %v", err)
	}
	if listed.Status != "active" {
		t.Fatalf("listing status = %s, want active", listed.Status)
	}
	if listed.BidCount != 2 {
		t.Fatalf("AI interest bids = %d, want 2 (two AI clubs)", listed.BidCount)
	}
	if listed.LatestBid == nil || !listed.LatestBid.NegotiationOpen() {
		t.Fatalf("no open latest bid: %+v", listed)
	}

	// The human accepts one AI offer: the competing thread expires, the
	// listing closes, the fee lands on the seller's ledger, the buyer takes
	// the player.
	winning := listed.LatestBid
	done, completed, _, err := svc.RespondToBid(ctx, humanActor, tw.WorldID,
		winning.ID, transfer.RespondAccept, nil)
	if err != nil {
		t.Fatalf("accept ai bid: %v", err)
	}
	if done.Status != transfer.BidStatusAccepted || completed.ToClubID != winning.BiddingClubID {
		t.Fatalf("bought-by-ai transfer wrong: %+v / %+v", done, completed)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.bids
		WHERE player_id = $1 AND status = 'expired'`, player); got != 1 {
		t.Fatalf("competing threads = %d, want 1 expired", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.listings WHERE id = $1 AND status = 'completed'`, listed.ID); got != 1 {
		t.Fatalf("listing not completed: %d", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1 AND l.entry_type = 'credit' AND l.category = 'player_sale' AND l.amount = $2`,
		tw.HumanClub, winning.Fee); got != 1 {
		t.Fatalf("seller credit missing: %d", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM player.players WHERE id = $1 AND club_id = $2`,
		player, winning.BiddingClubID); got != 1 {
		t.Fatalf("player not moved to winning AI club: %d", got)
	}

	// Reject path: a fresh listing whose AI offer the human turns down stays
	// on the market.
	player2 := pickPlayer(t, pool, tw.HumanClub)
	listed2, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: player2, ListingType: transfer.ListingOpenToOffers})
	if err != nil {
		t.Fatalf("create listing 2: %v", err)
	}
	if listed2.LatestBid == nil {
		t.Fatalf("listing 2 drew no AI bid: %+v", listed2)
	}
	rejected, _, _, err := svc.RespondToBid(ctx, humanActor, tw.WorldID,
		listed2.LatestBid.ID, transfer.RespondReject, nil)
	if err != nil {
		t.Fatalf("reject ai bid: %v", err)
	}
	if rejected.Status != transfer.BidStatusRejected {
		t.Fatalf("rejected bid status = %s", rejected.Status)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.listings WHERE id = $1 AND status = 'active'`, listed2.ID); got != 1 {
		t.Fatalf("listing 2 closed after reject: %d", got)
	}
}

func TestDailyTickExpiresRefreshesAndRebids(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-tick")
	ctx := context.Background()
	humanActor := transfer.Actor{ManagerID: tw.HumanMgr}

	player := pickPlayer(t, pool, tw.HumanClub)
	if _, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: player, ListingType: transfer.ListingOpenToOffers}); err != nil {
		t.Fatalf("create listing: %v", err)
	}
	// Age the open AI bids past the agreed TTL.
	if _, err := pool.Exec(ctx, `
		UPDATE transfer.bids SET created_at = now() - interval '4 days'
		WHERE status IN ('pending','countered')`); err != nil {
		t.Fatalf("backdate bids: %v", err)
	}

	if err := svc.DailyTick(ctx, tw.WorldID, 17); err != nil {
		t.Fatalf("daily tick: %v", err)
	}

	// Stale bids expired AND the market re-bid the now-open listing.
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.bids b
		WHERE b.listing_id IS NOT NULL AND b.status = 'expired'`); got != 2 {
		t.Fatalf("expired stale bids = %d, want 2", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.bids b
		JOIN transfer.listings l ON l.id = b.listing_id
		WHERE b.status IN ('pending','countered') AND l.status = 'active'`); got != 2 {
		t.Fatalf("open bids after rebid = %d, want 2", got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND world_tick = 17 AND event_type = 'BID_EXPIRED'`,
		tw.WorldID); got != 1 {
		t.Fatalf("BID_EXPIRED events = %d, want 1", got)
	}

	// Valuations refreshed for the whole world.
	val := valuation(t, pool, player)
	if got := count(t, pool, `SELECT COUNT(*) FROM player.players WHERE id = $1 AND market_value = $2`,
		player, val); got != 1 {
		t.Fatalf("player market_value not refreshed (val %d): %d", val, got)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND world_tick = 17 AND event_type = 'MARKET_VALUATIONS_REFRESHED'`,
		tw.WorldID); got != 1 {
		t.Fatalf("valuation refresh events = %d, want 1", got)
	}

	// Replaying the same tick is a no-op: nothing left to expire or refresh,
	// and the fresh bids occupy the listing.
	beforeExpired := count(t, pool, `SELECT COUNT(*) FROM transfer.bids WHERE status = 'expired'`)
	beforeOpen := count(t, pool, `SELECT COUNT(*) FROM transfer.bids WHERE status IN ('pending','countered')`)
	if err := svc.DailyTick(ctx, tw.WorldID, 17); err != nil {
		t.Fatalf("replay daily tick: %v", err)
	}
	if got := count(t, pool, `SELECT COUNT(*) FROM transfer.bids WHERE status = 'expired'`); got != beforeExpired {
		t.Fatalf("replay expired more bids: %d -> %d", beforeExpired, got)
	}
	if got := count(t, pool, `SELECT COUNT(*) FROM transfer.bids WHERE status IN ('pending','countered')`); got != beforeOpen {
		t.Fatalf("replay stacked more bids: %d -> %d", beforeOpen, got)
	}
}

func TestTransferGuards(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-guards")
	ctx := context.Background()
	humanActor := transfer.Actor{ManagerID: tw.HumanMgr}

	player := pickPlayer(t, pool, tw.HumanClub)
	listed, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: player, ListingType: transfer.ListingOpenToOffers})
	if err != nil {
		t.Fatalf("first listing: %v", err)
	}
	// Duplicate active listing for the same player is rejected.
	if _, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: player, ListingType: transfer.ListingOpenToOffers}); err != transfer.ErrDuplicateListing {
		t.Fatalf("duplicate listing err = %v, want ErrDuplicateListing", err)
	}
	// Self-bid on the club's own (listed) player.
	target := int64(float64(valuation(t, pool, player)) * 1.10)
	if _, _, _, err := svc.PlaceBid(ctx, humanActor, tw.WorldID,
		transfer.BidInput{PlayerID: &player, Terms: fullTerms(target)}); err != transfer.ErrSelfBid {
		t.Fatalf("self bid err = %v, want ErrSelfBid", err)
	}
	// World mismatch: a club from another world can't transact here.
	otherWorld := testdb.CreateWorld(t, pool, "tf-other")
	if _, _, _, err := svc.PlaceBid(ctx, humanActor, otherWorld,
		transfer.BidInput{PlayerID: &player, Terms: fullTerms(target)}); err != transfer.ErrWorldMismatch {
		t.Fatalf("cross-world bid err = %v, want ErrWorldMismatch", err)
	}
	// A buyer with no finance account at all can't hold the fee hostage.
	pennilessClub := testdb.CreateClub(t, pool, tw.WorldID)
	pennilessUser := testdb.CreateUser(t, pool, "tf-penniless@example.com", "s3cret",
		[]testdb.Join{{WorldID: tw.WorldID, Employed: true}})
	var pennilessMgrID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM manager.managers WHERE user_id = $1 AND world_id = $2`,
		pennilessUser, tw.WorldID).Scan(&pennilessMgrID); err != nil {
		t.Fatalf("penniless manager: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE club.clubs SET current_manager_id = $1 WHERE id = $2`,
		pennilessMgrID, pennilessClub); err != nil {
		t.Fatalf("point penniless manager at club: %v", err)
	}
	if _, _, _, err := svc.PlaceBid(ctx, transfer.Actor{ManagerID: pennilessMgrID}, tw.WorldID,
		transfer.BidInput{PlayerID: &player, Terms: fullTerms(target)}); err != transfer.ErrInsufficientFunds {
		t.Fatalf("penniless bid err = %v, want ErrInsufficientFunds", err)
	}

	// Withdrawing the listing expires the AI offers on it.
	withdrawn, err := svc.WithdrawListing(ctx, humanActor, tw.WorldID, listed.ID)
	if err != nil {
		t.Fatalf("withdraw listing: %v", err)
	}
	if withdrawn.Status != "withdrawn" {
		t.Fatalf("withdrawn status = %s", withdrawn.Status)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.bids WHERE listing_id = $1 AND status = 'expired'`, listed.ID); got != 2 {
		t.Fatalf("expired bids after withdrawal = %d, want 2", got)
	}
}

func TestBidTTLGuardRespondExpired(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-ttl")
	ctx := context.Background()
	humanActor := transfer.Actor{ManagerID: tw.HumanMgr}

	player := pickPlayer(t, pool, tw.HumanClub)
	listed, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: player, ListingType: transfer.ListingOpenToOffers})
	if err != nil {
		t.Fatalf("create listing: %v", err)
	}
	if listed.LatestBid == nil {
		t.Fatalf("no AI bid: %+v", listed)
	}
	// Backdate the thread so the respond wall-clock guard fires (created_at is
	// the negotiation anchor, shared with the expiry sweep).
	if _, err := pool.Exec(ctx,
		`UPDATE transfer.bids SET created_at = now() - interval '4 days' WHERE id = $1`,
		listed.LatestBid.ID); err != nil {
		t.Fatalf("backdate bid: %v", err)
	}
	if _, _, _, err := svc.RespondToBid(ctx, humanActor, tw.WorldID,
		listed.LatestBid.ID, transfer.RespondReject, nil); err != transfer.ErrBidExpired {
		t.Fatalf("stale respond err = %v, want ErrBidExpired", err)
	}
	if got := count(t, pool, `
		SELECT COUNT(*) FROM transfer.bids WHERE id = $1 AND status = 'expired'`,
		listed.LatestBid.ID); got != 1 {
		t.Fatalf("stale bid marked expired: %d", got)
	}
}

func TestListingsAndBidsReads(t *testing.T) {
	svc, pool, tw := newTransferFixture(t, "tf-read")
	ctx := context.Background()
	humanActor := transfer.Actor{ManagerID: tw.HumanMgr}

	p1 := pickPlayer(t, pool, tw.HumanClub)
	l1, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: p1, ListingType: transfer.ListingOpenToOffers})
	if err != nil {
		t.Fatalf("create listing: %v", err)
	}
	p2 := pickPlayer(t, pool, tw.HumanClub)
	if _, err := svc.CreateListing(ctx, humanActor, tw.WorldID,
		transfer.CreateListingInput{PlayerID: p2, ListingType: transfer.ListingLoanAvailable}); err != nil {
		t.Fatalf("create loan-available listing: %v", err)
	}

	listings, err := svc.ListListings(ctx, tw.WorldID, "", "", "", uuid.Nil)
	if err != nil {
		t.Fatalf("list listings: %v", err)
	}
	if len(listings) != 2 {
		t.Fatalf("listings = %d, want 2", len(listings))
	}
	byID := map[uuid.UUID]transfer.Listing{}
	for _, l := range listings {
		byID[l.ID] = l
	}
	if got, ok := byID[l1.ID]; !ok || got.BidCount != 2 {
		t.Fatalf("l1 missing or bid count wrong: %+v", listings)
	}

	got, err := svc.GetListing(ctx, tw.WorldID, l1.ID)
	if err != nil {
		t.Fatalf("get listing: %v", err)
	}
	if got.PlayerID != p1 || got.Status != "active" {
		t.Fatalf("get listing wrong: %+v", got)
	}
	if _, err := svc.GetListing(ctx, tw.WorldID, uuid.New()); err != transfer.ErrListingNotFound {
		t.Fatalf("get unknown listing err = %v, want ErrListingNotFound", err)
	}

	incoming, outgoing, err := svc.ListBids(ctx, tw.WorldID, tw.HumanMgr)
	if err != nil {
		t.Fatalf("list bids: %v", err)
	}
	if len(incoming) != 4 { // two AI offers on each of the two open listings
		t.Fatalf("incoming bids = %d, want 4", len(incoming))
	}
	if len(outgoing) != 0 {
		t.Fatalf("outgoing bids = %d, want 0", len(outgoing))
	}
	for _, b := range incoming {
		if b.SellingClubID != tw.HumanClub {
			t.Fatalf("incoming bid selling club = %s, want human club", b.SellingClubID)
		}
	}
}
