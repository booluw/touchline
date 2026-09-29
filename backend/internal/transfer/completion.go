package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/apiref"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
)

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

	// 1. Ownership flip. Before the player leaves, the dressing-room hook
	// settles relationships and unrest against the pre-sale squad.
	if s.squadDynamics != nil {
		if err := s.squadDynamics.OnPlayerSold(ctx, tx, worldID, worldTick, sellerClub, playerID); err != nil {
			return nil, nil, fmt.Errorf("squad dynamics hook: %w", err)
		}
	}
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

	clauses := termsToClauses(terms, sellerClub)
	for i := range clauses {
		if clauses[i].BeneficiaryClubID != nil {
			clauses[i].BeneficiaryClub = &apiref.ClubRef{ID: *clauses[i].BeneficiaryClubID, Name: clubNameTx(ctx, tx, *clauses[i].BeneficiaryClubID)}
		}
	}
	return &CompletedTransfer{
		ID:             ctID,
		WorldID:        worldID,
		BidID:          bidID,
		PlayerID:       playerID,
		Player:         &apiref.PlayerRef{ID: playerID, Name: playerNameTx(ctx, tx, playerID)},
		FromClubID:     sellerClub,
		FromClub:       &apiref.ClubRef{ID: sellerClub, Name: clubNameTx(ctx, tx, sellerClub)},
		ToClubID:       buyerClub,
		ToClub:         &apiref.ClubRef{ID: buyerClub, Name: clubNameTx(ctx, tx, buyerClub)},
		Fee:            terms.Fee,
		Clauses:        clauses,
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
