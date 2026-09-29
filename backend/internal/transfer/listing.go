package transfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
	defer tx.Rollback(ctx) //nolint:errcheck

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
	defer tx.Rollback(ctx) //nolint:errcheck

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
