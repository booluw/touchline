package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

// PlayerLifecycle is the pluggable hook the transfer completion transaction
// calls so downstream player state (fresh-start morale, open request cleanup)
// lands atomically with the club move. Implemented by internal/player; declared
// here (net interface, not an import) so transfer never depends on player.
type PlayerLifecycle interface {
	OnPlayerTransferred(ctx context.Context, tx pgx.Tx, playerID, newClubID uuid.UUID) error
}

// Service is the transfer market engine. All writes that emit events go
// through the transactional outbox (OPD-23) via eventbus.WriteTx inside the
// caller's transaction; bus may be nil in tests and falls back to RecordTx.
type Service struct {
	pool            *pgxpool.Pool
	bus             eventbus.Publisher
	store           *Store
	playerLifecycle PlayerLifecycle
}

// NewService wires the transfer engine onto a pool and the event bus.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher) *Service {
	return &Service{pool: pool, bus: bus, store: NewStore(pool)}
}

// WithPlayerLifecycle plugs the downstream hook invoked on transfer completion.
func (s *Service) WithPlayerLifecycle(h PlayerLifecycle) *Service {
	s.playerLifecycle = h
	return s
}

// ---------- reads ----------

// ListListings returns the market listings (default: active) in a world.
func (s *Service) ListListings(ctx context.Context, worldID uuid.UUID, status, position, listingType string, sellingClub uuid.UUID) ([]Listing, error) {
	if status == "" {
		status = "active"
	}
	listings, err := s.store.ListListings(ctx, worldID, status, position, listingType, sellingClub)
	if err != nil {
		return nil, err
	}
	for i := range listings {
		if err := s.attachOpenBids(ctx, &listings[i]); err != nil {
			return nil, err
		}
	}
	return listings, nil
}

// GetListing returns one listing with its open bids.
func (s *Service) GetListing(ctx context.Context, worldID, listingID uuid.UUID) (Listing, error) {
	l, err := s.store.GetListing(ctx, listingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrListingNotFound
	}
	if err != nil {
		return l, err
	}
	if l.WorldID != worldID {
		return l, ErrListingNotFound
	}
	if err := s.attachOpenBids(ctx, &l); err != nil {
		return l, err
	}
	return l, nil
}

// attachOpenBids fills the listing's bid count and, when one is open, the
// latest bid.
func (s *Service) attachOpenBids(ctx context.Context, l *Listing) error {
	if l.Status != "active" {
		return nil
	}
	bids, err := s.store.OpenBidsByListing(ctx, l.ID)
	if err != nil {
		return err
	}
	l.BidCount = len(bids)
	if len(bids) > 0 {
		latest := bids[len(bids)-1]
		l.LatestBid = &latest
	}
	return nil
}

// ListBids returns the negotiation threads a manager's club is involved in
// (incoming = offers to buy its players, outgoing = its bids on others).
func (s *Service) ListBids(ctx context.Context, worldID, managerID uuid.UUID) (incoming, outgoing []Bid, err error) {
	clubID, err := s.actorClub(ctx, managerID)
	if err != nil {
		return nil, nil, err
	}
	cf, err := s.store.ClubFitness(ctx, clubID)
	if err != nil {
		return nil, nil, err
	}
	if cf.WorldID != worldID {
		return nil, nil, ErrWorldMismatch
	}
	return s.store.BidsByClub(ctx, worldID, clubID)
}

// GetBid returns one negotiation thread (scoped to the caller's world).
func (s *Service) GetBid(ctx context.Context, worldID, bidID uuid.UUID) (Bid, error) {
	b, err := s.store.GetBid(ctx, bidID)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrBidNotFound
	}
	if err != nil {
		return b, err
	}
	if b.WorldID != worldID {
		return b, ErrBidNotFound
	}
	return b, nil
}

// ---------- listings ----------

// CreateListingInput is the payload of POST /api/transfers/listings.
type CreateListingInput struct {
	PlayerID    uuid.UUID `json:"player_id"`
	AskingPrice *int64    `json:"asking_price,omitempty"`
	ListingType string    `json:"listing_type,omitempty"`
}

// CreateListing lists a player of the caller's club for transfer (or as a
// loan-available note; the loan lifecycle itself is a later slice). AI buyer
// interest is placed atomically in the same transaction.
func (s *Service) CreateListing(ctx context.Context, actor Actor, worldID uuid.UUID, in CreateListingInput) (*Listing, error) {
	clubID, err := s.actorClub(ctx, actor.ManagerID)
	if err != nil {
		return nil, err
	}
	cf, err := s.store.ClubFitness(ctx, clubID)
	if err != nil {
		return nil, err
	}
	if cf.WorldID != worldID {
		return nil, ErrWorldMismatch
	}
	if in.ListingType == "" {
		in.ListingType = ListingOpenToOffers
	}
	if !ReasonableListingTypes[in.ListingType] {
		return nil, ErrInvalidTerms
	}
	if in.AskingPrice != nil && *in.AskingPrice < 0 {
		return nil, ErrInvalidTerms
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin listing tx: %w", err)
	}
	defer tx.Rollback(ctx)

	player, err := s.lockPlayer(ctx, tx, in.PlayerID)
	if err != nil {
		return nil, err
	}
	if player.ClubID != clubID {
		return nil, ErrNotClubMember
	}
	if player.Status != "active" {
		return nil, ErrPlayerNotTransferable
	}
	if player.WorldID != worldID {
		return nil, ErrWorldMismatch
	}

	var listingID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO transfer.listings (world_id, player_id, listing_club_id, asking_price, listing_type)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		worldID, player.ID, clubID, in.AskingPrice, in.ListingType,
	).Scan(&listingID)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateListing
	}
	if err != nil {
		return nil, fmt.Errorf("insert listing: %w", err)
	}

	ticks, err := s.worldTick(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	payload := mustJSON(map[string]any{
		"listing_id":   listingID,
		"player_id":    player.ID,
		"club_id":      clubID,
		"listing_type": in.ListingType,
	})
	if err := s.recordEvent(ctx, tx, worldID, EventListed, actor, payload); err != nil {
		return nil, err
	}

	// AI buyer interest on a fresh listing settles immediately (deterministic).
	if in.ListingType == ListingOpenToOffers {
		if err := s.aiBiddersForListing(ctx, tx, worldID, ticks, OpenListing{
			ID:            listingID,
			SellingClubID: clubID,
			PlayerID:      player.ID,
			AskingPrice:   in.AskingPrice,
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit listing: %w", err)
	}

	listed, err := s.store.GetListing(ctx, listingID)
	if err != nil {
		return nil, err
	}
	_ = s.attachOpenBids(ctx, &listed)
	return &listed, nil
}

// WithdrawListing takes the caller's listing off the market and expires any
// open bids anchored to it.
func (s *Service) WithdrawListing(ctx context.Context, actor Actor, worldID, listingID uuid.UUID) (Listing, error) {
	clubID, err := s.actorClub(ctx, actor.ManagerID)
	if err != nil {
		return Listing{}, err
	}
	cf, err := s.store.ClubFitness(ctx, clubID)
	if err != nil {
		return Listing{}, err
	}
	if cf.WorldID != worldID {
		return Listing{}, ErrWorldMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Listing{}, fmt.Errorf("begin withdraw tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		lWorldID uuid.UUID
		curClub  uuid.UUID
		status   string
	)
	err = tx.QueryRow(ctx,
		`SELECT world_id, listing_club_id, status FROM transfer.listings WHERE id = $1 FOR UPDATE`,
		listingID).Scan(&lWorldID, &curClub, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Listing{}, ErrListingNotFound
	}
	if err != nil {
		return Listing{}, fmt.Errorf("load listing: %w", err)
	}
	if lWorldID != worldID || status != "active" {
		return Listing{}, ErrListingNotFound
	}
	if curClub != clubID {
		return Listing{}, ErrNotListingClub
	}

	if _, err := tx.Exec(ctx,
		`UPDATE transfer.listings SET status = 'withdrawn' WHERE id = $1`, listingID); err != nil {
		return Listing{}, fmt.Errorf("withdraw listing: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE transfer.bids SET status = 'expired', responded_at = now()
		 WHERE listing_id = $1 AND status IN ('pending','countered')`, listingID); err != nil {
		return Listing{}, fmt.Errorf("expire listing bids: %w", err)
	}
	var playerID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT player_id FROM transfer.listings WHERE id = $1`, listingID).Scan(&playerID); err != nil {
		return Listing{}, fmt.Errorf("listing player: %w", err)
	}

	if _, err := s.worldTick(ctx, tx, worldID); err != nil {
		return Listing{}, err
	}
	payload := mustJSON(map[string]any{"listing_id": listingID, "player_id": playerID, "club_id": clubID})
	if err := s.recordEvent(ctx, tx, worldID, EventListingWithdrawn, actor, payload); err != nil {
		return Listing{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Listing{}, fmt.Errorf("commit withdraw: %w", err)
	}
	return s.store.GetListing(ctx, listingID)
}

// ---------- bids ----------

// PlaceBid submits an offer for a player (via a listing or directly). When the
// selling club is AI the counterpart policy resolves it atomically in the same
// transaction: the call may come back accepted (transfer completed), rejected,
// or countered.
func (s *Service) PlaceBid(ctx context.Context, actor Actor, worldID uuid.UUID, in BidInput) (Bid, *CompletedTransfer, *explanation.Explanation, error) {
	buyerClub, err := s.actorClub(ctx, actor.ManagerID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	cf, err := s.store.ClubFitness(ctx, buyerClub)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	if cf.WorldID != worldID {
		return Bid{}, nil, nil, ErrWorldMismatch
	}
	if err := validateTerms(in.Terms); err != nil {
		return Bid{}, nil, nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bid{}, nil, nil, fmt.Errorf("begin bid tx: %w", err)
	}
	defer tx.Rollback(ctx)

	player, err := s.lockPlayer(ctx, tx, bidTarget(ctx, tx, in))
	if err != nil {
		return Bid{}, nil, nil, err
	}
	if player.WorldID != worldID {
		return Bid{}, nil, nil, ErrWorldMismatch
	}
	if player.ClubID == uuid.Nil || player.Status != "active" {
		return Bid{}, nil, nil, ErrPlayerNotTransferable
	}
	sellingClub := player.ClubID
	if sellingClub == buyerClub {
		return Bid{}, nil, nil, ErrSelfBid
	}

	var listingID *uuid.UUID
	if in.ListingID != nil {
		var (
			lWorld uuid.UUID
			lOwner uuid.UUID
			lStat  string
		)
		if err := tx.QueryRow(ctx,
			`SELECT world_id, player_id, status FROM transfer.listings WHERE id = $1`, *in.ListingID,
		).Scan(&lWorld, &lOwner, &lStat); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Bid{}, nil, nil, ErrListingNotFound
			}
			return Bid{}, nil, nil, fmt.Errorf("load bid listing: %w", err)
		}
		if lWorld != worldID || lOwner != player.ID || lStat != "active" {
			return Bid{}, nil, nil, ErrListingNotFound
		}
		listingID = in.ListingID
	}

	var dup uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM transfer.bids
		WHERE bidding_club_id = $1 AND player_id = $2 AND status IN ('pending','countered')
		LIMIT 1`, buyerClub, player.ID).Scan(&dup)
	if err == nil {
		return Bid{}, nil, nil, ErrDuplicateOpenBid
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Bid{}, nil, nil, fmt.Errorf("duplicate bid check: %w", err)
	}

	if err := s.requireFunds(ctx, tx, buyerClub, in.Terms.Fee); err != nil {
		return Bid{}, nil, nil, err
	}

	ticks, err := s.worldTick(ctx, tx, worldID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	bidID, err := s.writeBidThread(ctx, tx, worldID, listingID, player.ID, buyerClub, sellingClub, in.Terms, ProposedByBuyingClub)
	if err != nil {
		return Bid{}, nil, nil, err
	}

	payload := mustJSON(map[string]any{
		"bid_id": bidID, "player_id": player.ID,
		"buying_club_id": buyerClub, "selling_club_id": sellingClub, "fee": in.Terms.Fee,
	})
	if err := s.recordEvent(ctx, tx, worldID, EventBidPlaced, actor, payload); err != nil {
		return Bid{}, nil, nil, err
	}

	var (
		resolved *CompletedTransfer
		exp      *explanation.Explanation
	)
	aiSeller, err := s.isAIClubTx(ctx, tx, sellingClub)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	if aiSeller {
		attrs, err := s.attrsForPlayerTx(ctx, tx, player.ID)
		if err != nil {
			return Bid{}, nil, nil, err
		}
		val := Valuation(attrs)
		decision, target := aiSellerDecision(in.Terms.Fee, val, askingForPlayer(ctx, tx, listingID))
		bot := sellerBot(ctx, tx, sellingClub)
		switch decision {
		case RespondAccept:
			resolved, exp, err = s.acceptBid(ctx, tx, worldID, ticks, bidID, bot, EventBidAccepted, val)
			if err != nil {
				return Bid{}, nil, nil, err
			}
		case RespondReject:
			if err := s.rejectBid(ctx, tx, worldID, ticks, bidID, bot, EventBidRejected); err != nil {
				return Bid{}, nil, nil, err
			}
		case RespondCounter:
			counter := in.Terms
			counter.Fee = target
			if err := s.counterBid(ctx, tx, worldID, ticks, bidID, bot, ProposedBySellingClub, counter); err != nil {
				return Bid{}, nil, nil, err
			}
			exp, err = counterExplanation("ai_counter", val, target)
			if err != nil {
				return Bid{}, nil, nil, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Bid{}, nil, nil, fmt.Errorf("commit bid: %w", err)
	}

	bid, err := s.store.GetBid(ctx, bidID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	return bid, resolved, exp, nil
}

// RespondToBid is the single negotiation endpoint: accept the current offer,
// reject it, or put a counter on the table. When the counterparty is an AI
// club, a human counter is answered by the policy automatically; an accepted
// bid runs the atomic completion.
func (s *Service) RespondToBid(ctx context.Context, actor Actor, worldID, bidID uuid.UUID, action string, terms *Terms) (Bid, *CompletedTransfer, *explanation.Explanation, error) {
	if !ValidRespondActions[action] {
		return Bid{}, nil, nil, ErrInvalidActionForRole
	}
	clubID, err := s.actorClub(ctx, actor.ManagerID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	cf, err := s.store.ClubFitness(ctx, clubID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	if cf.WorldID != worldID {
		return Bid{}, nil, nil, ErrWorldMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bid{}, nil, nil, fmt.Errorf("begin respond tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		bWorldID, buyerClub, sellerClub uuid.UUID
		status                          string
	)
	err = tx.QueryRow(ctx,
		`SELECT world_id, bidding_club_id, selling_club_id, status
		 FROM transfer.bids WHERE id = $1 FOR UPDATE`,
		bidID).Scan(&bWorldID, &buyerClub, &sellerClub, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bid{}, nil, nil, ErrBidNotFound
	}
	if err != nil {
		return Bid{}, nil, nil, fmt.Errorf("load bid: %w", err)
	}
	if bWorldID != worldID {
		return Bid{}, nil, nil, ErrBidNotFound
	}

	roleSeller := clubID == sellerClub
	roleBuyer := clubID == buyerClub
	if !roleSeller && !roleBuyer {
		return Bid{}, nil, nil, ErrNotBidParticipant
	}
	if status != BidStatusPending && status != BidStatusCountered {
		return Bid{}, nil, nil, ErrBidResolved
	}
	if ok, err := s.bidExpired(ctx, tx, bidID); err != nil {
		return Bid{}, nil, nil, err
	} else if ok {
		if _, err := tx.Exec(ctx,
			`UPDATE transfer.bids SET status = 'expired', responded_at = now() WHERE id = $1`, bidID); err != nil {
			return Bid{}, nil, nil, err
		}
		return Bid{}, nil, nil, ErrBidExpired
	}

	if action == RespondAccept {
		// You agree to the other side's proposal, never your own.
		var curProposer string
		if err := tx.QueryRow(ctx, `
			SELECT n.proposed_by FROM transfer.negotiations n
			WHERE n.bid_id = $1 ORDER BY n.round DESC LIMIT 1`, bidID).Scan(&curProposer); err != nil {
			return Bid{}, nil, nil, fmt.Errorf("current round: %w", err)
		}
		mine := ProposedBySellingClub
		if roleBuyer {
			mine = ProposedByBuyingClub
		}
		if curProposer == mine {
			return Bid{}, nil, nil, ErrInvalidActionForRole
		}
	}

	ticks, err := s.worldTick(ctx, tx, worldID)
	if err != nil {
		return Bid{}, nil, nil, err
	}

	var (
		playerID uuid.UUID
		lID      *uuid.UUID
		curAsk   *int64
	)
	if err := tx.QueryRow(ctx,
		`SELECT player_id, listing_id FROM transfer.bids WHERE id = $1`, bidID).Scan(&playerID, &lID); err != nil {
		return Bid{}, nil, nil, fmt.Errorf("bid player: %w", err)
	}
	if lID != nil {
		if err := tx.QueryRow(ctx,
			`SELECT asking_price FROM transfer.listings WHERE id = $1`, *lID).Scan(&curAsk); err != nil {
			curAsk = nil
		}
	}

	var (
		resolved *CompletedTransfer
		exp      *explanation.Explanation
	)
	switch action {
	case RespondAccept:
		attrs, err := s.attrsForPlayerTx(ctx, tx, playerID)
		if err != nil {
			return Bid{}, nil, nil, err
		}
		val := Valuation(attrs)
		resolved, exp, err = s.acceptBid(ctx, tx, worldID, ticks, bidID, actor, EventBidAccepted, val)
		if err != nil {
			return Bid{}, nil, nil, err
		}
	case RespondReject:
		if err := s.rejectBid(ctx, tx, worldID, ticks, bidID, actor, EventBidRejected); err != nil {
			return Bid{}, nil, nil, err
		}
	case RespondCounter:
		if terms == nil {
			return Bid{}, nil, nil, ErrInvalidTerms
		}
		mySide := ProposedBySellingClub
		if roleBuyer {
			mySide = ProposedByBuyingClub
		}
		if err := s.counterBid(ctx, tx, worldID, ticks, bidID, actor, mySide, *terms); err != nil {
			return Bid{}, nil, nil, err
		}

		// An AI counterparty replies to a human counter immediately: the
		// buyer when the human is the seller, the seller otherwise.
		opponentClub := buyerClub
		if !roleSeller {
			opponentClub = sellerClub
		}
		aiOpponent, err := s.isAIClubTx(ctx, tx, opponentClub)
		if err != nil {
			return Bid{}, nil, nil, err
		}
		if aiOpponent && !actor.IsPolicyBot {
			attrs, err := s.attrsForPlayerTx(ctx, tx, playerID)
			if err != nil {
				return Bid{}, nil, nil, err
			}
			val := Valuation(attrs)
			oppSide := ProposedByBuyingClub
			if roleBuyer {
				oppSide = ProposedBySellingClub
			}
			exp, err = s.aiRepliesToCounter(ctx, tx, worldID, ticks, bidID, roleSeller, val, *terms, oppSide, curAsk)
			if err != nil {
				return Bid{}, nil, nil, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Bid{}, nil, nil, fmt.Errorf("commit response: %w", err)
	}

	bid, err := s.store.GetBid(ctx, bidID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	return bid, resolved, exp, nil
}

// WithdrawBid abandons an offer the buyer made while it is still open.
func (s *Service) WithdrawBid(ctx context.Context, actor Actor, worldID, bidID uuid.UUID) (Bid, error) {
	clubID, err := s.actorClub(ctx, actor.ManagerID)
	if err != nil {
		return Bid{}, err
	}
	cf, err := s.store.ClubFitness(ctx, clubID)
	if err != nil {
		return Bid{}, err
	}
	if cf.WorldID != worldID {
		return Bid{}, ErrWorldMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bid{}, fmt.Errorf("begin withdraw bid tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		bWorldID, buyerClub uuid.UUID
		status              string
	)
	err = tx.QueryRow(ctx,
		`SELECT world_id, bidding_club_id, status FROM transfer.bids WHERE id = $1 FOR UPDATE`,
		bidID).Scan(&bWorldID, &buyerClub, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bid{}, ErrBidNotFound
	}
	if err != nil {
		return Bid{}, fmt.Errorf("load bid: %w", err)
	}
	if bWorldID != worldID {
		return Bid{}, ErrBidNotFound
	}
	if buyerClub != clubID {
		return Bid{}, ErrNotBidParticipant
	}
	if status != BidStatusPending && status != BidStatusCountered {
		return Bid{}, ErrBidResolved
	}

	if _, err := tx.Exec(ctx,
		`UPDATE transfer.bids SET status = 'withdrawn', responded_at = now() WHERE id = $1`, bidID); err != nil {
		return Bid{}, fmt.Errorf("withdraw bid: %w", err)
	}
	if _, err := s.worldTick(ctx, tx, worldID); err != nil {
		return Bid{}, err
	}
	payload := mustJSON(map[string]any{"bid_id": bidID})
	if err := s.recordEvent(ctx, tx, worldID, EventBidWithdrawn, actor, payload); err != nil {
		return Bid{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Bid{}, fmt.Errorf("commit withdraw bid: %w", err)
	}
	return s.store.GetBid(ctx, bidID)
}

// ---------- daily sweep ----------

// DailyTick runs the market's daily housekeeping for a world: expiring stale
// bids, refreshing every valuation, and letting AI clubs bid on unsold human
// listings. Every step is idempotent under redelivery.
func (s *Service) DailyTick(ctx context.Context, worldID uuid.UUID, worldTick int64) error {
	if _, err := s.ExpireStale(ctx, worldID, worldTick); err != nil {
		return err
	}
	if _, err := s.RecomputeValuations(ctx, worldID, worldTick); err != nil {
		return err
	}
	if _, err := s.AIBidActivity(ctx, worldID, worldTick); err != nil {
		return err
	}
	return nil
}

// ExpireStale resolves bids that have gone unanswered beyond BidTTLWorldDays.
func (s *Service) ExpireStale(ctx context.Context, worldID uuid.UUID, worldTick int64) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE transfer.bids SET status = 'expired', responded_at = now()
		WHERE world_id = $1 AND status IN ('pending','countered')
		  AND created_at < now() - ($2 || ' days')::interval`, worldID, BidTTLWorldDays)
	if err != nil {
		return 0, fmt.Errorf("expire stale bids: %w", err)
	}
	n := tag.RowsAffected()
	if n == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire event tx: %w", err)
	}
	defer tx.Rollback(ctx)
	payload := mustJSON(map[string]any{"world_id": worldID, "world_tick": worldTick, "expired": n})
	if err := s.recordSystemEvent(ctx, tx, worldID, worldTick, EventBidExpired, payload); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expire event: %w", err)
	}
	return n, nil
}

// RecomputeValuations refreshes player.players.market_value for every active
// squad member in a world (filling the "never written" gap this slice closes).
func (s *Service) RecomputeValuations(ctx context.Context, worldID uuid.UUID, worldTick int64) (int64, error) {
	players, err := s.store.ActiveAttrPlayers(ctx, worldID)
	if err != nil {
		return 0, err
	}
	if len(players) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin valuation tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var n int64
	for _, p := range players {
		v := Valuation(p)
		if v == p.MarketValue {
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE player.players SET market_value = $1 WHERE id = $2`, v, p.PlayerID); err != nil {
			return 0, fmt.Errorf("set valuation: %w", err)
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	payload := mustJSON(map[string]any{"world_id": worldID, "world_tick": worldTick, "players": n})
	if err := s.recordSystemEvent(ctx, tx, worldID, worldTick, EventValuationsRefreshed, payload); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit valuations: %w", err)
	}
	return n, nil
}

// AIBidActivity has the highest-interest AI clubs bid on unsold human
// listings. Deterministic per (listing, worldTick); listing guards keep the
// market from stacking bids.
func (s *Service) AIBidActivity(ctx context.Context, worldID uuid.UUID, worldTick int64) (int64, error) {
	targets, err := s.store.AIInterestTargets(ctx, worldID)
	if err != nil {
		return 0, err
	}
	if len(targets) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin ai activity tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var placed int64
	for _, t := range targets {
		attrs, err := s.attrsForPlayerTx(ctx, tx, t.PlayerID)
		if err != nil {
			return 0, err
		}
		val := Valuation(attrs)
		candidates, err := s.aiCandidatesTx(ctx, tx, worldID, t.SellingClubID, attrs.Position)
		if err != nil {
			return 0, err
		}
		for _, cand := range candidates {
			fee := aiBidFee(aiBidSeed(t.ID, worldTick), val, bidAsking(t.AskingPrice))
			if cand.Cash < int64(float64(fee)*aiBuyFundsMargin) {
				continue
			}
			if err := s.placeAIBid(ctx, tx, worldID, worldTick, t, cand, fee, attrs); err != nil {
				return 0, err
			}
			placed++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit ai activity: %w", err)
	}
	return placed, nil
}

// ---------- completion (the atomic ownership flip) ----------

// acceptBid settles the transfer: player club move, contract swap, ledger
// posting, history row, clause capture, listing close-outs and competitor bid
// expiry — all in the caller's transaction.
func (s *Service) acceptBid(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, bidID uuid.UUID, actor Actor, eventType string, valuation int64) (*CompletedTransfer, *explanation.Explanation, error) {
	var (
		playerID, buyerClub, sellerClub uuid.UUID
		listingID                       *uuid.UUID
	)
	if err := tx.QueryRow(ctx,
		`SELECT player_id, bidding_club_id, selling_club_id, listing_id
		 FROM transfer.bids WHERE id = $1 FOR UPDATE`, bidID,
	).Scan(&playerID, &buyerClub, &sellerClub, &listingID); err != nil {
		return nil, nil, fmt.Errorf("load accepted bid: %w", err)
	}

	var termsBytes []byte
	if err := tx.QueryRow(ctx, `
		SELECT n.terms FROM transfer.negotiations n
		WHERE n.bid_id = $1 ORDER BY n.round DESC LIMIT 1`, bidID).Scan(&termsBytes); err != nil {
		return nil, nil, fmt.Errorf("accepted terms: %w", err)
	}
	var terms Terms
	if err := json.Unmarshal(termsBytes, &terms); err != nil {
		return nil, nil, fmt.Errorf("decode accepted terms: %w", err)
	}
	if err := validateTerms(terms); err != nil {
		return nil, nil, ErrInvalidTerms
	}

	buyerAccount, err := ensureAccount(ctx, tx, worldID, buyerClub)
	if err != nil {
		return nil, nil, err
	}
	cash, err := cashInTx(ctx, tx, buyerAccount)
	if err != nil {
		return nil, nil, err
	}
	if cash < terms.Fee {
		return nil, nil, ErrInsufficientFunds
	}

	// 1. Ownership flip.
	if _, err := tx.Exec(ctx,
		`UPDATE player.players SET club_id = $2, status = 'active' WHERE id = $1`, playerID, buyerClub); err != nil {
		return nil, nil, fmt.Errorf("move player: %w", err)
	}
	// 1b. Downstream player hook (fresh-start morale, request cleanup) rides
	// the same transaction as the club move.
	if s.playerLifecycle != nil {
		if err := s.playerLifecycle.OnPlayerTransferred(ctx, tx, playerID, buyerClub); err != nil {
			return nil, nil, fmt.Errorf("player lifecycle hook: %w", err)
		}
	}

	// 2. Terminate the seller's active contracts and end their wage
	// commitments today so wage runs stop.
	if _, err := tx.Exec(ctx,
		`UPDATE player.contracts SET status = 'terminated'
		 WHERE player_id = $1 AND status = 'active' AND club_id = $2`, playerID, sellerClub); err != nil {
		return nil, nil, fmt.Errorf("terminate seller contracts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE finance.wage_commitments w
		SET end_date = CURRENT_DATE
		FROM player.contracts c
		WHERE w.contract_id = c.id AND c.player_id = $1 AND c.club_id = $2
		  AND c.status = 'terminated'`, playerID, sellerClub); err != nil {
		return nil, nil, fmt.Errorf("end seller wage commitments: %w", err)
	}

	// 3. The buyer contract and its commitment, from the agreed terms.
	endDate := time.Now().UTC().AddDate(0, terms.ContractLengthMonths, 0)
	var contractID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO player.contracts
			(player_id, club_id, weekly_wage, signing_bonus, start_date, end_date,
			 release_clause, status)
		VALUES ($1, $2, $3, $4, CURRENT_DATE, $5, $6, 'active')
		RETURNING id`,
		playerID, buyerClub, terms.WeeklyWage, terms.SigningBonus, endDate.Format("2006-01-02"), terms.ReleaseClause,
	).Scan(&contractID); err != nil {
		return nil, nil, fmt.Errorf("insert buyer contract: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO finance.wage_commitments (contract_id, club_id, weekly_wage, start_date, end_date)
		VALUES ($1, $2, $3, CURRENT_DATE, $4)`,
		contractID, buyerClub, terms.WeeklyWage, endDate.Format("2006-01-02")); err != nil {
		return nil, nil, fmt.Errorf("insert buyer wage commitment: %w", err)
	}

	// 4. The auditable border event, then the completed-transfer record.
	exp := completionExplanation(valuation, terms)
	exJSON, err := json.Marshal(exp)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal transfer explanation: %w", err)
	}
	payload := mustJSON(map[string]any{
		"bid_id": bidID, "player_id": playerID,
		"from_club_id": sellerClub, "to_club_id": buyerClub, "fee": terms.Fee,
	})
	eventID, err := s.recordEventWithExplanation(ctx, tx, worldID, worldTick, eventType, actor, payload, exJSON)
	if err != nil {
		return nil, nil, err
	}

	var ctID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO transfer.completed_transfers
			(world_id, bid_id, player_id, from_club_id, to_club_id, fee,
			 is_record_transfer_for_buyer, is_record_transfer_for_seller, related_event_id)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE, TRUE, $7)
		RETURNING id`,
		worldID, bidID, playerID, sellerClub, buyerClub, terms.Fee, eventID,
	).Scan(&ctID); err != nil {
		return nil, nil, fmt.Errorf("insert completed transfer: %w", err)
	}

	// 5. Ledger: money out of the buyer, into the seller, idempotently keyed.
	sellerAccount, err := ensureAccount(ctx, tx, worldID, sellerClub)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	if _, err := postLedger(ctx, tx, buyerAccount, "debit", "transfer_fee", terms.Fee,
		"Transfer fee paid", &eventID, now, "transfer:"+ctID.String()+":buyer"); err != nil {
		return nil, nil, err
	}
	if _, err := postLedger(ctx, tx, sellerAccount, "credit", "player_sale", terms.Fee,
		"Player sale: transfer fee received", &eventID, now, "transfer:"+ctID.String()+":seller"); err != nil {
		return nil, nil, err
	}

	// 6. Clauses captured from the agreed terms (whitelist: clauses CHECK).
	for _, cl := range termsToClauses(terms, sellerClub) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO transfer.clauses
				(completed_transfer_id, clause_type, percentage, amount, beneficiary_club_id)
			VALUES ($1, $2, $3, $4, $5)`,
			ctID, cl.ClauseType, cl.Percentage, cl.Amount, cl.BeneficiaryClubID); err != nil {
			return nil, nil, fmt.Errorf("insert clause: %w", err)
		}
	}

	// 7. Player history on the new club.
	if _, err := tx.Exec(ctx, `
		INSERT INTO player.player_history (player_id, world_id, season, club_id, event_type, description, related_event_id)
		VALUES ($1, $2, $3, $4, 'transfer', $5, $6)`,
		playerID, worldID, seasonFor(ctx, tx, worldID), buyerClub,
		fmt.Sprintf("Completed transfer from %s to %s for %d",
			clubNameTx(ctx, tx, sellerClub), clubNameTx(ctx, tx, buyerClub), terms.Fee),
		&eventID); err != nil {
		return nil, nil, fmt.Errorf("buyer history: %w", err)
	}

	// 8. Close the listing and every other thread on this player.
	if _, err := tx.Exec(ctx,
		`UPDATE transfer.listings SET status = 'completed'
		 WHERE player_id = $1 AND status = 'active'`, playerID); err != nil {
		return nil, nil, fmt.Errorf("close listings: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE transfer.bids SET status = 'expired', responded_at = now()
		 WHERE player_id = $1 AND id <> $2 AND status IN ('pending','countered')`,
		playerID, bidID); err != nil {
		return nil, nil, fmt.Errorf("expire competing bids: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE transfer.bids SET status = 'accepted', responded_at = now() WHERE id = $1`, bidID); err != nil {
		return nil, nil, fmt.Errorf("accept bid: %w", err)
	}

	return &CompletedTransfer{
		ID:             ctID,
		WorldID:        worldID,
		BidID:          bidID,
		PlayerID:       playerID,
		FromClubID:     sellerClub,
		ToClubID:       buyerClub,
		Fee:            terms.Fee,
		Clauses:        termsToClauses(terms, sellerClub),
		RelatedEventID: eventID,
		CompletedAt:    time.Now().UTC(),
	}, exp, nil
}

// rejectBid marks a thread rejected and emits the decision event.
func (s *Service) rejectBid(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, ticks int64, bidID uuid.UUID, actor Actor, eventType string) error {
	if _, err := tx.Exec(ctx,
		`UPDATE transfer.bids SET status = 'rejected', responded_at = now() WHERE id = $1`, bidID); err != nil {
		return fmt.Errorf("reject bid: %w", err)
	}
	payload := mustJSON(map[string]any{"bid_id": bidID, "decision": "reject"})
	actorType, actorID := actor.actorTypeAndID()
	e := eventbus.Event{
		WorldID:   worldID,
		WorldTick: ticks,
		EventType: eventType,
		ActorType: &actorType,
		ActorID:   &actorID,
		Payload:   payload,
	}
	return eventbus.WriteTx(ctx, s.bus, tx, &e)
}

// counterBid appends a negotiation round and flips the thread to 'countered'
// (a counter is on the table).
func (s *Service) counterBid(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, ticks int64, bidID uuid.UUID, actor Actor, proposedBy string, terms Terms) error {
	if err := validateTerms(terms); err != nil {
		return err
	}
	actorType, actorID := actor.actorTypeAndID()
	payload := mustJSON(map[string]any{
		"bid_id": bidID, "proposed_by": proposedBy, "fee": terms.Fee,
	})
	e := eventbus.Event{
		WorldID:   worldID,
		WorldTick: ticks,
		EventType: EventBidCountered,
		ActorType: &actorType,
		ActorID:   &actorID,
		Payload:   payload,
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transfer.negotiations (bid_id, round, proposed_by, terms)
		SELECT $1, COALESCE(MAX(round), 0) + 1, $3, $4
		FROM transfer.negotiations WHERE bid_id = $2`,
		bidID, bidID, proposedBy, mustJSON(terms)); err != nil {
		return fmt.Errorf("insert negotiation round: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE transfer.bids SET status = 'countered', responded_at = now() WHERE id = $1`, bidID); err != nil {
		return fmt.Errorf("mark countered: %w", err)
	}
	return eventbus.WriteTx(ctx, s.bus, tx, &e)
}

// aiRepliesToCounter resolves an AI club's reaction to a human counter: it
// may accept (completing the transfer), counter once more, or reject.
func (s *Service) aiRepliesToCounter(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, ticks int64, bidID uuid.UUID,
	humanIsSeller bool, valuation int64, terms Terms, oppSide string, asking *int64) (*explanation.Explanation, error) {
	bot, err := s.oppositeBot(ctx, tx, bidID, humanIsSeller)
	if err != nil {
		return nil, err
	}

	var decision string
	var target int64
	if humanIsSeller {
		decision, target = aiBuyerDecision(terms.Fee, valuation) // AI is the buyer
	} else {
		decision, target = aiSellerDecision(terms.Fee, valuation, asking) // AI is the seller
	}

	switch decision {
	case RespondAccept:
		_, exp, err := s.acceptBid(ctx, tx, worldID, ticks, bidID, bot, EventBidAccepted, valuation)
		return exp, err
	case RespondReject:
		return nil, s.rejectBid(ctx, tx, worldID, ticks, bidID, bot, EventBidRejected)
	default:
		if roundCount(ctx, tx, bidID) >= aiMaxRounds {
			return nil, s.rejectBid(ctx, tx, worldID, ticks, bidID, bot, EventBidRejected)
		}
		counter := terms
		counter.Fee = target
		if err := s.counterBid(ctx, tx, worldID, ticks, bidID, bot, oppSide, counter); err != nil {
			return nil, err
		}
		return counterExplanation("ai_counter", valuation, target)
	}
}

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

// ---------- ai internals ----------

func (s *Service) isAIClubTx(ctx context.Context, tx pgx.Tx, clubID uuid.UUID) (bool, error) {
	var ai bool
	err := tx.QueryRow(ctx, `SELECT is_ai_controlled FROM club.clubs WHERE id = $1`, clubID).Scan(&ai)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrClubNotFound
	}
	return ai, err
}

// sellerBot returns the policy-bot manager running an AI selling club.
func sellerBot(ctx context.Context, tx pgx.Tx, sellingClub uuid.UUID) Actor {
	var botID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE current_club_id = $1 AND is_policy_bot = TRUE LIMIT 1`,
		sellingClub).Scan(&botID)
	if err != nil {
		return Actor{ManagerID: uuid.Nil, IsPolicyBot: true}
	}
	return Actor{ManagerID: botID, IsPolicyBot: true}
}

// oppositeBot returns the AI policy-bot for the club opposite the human
// participant in a bid thread.
func (s *Service) oppositeBot(ctx context.Context, tx pgx.Tx, bidID uuid.UUID, humanIsSeller bool) (Actor, error) {
	var clubID uuid.UUID
	if humanIsSeller {
		if err := tx.QueryRow(ctx,
			`SELECT bidding_club_id FROM transfer.bids WHERE id = $1`, bidID).Scan(&clubID); err != nil {
			return Actor{}, fmt.Errorf("ai bidder club: %w", err)
		}
	} else {
		if err := tx.QueryRow(ctx,
			`SELECT selling_club_id FROM transfer.bids WHERE id = $1`, bidID).Scan(&clubID); err != nil {
			return Actor{}, fmt.Errorf("ai seller club: %w", err)
		}
	}
	var bot uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE current_club_id = $1 AND is_policy_bot = TRUE LIMIT 1`,
		clubID).Scan(&bot)
	if err != nil {
		return Actor{}, fmt.Errorf("ai bot manager: %w", err)
	}
	return Actor{ManagerID: bot, IsPolicyBot: true}, nil
}

// aiBiddersForListing invites AI buyers for a freshly listed player, called
// inside the create-listing transaction.
func (s *Service) aiBiddersForListing(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, l OpenListing) error {
	attrs, err := s.attrsForPlayerTx(ctx, tx, l.PlayerID)
	if err != nil {
		return err
	}
	candidates, err := s.aiCandidatesTx(ctx, tx, worldID, l.SellingClubID, attrs.Position)
	if err != nil {
		return err
	}
	val := Valuation(attrs)
	for _, cand := range candidates {
		fee := aiBidFee(aiBidSeed(l.ID, worldTick), val, bidAsking(l.AskingPrice))
		if cand.Cash < int64(float64(fee)*aiBuyFundsMargin) {
			continue
		}
		if err := s.placeAIBid(ctx, tx, worldID, worldTick, l, cand, fee, attrs); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) placeAIBid(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, worldTick int64, l OpenListing, cand AIClubCandidate, fee int64, attrs PlayerAttrs) error {
	wage := attrs.Wage
	if wage <= 0 {
		wage = defaultAIDealWage
	}
	terms := Terms{
		Fee:                  fee,
		WeeklyWage:           wage,
		ContractLengthMonths: 24,
	}
	bidID, err := s.writeBidThread(ctx, tx, worldID, &l.ID, l.PlayerID, cand.ClubID, l.SellingClubID, terms, ProposedByBuyingClub)
	if err != nil {
		return err
	}
	bot := Actor{ManagerID: cand.ManagerID, IsPolicyBot: true}
	payload := mustJSON(map[string]any{
		"bid_id": bidID, "player_id": l.PlayerID,
		"buying_club_id": cand.ClubID, "selling_club_id": l.SellingClubID, "fee": fee,
	})
	actorType, actorID := bot.actorTypeAndID()
	e := eventbus.Event{
		WorldID:   worldID,
		WorldTick: worldTick,
		EventType: EventBidPlaced,
		ActorType: &actorType,
		ActorID:   &actorID,
		Payload:   payload,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record ai bid: %w", err)
	}
	return nil
}

const defaultAIDealWage = int64(20_000)

// aiCandidatesTx builds the AI buyer shortlist for a player within a tx.
func (s *Service) aiCandidatesTx(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, sellerClubID uuid.UUID, position string) ([]AIClubCandidate, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, m.id, c.name,
		       (SELECT COUNT(*) FROM player.players p
		         WHERE p.club_id = c.id AND p.status = 'active'
		           AND CASE $3 WHEN 'GK'  THEN p.primary_position = 'GK'
		                       WHEN 'DEF' THEN p.primary_position IN ('CB','LB','RB')
		                       WHEN 'MID' THEN p.primary_position IN ('DM','CM','AM')
		                       WHEN 'WIDE' THEN p.primary_position IN ('LM','RM','LW','RW')
		                       WHEN 'ATT' THEN p.primary_position = 'ST' END),
		       COALESCE((SELECT SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint
		                  FROM finance.ledger_entries l
		                  JOIN finance.accounts a ON a.id = l.account_id
		                  WHERE a.club_id = c.id), 0)
		FROM club.clubs c
		JOIN manager.managers m ON m.id = c.current_manager_id
		WHERE c.world_id = $1 AND c.is_ai_controlled = TRUE
		  AND m.is_policy_bot = TRUE AND c.id <> $2
		ORDER BY c.id`, worldID, sellerClubID, positionFamily(position))
	if err != nil {
		return nil, fmt.Errorf("ai candidates: %w", err)
	}
	defer rows.Close()

	var out []AIClubCandidate
	for rows.Next() {
		var c AIClubCandidate
		if err := rows.Scan(&c.ClubID, &c.ManagerID, &c.ClubName, &c.SquadDepth, &c.Cash); err != nil {
			return nil, fmt.Errorf("ai candidate scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ai candidates rows: %w", err)
	}
	// The deterministic shortlist: thinnest positional squads first (they need
	// the signing most), club id as the stable tie-break, capped at the agreed
	// top-K interest size so the market never stacks.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SquadDepth != out[j].SquadDepth {
			return out[i].SquadDepth < out[j].SquadDepth
		}
		return out[i].ClubID.String() < out[j].ClubID.String()
	})
	if len(out) > aiInterestClubs {
		out = out[:aiInterestClubs]
	}
	return out, nil
}

// attrsForPlayerTx loads valuation inputs within a transaction (valuation
// needs the player's current contract, which the completion moves).
func (s *Service) attrsForPlayerTx(ctx context.Context, tx pgx.Tx, playerID uuid.UUID) (PlayerAttrs, error) {
	var a PlayerAttrs
	err := tx.QueryRow(ctx, `
		SELECT pl.id, pl.primary_position, pl.market_value,
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
		WHERE pl.id = $1`, playerID,
	).Scan(
		&a.PlayerID, &a.Position, &a.MarketValue, &a.Age, &a.ContractEndDays,
		&a.Attributes.Technical, &a.Attributes.Physical, &a.Attributes.Mental,
		&a.Attributes.Tactical, &a.Attributes.Goalkeeping, &a.Attributes.Positional,
		&a.Wage,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrPlayerNotFound
	}
	if err != nil {
		return a, fmt.Errorf("player attrs in tx: %w", err)
	}
	return a, nil
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
	if err := tx.QueryRow(ctx,
		`SELECT created_at FROM transfer.bids WHERE id = $1`, bidID).Scan(&created); err != nil {
		return false, fmt.Errorf("bid created at: %w", err)
	}
	return time.Since(created) > time.Duration(BidTTLWorldDays)*24*time.Hour, nil
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

func completionExplanation(valuation int64, terms Terms) *explanation.Explanation {
	exp := explanation.New("transfer_value", int(valuation))
	exp.Add("market_value", int(valuation))
	exp.Add("bid_fee", int(terms.Fee))
	return exp
}

func counterExplanation(subject string, valuation, target int64) (*explanation.Explanation, error) {
	exp := explanation.New(subject, int(valuation))
	exp.Add("valuation", int(valuation))
	exp.Add("counter_fee", int(target))
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
