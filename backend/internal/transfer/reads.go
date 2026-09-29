package transfer

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
