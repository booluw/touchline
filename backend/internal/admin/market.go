package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/touchline/backend/pkg/apiref"
)

// ---------------------------------------------------------------------------
// Market
// ---------------------------------------------------------------------------

// Market returns the country's open listings, both bid directions, and the
// rolling transfer ledger with the fee-vs-value annotation.
func (s *Service) Market(ctx context.Context, worldID, countryID uuid.UUID, windowDays int) (*MarketPanels, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	if windowDays <= 0 {
		windowDays = 90
	}
	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	m := &MarketPanels{Country: country, WindowDays: windowDays,
		OpenListings: []ListingRow{}, BidsReceived: []BidRow{}, BidsMade: []BidRow{},
		TransfersIn: []TransferRow{}, TransfersOut: []TransferRow{}, Signings: []TransferRow{}}
	if len(clubIDs) == 0 {
		return m, nil
	}

	m.OpenListings = s.openListings(ctx, worldID, clubIDs)
	m.BidsReceived = s.bids(ctx, worldID, clubIDs, true)
	m.BidsMade = s.bids(ctx, worldID, clubIDs, false)
	m.TransfersIn = s.completedTransfers(ctx, worldID, clubIDs, "in", windowDays)
	m.TransfersOut = s.completedTransfers(ctx, worldID, clubIDs, "out", windowDays)
	m.Signings = s.completedTransfers(ctx, worldID, clubIDs, "signing", windowDays)
	return m, nil
}

func (s *Service) openListings(ctx context.Context, worldID uuid.UUID, clubIDs []uuid.UUID) []ListingRow {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.player_id, pe.display_name, p.primary_position,
		       l.listing_club_id, c.short_name, l.asking_price::bigint, p.market_value::bigint,
		       l.listing_type, l.listed_at
		FROM transfer.listings l
		JOIN player.players p ON p.id = l.player_id
		JOIN person.people pe ON pe.id = p.person_id
		JOIN club.clubs c ON c.id = l.listing_club_id
		WHERE l.world_id = $1 AND l.status = 'active' AND l.listing_club_id = ANY($2)
		ORDER BY l.listed_at DESC
		LIMIT 200`, worldID, clubIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ListingRow{}
	for rows.Next() {
		var r ListingRow
		var playerID, clubID uuid.UUID
		var playerName, clubName string
		if err := rows.Scan(&r.ListingID, &playerID, &playerName, &r.Position,
			&clubID, &clubName, &r.AskingPrice, &r.MarketValue, &r.ListingType, &r.ListedAt); err != nil {
			return out
		}
		r.Player = &apiref.PlayerRef{ID: playerID, Name: playerName}
		r.ListingClub = &apiref.ClubRef{ID: clubID, Name: clubName}
		out = append(out, r)
	}
	return out
}

func (s *Service) bids(ctx context.Context, worldID uuid.UUID, clubIDs []uuid.UUID, received bool) []BidRow {
	attribution := "b.selling_club_id"
	if !received {
		attribution = "b.bidding_club_id"
	}
	q := fmt.Sprintf(`
		SELECT b.id, b.player_id, pe.display_name,
		       bc.id, bc.short_name, sc.id, sc.short_name,
		       b.fee::bigint, b.status, b.created_at
		FROM transfer.bids b
		JOIN player.players p ON p.id = b.player_id
		JOIN person.people pe ON pe.id = p.person_id
		JOIN club.clubs bc ON bc.id = b.bidding_club_id
		JOIN club.clubs sc ON sc.id = b.selling_club_id
		WHERE b.world_id = $1 AND b.status IN ('pending','countered')
		  AND %s = ANY($2)
		ORDER BY b.created_at DESC
		LIMIT 200`, attribution)
	rows, err := s.pool.Query(ctx, q, worldID, clubIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []BidRow{}
	for rows.Next() {
		var r BidRow
		var playerID, buyID, sellID uuid.UUID
		var playerName, buyName, sellName string
		if err := rows.Scan(&r.BidID, &playerID, &playerName,
			&buyID, &buyName, &sellID, &sellName,
			&r.Fee, &r.Status, &r.CreatedAt); err != nil {
			return out
		}
		r.Player = &apiref.PlayerRef{ID: playerID, Name: playerName}
		r.BiddingClub = &apiref.ClubRef{ID: buyID, Name: buyName}
		r.SellingClub = &apiref.ClubRef{ID: sellID, Name: sellName}
		out = append(out, r)
	}
	return out
}

func (s *Service) completedTransfers(ctx context.Context, worldID uuid.UUID, clubIDs []uuid.UUID, kind string, windowDays int) []TransferRow {
	var direction string
	switch kind {
	case "in":
		// Paid arrivals: to_club in the country, real seller elsewhere.
		direction = `t.to_club_id = ANY($2) AND t.from_club_id IS NOT NULL`
	case "out":
		direction = `t.from_club_id = ANY($2)`
	default: // signing
		direction = `t.to_club_id = ANY($2) AND t.from_club_id IS NULL`
	}
	q := fmt.Sprintf(`
		SELECT t.id, t.player_id, pe.display_name,
		       t.from_club_id, COALESCE(fc.short_name, ''), t.to_club_id, tc.short_name,
		       t.fee::bigint, p.market_value::bigint, t.completed_at
		FROM transfer.completed_transfers t
		JOIN player.players p ON p.id = t.player_id
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN club.clubs fc ON fc.id = t.from_club_id
		JOIN club.clubs tc ON tc.id = t.to_club_id
		WHERE t.world_id = $1 AND %s
		  AND t.completed_at >= now() - make_interval(days => $3)
		ORDER BY t.completed_at DESC
		LIMIT 200`, direction)

	rows, err := s.pool.Query(ctx, q, worldID, clubIDs, windowDays)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []TransferRow{}
	for rows.Next() {
		var r TransferRow
		var playerID, toClubID uuid.UUID
		var playerName, toClubName, fromClubName string
		var fromClubID *uuid.UUID
		if err := rows.Scan(&r.TransferID, &playerID, &playerName,
			&fromClubID, &fromClubName, &toClubID, &toClubName,
			&r.Fee, &r.MarketValue, &r.CompletedAt); err != nil {
			return out
		}
		r.Player = &apiref.PlayerRef{ID: playerID, Name: playerName}
		if fromClubID != nil {
			r.FromClub = &apiref.ClubRef{ID: *fromClubID, Name: fromClubName}
		}
		r.ToClub = &apiref.ClubRef{ID: toClubID, Name: toClubName}
		if r.MarketValue > 0 && r.Fee > r.MarketValue {
			r.Overpay = true
			pct := int((float64(r.Fee) / float64(r.MarketValue) * 100) - 100)
			r.OverpayPct = &pct
		}
		out = append(out, r)
	}
	return out
}
