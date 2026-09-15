package transfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the persistence layer for the transfer module: read helpers query
// the pool directly; mutations live in Service (tx-managed, like the manager
// and finance modules).
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// listingColumns is the projection shared by every listing read.
const listingSelect = `
	SELECT l.id, l.world_id, l.player_id, p.display_name,
	       pl.primary_position,
	       (CURRENT_DATE - pp.date_of_birth) / 365,
	       pl.market_value,
	       l.listing_club_id, c.name AS club_name,
	       l.asking_price, l.listing_type, l.status,
	       l.listed_at
	FROM transfer.listings l
	JOIN player.players pl ON pl.id = l.player_id
	JOIN person.people pp ON pp.id = pl.person_id
	JOIN club.clubs c ON c.id = l.listing_club_id`

func scanListing(row pgx.Row) (Listing, error) {
	var l Listing
	err := row.Scan(
		&l.ID, &l.WorldID, &l.PlayerID, &l.PlayerName,
		&l.Position, &l.Age, &l.MarketValue,
		&l.ListingClubID, &l.ListingClubName,
		&l.AskingPrice, &l.ListingType, &l.Status, &l.ListedAt,
	)
	return l, err
}

// ListListings returns the market's active listings in a world with
// optional filters. Filter values are empty when not applied.
func (s *Store) ListListings(ctx context.Context, worldID uuid.UUID, status, position, listingType string, sellingClub uuid.UUID) ([]Listing, error) {
	q := listingSelect + `
		WHERE l.world_id = $1`
	args := []any{worldID}
	argi := 2
	clause := ""
	if status != "" {
		clause += fmt.Sprintf(" AND l.status = $%d", argi)
		args = append(args, status)
		argi++
	}
	if position != "" {
		clause += fmt.Sprintf(" AND pl.primary_position = $%d", argi)
		args = append(args, position)
		argi++
	}
	if listingType != "" {
		clause += fmt.Sprintf(" AND l.listing_type = $%d", argi)
		args = append(args, listingType)
		argi++
	}
	if sellingClub != uuid.Nil {
		clause += fmt.Sprintf(" AND l.listing_club_id = $%d", argi)
		args = append(args, sellingClub)
		argi++
	}
	q += clause + `
		ORDER BY l.listed_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listings query: %w", err)
	}
	defer rows.Close()

	var out []Listing
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return nil, fmt.Errorf("listing scan: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listings rows: %w", err)
	}
	if out == nil {
		out = []Listing{}
	}
	return out, nil
}

// GetListing returns one listing row regardless of status.
func (s *Store) GetListing(ctx context.Context, listingID uuid.UUID) (Listing, error) {
	return scanListing(s.pool.QueryRow(ctx,
		listingSelect+` WHERE l.id = $1`, listingID))
}

// ListingByPlayerID returns the active listing for a player, if any.
func (s *Store) ListingByPlayerID(ctx context.Context, playerID uuid.UUID) (Listing, error) {
	return scanListing(s.pool.QueryRow(ctx,
		listingSelect+` WHERE l.player_id = $1 AND l.status = 'active' ORDER BY l.listed_at DESC LIMIT 1`,
		playerID))
}

// bidSelect is the projection of one bid thread with its latest round terms.
const bidSelect = `
	SELECT b.id, b.world_id, b.listing_id, b.player_id, p.display_name,
	       b.bidding_club_id, bc.name AS bidder_name,
	       b.selling_club_id, sc.name AS seller_name,
	       (n.terms->>'fee')::bigint,
	       (n.terms->>'weekly_wage')::bigint,
	       (n.terms->>'contract_length_months')::int,
	       COALESCE((n.terms->>'signing_bonus')::bigint, 0),
	       (n.terms->>'release_clause')::bigint,
	       n.round, n.proposed_by, b.status,
	       b.created_at, b.responded_at
	FROM transfer.bids b
	JOIN transfer.negotiations n ON n.id = (
		SELECT n2.id FROM transfer.negotiations n2
		WHERE n2.bid_id = b.id ORDER BY n2.round DESC LIMIT 1)
	JOIN player.players p ON p.id = b.player_id
	JOIN club.clubs bc ON bc.id = b.bidding_club_id
	JOIN club.clubs sc ON sc.id = b.selling_club_id`

func scanBid(row pgx.Row) (Bid, error) {
	var b Bid
	err := row.Scan(
		&b.ID, &b.WorldID, &b.ListingID, &b.PlayerID, &b.PlayerName,
		&b.BiddingClubID, &b.BiddingClubName,
		&b.SellingClubID, &b.SellingClubName,
		&b.Fee, &b.WeeklyWage, &b.ContractLengthMonths,
		&b.SigningBonus, &b.ReleaseClause,
		&b.Round, &b.ProposedBy, &b.Status,
		&b.CreatedAt, &b.RespondedAt,
	)
	return b, err
}

// GetBid returns one bid thread with its current offer.
func (s *Store) GetBid(ctx context.Context, bidID uuid.UUID) (Bid, error) {
	return scanBid(s.pool.QueryRow(ctx, bidSelect+` WHERE b.id = $1`, bidID))
}

// OpenBidsByListing returns the open (awaiting a response) bids on a listing.
func (s *Store) OpenBidsByListing(ctx context.Context, listingID uuid.UUID) ([]Bid, error) {
	return s.bidsWhere(ctx, `WHERE b.listing_id = $1 AND b.status IN ('pending','countered') ORDER BY b.created_at`, listingID)
}

// OpenBidsByPlayer returns the open bids on a player (any listing).
func (s *Store) OpenBidsByPlayer(ctx context.Context, playerID uuid.UUID) ([]Bid, error) {
	return s.bidsWhere(ctx, `WHERE b.player_id = $1 AND b.status IN ('pending','countered') ORDER BY b.created_at`, playerID)
}

func (s *Store) bidsWhere(ctx context.Context, where string, arg any) ([]Bid, error) {
	rows, err := s.pool.Query(ctx, bidSelect+" "+where, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bid
	for rows.Next() {
		b, err := scanBid(rows)
		if err != nil {
			return nil, fmt.Errorf("bid scan: %w", err)
		}
		out = append(out, b)
	}
	if out == nil {
		out = []Bid{}
	}
	return out, rows.Err()
}

// BidsByClub returns the negotiation threads a club participates in (as buyer
// or seller), newest first, in one world.
func (s *Store) BidsByClub(ctx context.Context, worldID, clubID uuid.UUID) (incoming, outgoing []Bid, err error) {
	rows, err := s.pool.Query(ctx, bidSelect+`
		WHERE b.world_id = $1 AND (b.selling_club_id = $2 OR b.bidding_club_id = $2)
		ORDER BY b.created_at DESC`, worldID, clubID)
	if err != nil {
		return nil, nil, fmt.Errorf("bids query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		b, err := scanBid(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("bid scan: %w", err)
		}
		if b.SellingClubID == clubID {
			incoming = append(incoming, b) // bids OFFERED TO the club (it sells)
		} else {
			outgoing = append(outgoing, b) // bids the club made (it buys)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, rows.Err()
	}
	if incoming == nil {
		incoming = []Bid{}
	}
	if outgoing == nil {
		outgoing = []Bid{}
	}
	return incoming, outgoing, nil
}

// AttrsForPlayer loads the valuation inputs for a single player.
func (s *Store) AttrsForPlayer(ctx context.Context, playerID uuid.UUID) (PlayerAttrs, error) {
	var a PlayerAttrs
	err := s.pool.QueryRow(ctx, `
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
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'positional'), 50)
		FROM player.players pl
		JOIN person.people pp ON pp.id = pl.person_id
		WHERE pl.id = $1`, playerID,
	).Scan(
		&a.PlayerID, &a.Position, &a.MarketValue, &a.Age, &a.ContractEndDays,
		&a.Attributes.Technical, &a.Attributes.Physical, &a.Attributes.Mental,
		&a.Attributes.Tactical, &a.Attributes.Goalkeeping, &a.Attributes.Positional,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrPlayerNotFound
	}
	if err != nil {
		return a, fmt.Errorf("player attrs: %w", err)
	}
	return a, nil
}

// ActiveAttrPlayers lists the players of a world whose valuation must be
// recomputed (status active, signed to a club).
func (s *Store) ActiveAttrPlayers(ctx context.Context, worldID uuid.UUID) ([]PlayerAttrs, error) {
	rows, err := s.pool.Query(ctx, `
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
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'positional'), 50)
		FROM player.players pl
		JOIN person.people pp ON pp.id = pl.person_id
		WHERE pl.world_id = $1 AND pl.status = 'active' AND pl.club_id IS NOT NULL
		ORDER BY pl.id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("active players: %w", err)
	}
	defer rows.Close()

	var out []PlayerAttrs
	for rows.Next() {
		var a PlayerAttrs
		if err := rows.Scan(
			&a.PlayerID, &a.Position, &a.MarketValue, &a.Age, &a.ContractEndDays,
			&a.Attributes.Technical, &a.Attributes.Physical, &a.Attributes.Mental,
			&a.Attributes.Tactical, &a.Attributes.Goalkeeping, &a.Attributes.Positional,
		); err != nil {
			return nil, fmt.Errorf("active player scan: %w", err)
		}
		out = append(out, a)
	}
	if out == nil {
		out = []PlayerAttrs{}
	}
	return out, rows.Err()
}

// positionFamily buckets primary positions into the five squad-depth groups
// used by the AI buyer policy.
func positionFamily(position string) string {
	switch position {
	case "GK":
		return "GK"
	case "CB", "LB", "RB":
		return "DEF"
	case "DM", "CM", "AM":
		return "MID"
	case "LM", "RM", "LW", "RW":
		return "WIDE"
	case "ST":
		return "ATT"
	default:
		return "MID"
	}
}

// AIClubCandidate is one eligible AI club for buyer interest scoring.
type AIClubCandidate struct {
	ClubID     uuid.UUID
	ManagerID  uuid.UUID
	ClubName   string
	SquadDepth int
	Cash       int64
}

// OpenListing summarizes a listing needing AI buyer interest.
type OpenListing struct {
	ID            uuid.UUID
	SellingClubID uuid.UUID
	PlayerID      uuid.UUID
	AskingPrice   *int64
}

// AIInterestTargets lists active open_to_offers listings whose seller is a
// human-run club and that have no open bid yet — the ones the AI market can
// still move on. Returns the market information needed to construct a bid.
func (s *Store) AIInterestTargets(ctx context.Context, worldID uuid.UUID) ([]OpenListing, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.listing_club_id, l.player_id, l.asking_price
		FROM transfer.listings l
		JOIN club.clubs c ON c.id = l.listing_club_id
		WHERE l.world_id = $1
		  AND l.status = 'active'
		  AND l.listing_type = 'open_to_offers'
		  AND c.is_ai_controlled = FALSE
		  AND NOT EXISTS (
		        SELECT 1 FROM transfer.bids b2
		        WHERE b2.listing_id = l.id AND b2.status IN ('pending','countered'))
		ORDER BY l.listed_at ASC`, worldID)
	if err != nil {
		return nil, fmt.Errorf("ai interest targets: %w", err)
	}
	defer rows.Close()

	var out []OpenListing
	for rows.Next() {
		var o OpenListing
		if err := rows.Scan(&o.ID, &o.SellingClubID, &o.PlayerID, &o.AskingPrice); err != nil {
			return nil, fmt.Errorf("ai interest target scan: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ManagerClub returns the club a manager currently manages (status active),
// or ErrNoActiveClub.
func (s *Store) ManagerClub(ctx context.Context, managerID uuid.UUID) (uuid.UUID, error) {
	var clubID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT current_club_id FROM manager.managers
		 WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`, managerID).Scan(&clubID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoActiveClub
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("manager club: %w", err)
	}
	return clubID, nil
}

// ClubFitness loads a club's world membership and verifies the world is
// playable, returning ErrClubNotFound / ErrWorldNotPlayable. It mirrors
// finance.RequireOwnership's requireClub gate.
type clubFitness struct {
	ClubID  uuid.UUID
	WorldID uuid.UUID
}

func (s *Store) ClubFitness(ctx context.Context, clubID uuid.UUID) (clubFitness, error) {
	var cf clubFitness
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT c.world_id, w.status
		FROM club.clubs c
		JOIN world.worlds w ON w.id = c.world_id
		WHERE c.id = $1`, clubID).Scan(&cf.WorldID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return cf, ErrClubNotFound
	}
	if err != nil {
		return cf, fmt.Errorf("club fitness: %w", err)
	}
	if status != "active" && status != "open_beta" {
		return cf, ErrWorldNotPlayable
	}
	return cf, nil
}
