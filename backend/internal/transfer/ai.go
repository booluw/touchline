package transfer

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/pkg/eventbus"
)

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
