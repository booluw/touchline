package transfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/explanation"
)

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
	clubID, err := s.actorClub(ctx, actor.ManagerID)
	if err != nil {
		return Bid{}, nil, nil, err
	}
	return s.respondToBid(ctx, actor, worldID, clubID, bidID, action, terms)
}

// RespondToBidForClub runs a negotiation response on behalf of a delegated
// actor (the absence policy bot). Same validation and transactional rules as
// RespondToBid without the actorClub ownership lookup — the caller (policy
// engine) passes the club it is authorised to negotiate for.
func (s *Service) RespondToBidForClub(ctx context.Context, actor Actor, worldID, clubID, bidID uuid.UUID, action string, terms *Terms) (Bid, *CompletedTransfer, *explanation.Explanation, error) {
	return s.respondToBid(ctx, actor, worldID, clubID, bidID, action, terms)
}

// respondToBid is the shared negotiation core, parameterised by the acting
// club. Only the actor identity (a human manager or the policy bot) differs
// between the two public entry points.
func (s *Service) respondToBid(ctx context.Context, actor Actor, worldID, clubID, bidID uuid.UUID, action string, terms *Terms) (Bid, *CompletedTransfer, *explanation.Explanation, error) {
	if !ValidRespondActions[action] {
		return Bid{}, nil, nil, ErrInvalidActionForRole
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
