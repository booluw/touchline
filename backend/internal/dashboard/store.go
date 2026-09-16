package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the read-only aggregation layer. Every query reads tables owned by
// other modules (transfer, finance, board, player, competition, social, match)
// and never mutates game state — GET /api/dashboard and the realtime push both
// ride on it.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// pendingBid is one open bid thread where a managed club is the seller.
type pendingBid struct {
	BidID           uuid.UUID
	PlayerID        uuid.UUID
	PlayerName      string
	BiddingClubID   uuid.UUID
	BiddingClubName string
	SellingClubID   uuid.UUID
	SellingClubName string
	Fee             int64
	Round           int
	Status          string
	CreatedAt       time.Time
}

// ManagerClubs returns the clubs the manager currently manages within a world
// (one active club per manager row, but kept a slice for symmetry with
// policybot).
func (s *Store) ManagerClubs(ctx context.Context, worldID, managerID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND world_id = $2 AND current_club_id IS NOT NULL`, managerID, worldID)
	if err != nil {
		return nil, fmt.Errorf("manager clubs: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var c uuid.UUID
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan manager club: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("manager clubs rows: %w", err)
	}
	return out, nil
}

// ManagerForClub resolves the active manager of a club within a world. Used by
// the realtime bid-event hook to route a pushed urgent update to the selling
// club's manager.
func (s *Store) ManagerForClub(ctx context.Context, worldID, clubID uuid.UUID) (uuid.UUID, error) {
	var managerID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM manager.managers
		WHERE world_id = $1 AND current_club_id = $2
		ORDER BY created_at LIMIT 1`, worldID, clubID).Scan(&managerID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("manager for club: %w", err)
	}
	return managerID, nil
}

// PendingBids lists open bid threads (pending + countered) in which any of the
// given clubs is the seller, newest first.
func (s *Store) PendingBids(ctx context.Context, clubIDs []uuid.UUID) ([]pendingBid, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.player_id, p.display_name,
		       b.bidding_club_id, bc.name, b.selling_club_id, sc.name,
		       (n.terms->>'fee')::bigint, n.round, b.status, b.created_at
		FROM transfer.bids b
		JOIN transfer.negotiations n ON n.id = (
			SELECT n2.id FROM transfer.negotiations n2
			WHERE n2.bid_id = b.id ORDER BY n2.round DESC LIMIT 1)
		JOIN player.players p ON p.id = b.player_id
		JOIN club.clubs bc ON bc.id = b.bidding_club_id
		JOIN club.clubs sc ON sc.id = b.selling_club_id
		WHERE b.selling_club_id = ANY($1) AND b.status IN ('pending', 'countered')
		ORDER BY b.created_at DESC`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("pending bids: %w", err)
	}
	defer rows.Close()
	var out []pendingBid
	for rows.Next() {
		var b pendingBid
		if err := rows.Scan(&b.BidID, &b.PlayerID, &b.PlayerName,
			&b.BiddingClubID, &b.BiddingClubName, &b.SellingClubID, &b.SellingClubName,
			&b.Fee, &b.Round, &b.Status, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pending bid: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pending bids rows: %w", err)
	}
	return out, nil
}

// expiringContract is one active contract that ends inside the expiry window.
type expiringContract struct {
	PlayerID   uuid.UUID
	PlayerName string
	ClubID     uuid.UUID
	EndDate    time.Time
	WeeklyWage int64
	SquadRole  *string
}

// ExpiringContracts lists active contracts ending within the next windowDays
// days for any given club, soonest expiry first.
func (s *Store) ExpiringContracts(ctx context.Context, clubIDs []uuid.UUID, windowDays int) ([]expiringContract, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.player_id, p.display_name, c.club_id, c.end_date,
		       COALESCE(c.weekly_wage, 0)::bigint, c.squad_role
		FROM player.contracts c
		JOIN player.players p ON p.id = c.player_id
		WHERE c.club_id = ANY($1) AND c.status = 'active'
		  AND c.end_date <= CURRENT_DATE + $2::int
		ORDER BY c.end_date`, clubIDs, windowDays)
	if err != nil {
		return nil, fmt.Errorf("expiring contracts: %w", err)
	}
	defer rows.Close()
	var out []expiringContract
	for rows.Next() {
		var c expiringContract
		if err := rows.Scan(&c.PlayerID, &c.PlayerName, &c.ClubID, &c.EndDate,
			&c.WeeklyWage, &c.SquadRole); err != nil {
			return nil, fmt.Errorf("scan expiring contract: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expiring contracts rows: %w", err)
	}
	return out, nil
}

// urgentFixture is one scheduled fixture inside the urgency window.
type urgentFixture struct {
	FixtureID   uuid.UUID
	HomeClubID  uuid.UUID
	AwayClubID  uuid.UUID
	HomeName    string
	AwayName    string
	ScheduledAt time.Time
}

// NextUrgentFixtures lists scheduled fixtures involving any given club inside
// the next window, soonest first.
func (s *Store) NextUrgentFixtures(ctx context.Context, clubIDs []uuid.UUID, window time.Duration) ([]urgentFixture, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.home_club_id, f.away_club_id, h.name, a.name, f.scheduled_at
		FROM match.fixtures f
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		WHERE (f.home_club_id = ANY($1) OR f.away_club_id = ANY($1))
		  AND f.status = 'scheduled' AND f.scheduled_at <= $2
		ORDER BY f.scheduled_at`, clubIDs, time.Now().UTC().Add(window))
	if err != nil {
		return nil, fmt.Errorf("next fixtures: %w", err)
	}
	defer rows.Close()
	var out []urgentFixture
	for rows.Next() {
		var f urgentFixture
		if err := rows.Scan(&f.FixtureID, &f.HomeClubID, &f.AwayClubID,
			&f.HomeName, &f.AwayName, &f.ScheduledAt); err != nil {
			return nil, fmt.Errorf("scan next fixture: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("next fixtures rows: %w", err)
	}
	return out, nil
}

// boardSnapshot is one job-security confidence row for a managed club.
type boardSnapshot struct {
	ClubID     uuid.UUID
	WorldTick  int64
	TotalScore int
	CreatedAt  time.Time
}

// BoardSnapshots returns the two most recent weekly confidence snapshots for
// the manager's club(s), newest first. Weekly review writes one per world
// tick, so tick ordering approximates time ordering.
func (s *Store) BoardSnapshots(ctx context.Context, managerID uuid.UUID) ([]boardSnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT club_id, world_tick, total_score, created_at
		FROM manager.job_security_snapshots
		WHERE manager_id = $1
		ORDER BY club_id, world_tick DESC, created_at DESC LIMIT 4`, managerID)
	if err != nil {
		return nil, fmt.Errorf("board snapshots: %w", err)
	}
	defer rows.Close()
	var out []boardSnapshot
	for rows.Next() {
		var t boardSnapshot
		if err := rows.Scan(&t.ClubID, &t.WorldTick, &t.TotalScore, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan board snapshot: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("board snapshots rows: %w", err)
	}
	return out, nil
}

// financialCrisis is one unresolved crisis row for a managed club.
type financialCrisis struct {
	ClubID    uuid.UUID
	ClubName  string
	Stage     string
	StartedAt time.Time
}

// UnresolvedCrises lists unresolved financial-crisis states for any given club.
func (s *Store) UnresolvedCrises(ctx context.Context, clubIDs []uuid.UUID) ([]financialCrisis, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, cc.name, c.stage, c.started_at
		FROM finance.financial_crisis_states c
		JOIN club.clubs cc ON cc.id = c.club_id
		WHERE c.club_id = ANY($1) AND c.resolved_at IS NULL
		ORDER BY c.started_at`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("unresolved crises: %w", err)
	}
	defer rows.Close()
	var out []financialCrisis
	for rows.Next() {
		var c financialCrisis
		if err := rows.Scan(&c.ClubID, &c.ClubName, &c.Stage, &c.StartedAt); err != nil {
			return nil, fmt.Errorf("scan crisis: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unresolved crises rows: %w", err)
	}
	return out, nil
}

// cashBalance is one managed club's current cash position (from its ledger).
type cashBalance struct {
	ClubID   uuid.UUID
	ClubName string
	Cash     int64
}

// CashBalances returns the current cash balance per given club.
func (s *Store) CashBalances(ctx context.Context, clubIDs []uuid.UUID) ([]cashBalance, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.club_id, cc.name, COALESCE(
			(SELECT SUM(CASE WHEN l.entry_type = 'credit' THEN l.amount ELSE -l.amount END)::bigint
			 FROM finance.ledger_entries l WHERE l.account_id = a.id), 0)
		FROM finance.accounts a
		JOIN club.clubs cc ON cc.id = a.club_id
		WHERE a.club_id = ANY($1)
		ORDER BY cc.name`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("cash balances: %w", err)
	}
	defer rows.Close()
	var out []cashBalance
	for rows.Next() {
		var c cashBalance
		if err := rows.Scan(&c.ClubID, &c.ClubName, &c.Cash); err != nil {
			return nil, fmt.Errorf("scan cash: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cash balances rows: %w", err)
	}
	return out, nil
}

// unhappyPlayer is one managed player with morale at/below the warning line.
type unhappyPlayer struct {
	PlayerID      uuid.UUID
	PlayerName    string
	ClubID        uuid.UUID
	Morale        float64
	HasTransferTr bool
}

// UnhappyPlayers lists managed players with morale at/below threshold, plus a
// flag for an open transfer request.
func (s *Store) UnhappyPlayers(ctx context.Context, clubIDs []uuid.UUID, threshold float64) ([]unhappyPlayer, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pc.player_id, p.display_name, pc.club_id, pc.morale,
		       EXISTS (
				SELECT 1 FROM player.player_transfer_requests r
				WHERE r.player_id = pc.player_id AND r.status = 'pending')
		FROM player.player_condition pc
		JOIN player.players p ON p.id = pc.player_id
		WHERE pc.club_id = ANY($1) AND pc.morale <= $2
		ORDER BY pc.morale, p.display_name`, clubIDs, threshold)
	if err != nil {
		return nil, fmt.Errorf("unhappy players: %w", err)
	}
	defer rows.Close()
	var out []unhappyPlayer
	for rows.Next() {
		var p unhappyPlayer
		var has bool
		if err := rows.Scan(&p.PlayerID, &p.PlayerName, &p.ClubID, &p.Morale, &has); err != nil {
			return nil, fmt.Errorf("scan unhappy player: %w", err)
		}
		p.HasTransferTr = has
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("unhappy players rows: %w", err)
	}
	return out, nil
}

// standingsSpot is a managed club's active domestic-league position.
type standingsSpot struct {
	ClubID      uuid.UUID
	ClubName    string
	Position    int
	Points      int
	Played      int
	SeasonLabel string
}

// StandingsPositions resolves each given club's position in its active
// domestic league season.
func (s *Store) StandingsPositions(ctx context.Context, clubIDs []uuid.UUID) ([]standingsSpot, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH club_active_league AS (
			SELECT DISTINCT ON (ce.club_id) ce.club_id, s.id AS season_id, s.season_label
			FROM competition.competition_entries ce
			JOIN competition.seasons s ON s.id = ce.season_id
			JOIN competition.competitions c ON c.id = s.competition_id
			WHERE ce.club_id = ANY($1) AND s.status <> 'completed'
			  AND c.competition_type = 'league'
			ORDER BY ce.club_id, s.season_number DESC, s.status
		),
		ranked AS (
			SELECT st.season_id, st.club_id,
			       ROW_NUMBER() OVER (
			           PARTITION BY st.season_id
			           ORDER BY st.points DESC, (st.goals_for - st.goals_against) DESC,
			                    st.goals_for DESC, cl.name) AS position,
			       st.points, st.played
			FROM competition.standings st
			JOIN competition.seasons s ON s.id = st.season_id AND s.status <> 'completed'
			JOIN club.clubs cl ON cl.id = st.club_id
		)
		SELECT cal.club_id, cc.name, r.position, r.points, r.played, cal.season_label
		FROM club_active_league cal
		JOIN ranked r ON r.club_id = cal.club_id AND r.season_id = cal.season_id
		JOIN club.clubs cc ON cc.id = cal.club_id
		ORDER BY r.position`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("standings positions: %w", err)
	}
	defer rows.Close()
	var out []standingsSpot
	for rows.Next() {
		var st standingsSpot
		if err := rows.Scan(&st.ClubID, &st.ClubName, &st.Position, &st.Points, &st.Played, &st.SeasonLabel); err != nil {
			return nil, fmt.Errorf("scan standings spot: %w", err)
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("standings positions rows: %w", err)
	}
	return out, nil
}

// rivalResult is one completed league/domestic matchup against a rival club.
type rivalResult struct {
	FixtureID   uuid.UUID
	HomeClubID  uuid.UUID
	AwayClubID  uuid.UUID
	HomeName    string
	AwayName    string
	HomeScore   *int
	AwayScore   *int
	CompletedAt time.Time
}

// RivalResults returns the most recent completed fixtures between the managed
// clubs and their rivalries, newest first.
func (s *Store) RivalResults(ctx context.Context, worldID uuid.UUID, clubIDs []uuid.UUID, limit int) ([]rivalResult, error) {
	if len(clubIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.home_club_id, f.away_club_id, h.name, a.name,
		       f.ht_score, f.at_score, f.completed_at
		FROM match.fixtures f
		JOIN social.relationships r ON r.world_id = f.world_id AND r.relationship_type = 'rivalry'
		  AND ((r.entity_a_id = f.home_club_id AND r.entity_b_id = f.away_club_id
		        AND r.entity_a_type = 'club' AND r.entity_b_type = 'club')
		    OR (r.entity_a_id = f.away_club_id AND r.entity_b_id = f.home_club_id
		        AND r.entity_a_type = 'club' AND r.entity_b_type = 'club'))
		JOIN club.clubs h ON h.id = f.home_club_id
		JOIN club.clubs a ON a.id = f.away_club_id
		WHERE (f.home_club_id = ANY($1) OR f.away_club_id = ANY($1))
		  AND f.status = 'completed'
		ORDER BY f.completed_at DESC
		LIMIT $2`, clubIDs, limit)
	if err != nil {
		return nil, fmt.Errorf("rival results: %w", err)
	}
	defer rows.Close()
	var out []rivalResult
	for rows.Next() {
		var r rivalResult
		if err := rows.Scan(&r.FixtureID, &r.HomeClubID, &r.AwayClubID, &r.HomeName, &r.AwayName,
			&r.HomeScore, &r.AwayScore, &r.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan rival result: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rival results rows: %w", err)
	}
	return out, nil
}

// marketEvent is one recent listing or completed-transfer world event, with
// the player and club names resolved for the feed.
type marketEvent struct {
	EventID    uuid.UUID
	EventType  string
	PlayerID   uuid.UUID
	PlayerName string
	ClubID     uuid.UUID
	ClubName   string
	Fee        *int64
	OccurredAt time.Time
}

// RecentMarketEvents lists the most recent transfer-market world events in a
// world (listings + completed transfers).
func (s *Store) RecentMarketEvents(ctx context.Context, worldID uuid.UUID, limit int) ([]marketEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.event_type,
		       COALESCE(NULLIF(e.payload->>'player_id', '')::uuid, '00000000-0000-0000-0000-000000000000'),
		       COALESCE(p.display_name, ''),
		       COALESCE(NULLIF(e.payload->>'club_id', '')::uuid,
		                NULLIF(e.payload->>'to_club_id', '')::uuid,
		                '00000000-0000-0000-0000-000000000000'),
		       COALESCE(cc.name, ''),
		       NULLIF(e.payload->>'fee', '')::bigint,
		       e.occurred_at
		FROM world.events e
		LEFT JOIN player.players p ON p.id = NULLIF(e.payload->>'player_id', '')::uuid
		LEFT JOIN club.clubs cc ON cc.id = COALESCE(NULLIF(e.payload->>'club_id', '')::uuid,
		                                            NULLIF(e.payload->>'to_club_id', '')::uuid)
		WHERE e.world_id = $1
		  AND e.event_type IN ('PLAYER_LISTED', 'PLAYER_LISTING_WITHDRAWN', 'TRANSFER_COMPLETED')
		ORDER BY e.occurred_at DESC
		LIMIT $2`, worldID, limit)
	if err != nil {
		return nil, fmt.Errorf("market events: %w", err)
	}
	defer rows.Close()
	var out []marketEvent
	for rows.Next() {
		var m marketEvent
		if err := rows.Scan(&m.EventID, &m.EventType, &m.PlayerID, &m.PlayerName,
			&m.ClubID, &m.ClubName, &m.Fee, &m.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan market event: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("market events rows: %w", err)
	}
	return out, nil
}
