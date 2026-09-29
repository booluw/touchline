package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// Service runs the per-country admin dashboard queries plus the admin club
// rename surface (which publishes CLUB_RENAMED events + news stories).
type Service struct {
	pool *pgxpool.Pool
	pub  eventbus.Publisher // may be nil: log-only event writing
}

// NewService builds the country dashboard read service.
func NewService(pool *pgxpool.Pool, pub eventbus.Publisher) *Service {
	return &Service{pool: pool, pub: pub}
}

// Sentinel errors surfaced by handlers.
var (
	ErrCountryNotFound = errors.New("country not found in world")
)

// ResolveCountry verifies the country belongs to the world and returns its ref.
func (s *Service) ResolveCountry(ctx context.Context, worldID, countryID uuid.UUID) (*CountryRef, error) {
	var c CountryRef
	err := s.pool.QueryRow(ctx, `
		SELECT id, world_id, code, name FROM world.countries
		WHERE id = $1 AND world_id = $2`, countryID, worldID).
		Scan(&c.ID, &c.WorldID, &c.Code, &c.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("admin: resolve country: %w", err)
	}
	return &c, nil
}

// CountryClubIDs resolves the club ids whose country name matches this world
// country (name is the only club-to-country linkage, best-effort).
func (s *Service) CountryClubIDs(ctx context.Context, worldID, countryID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id
		FROM club.clubs c
		JOIN world.countries wc ON wc.world_id = c.world_id AND wc.name = c.country
		WHERE wc.id = $1`, countryID)
	if err != nil {
		return nil, fmt.Errorf("admin: country clubs: %w", err)
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("admin: scan club: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Overview composes the country's home counters in one pass.
func (s *Service) Overview(ctx context.Context, worldID, countryID uuid.UUID) (*Overview, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}

	ov := &Overview{Country: country, AsOf: time.Now().UTC()}
	ov.Population = s.populationSummary(ctx, worldID, countryID)
	ov.Unassigned = s.unassignedSummary(ctx, worldID)
	ov.Clubs = s.clubSummary(ctx, worldID, countryID, clubIDs)
	ov.Leagues = s.leagueSummary(ctx, countryID)
	ov.Market = s.marketSummary(ctx, worldID, countryID, clubIDs)
	ov.Economy = s.economySummary(ctx, clubIDs)
	ov.Headlines = s.headlines(ctx, countryID, clubIDs, country)
	return ov, nil
}

func (s *Service) populationSummary(ctx context.Context, worldID, countryID uuid.UUID) PopulationSummary {
	ps := PopulationSummary{ByStatus: map[string]int{}}
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE p.status <> 'retired'),
		       COUNT(*) FILTER (WHERE p.status = 'free_agent'),
		       COUNT(*) FILTER (WHERE p.status = 'retired'),
		       COUNT(*)
		FROM player.players p
		WHERE p.world_id = $1 AND p.country_id = $2`, worldID, countryID).
		Scan(&ps.Active, &ps.FreeAgent, &ps.Retired, &ps.Total)
	rows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM player.players
		WHERE world_id = $1 AND country_id = $2 GROUP BY status`, worldID, countryID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var n int
			if err := rows.Scan(&status, &n); err == nil {
				ps.ByStatus[status] = n
			}
		}
	}
	return ps
}

func (s *Service) unassignedSummary(ctx context.Context, worldID uuid.UUID) UnassignedSummary {
	var u UnassignedSummary
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM player.players WHERE world_id = $1 AND country_id IS NULL`, worldID).
		Scan(&u.WorldPoolPlayers)
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM club.clubs c
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND wc.name = c.country
		WHERE c.world_id = $1 AND wc.id IS NULL`, worldID).
		Scan(&u.OrphanClubs)
	return u
}

func (s *Service) clubSummary(ctx context.Context, worldID, countryID uuid.UUID, clubIDs []uuid.UUID) ClubSummary {
	cs := ClubSummary{Total: len(clubIDs)}
	if len(clubIDs) > 0 {
		s.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM finance.financial_crisis_states f
			WHERE f.club_id = ANY($1) AND f.resolved_at IS NULL`, clubIDs).Scan(&cs.InCrisis)
	}
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM club.clubs c
		LEFT JOIN world.countries wc ON wc.world_id = c.world_id AND wc.name = c.country
		WHERE c.world_id = $1 AND wc.id IS NULL`, worldID).
		Scan(&cs.Orphans)
	cs.Unassigned = cs.Orphans
	return cs
}

func (s *Service) leagueSummary(ctx context.Context, countryID uuid.UUID) LeagueSummary {
	ls := LeagueSummary{ByTier: map[int]int{}}
	rows, err := s.pool.Query(ctx, `
		SELECT tier, COUNT(*) FROM competition.competitions
		WHERE country_id = $1 AND competition_type = 'league' GROUP BY tier`, countryID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var tier int
			var n int
			if err := rows.Scan(&tier, &n); err == nil {
				ls.ByTier[tier] = n
				ls.Total += n
			}
		}
	}
	return ls
}

func (s *Service) marketSummary(ctx context.Context, worldID, countryID uuid.UUID, clubIDs []uuid.UUID) MarketSummary {
	var ms MarketSummary
	if len(clubIDs) == 0 {
		return ms
	}
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transfer.listings l
		WHERE l.world_id = $1 AND l.status = 'active' AND l.listing_club_id = ANY($2)`,
		worldID, clubIDs).Scan(&ms.OpenListings)
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transfer.bids b
		WHERE b.world_id = $1 AND b.status IN ('pending','countered') AND b.selling_club_id = ANY($2)`,
		worldID, clubIDs).Scan(&ms.BidsReceived)
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transfer.bids b
		WHERE b.world_id = $1 AND b.status IN ('pending','countered') AND b.bidding_club_id = ANY($2)`,
		worldID, clubIDs).Scan(&ms.BidsMade)
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transfer.completed_transfers t
		WHERE t.world_id = $1 AND t.to_club_id = ANY($2) AND t.from_club_id IS NOT NULL`,
		worldID, clubIDs).Scan(&ms.TransfersIn)
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transfer.completed_transfers t
		WHERE t.world_id = $1 AND t.from_club_id = ANY($2)`,
		worldID, clubIDs).Scan(&ms.TransfersOut)
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM transfer.completed_transfers t
		WHERE t.world_id = $1 AND t.to_club_id = ANY($2) AND t.from_club_id IS NULL`,
		worldID, clubIDs).Scan(&ms.FreeAgentSignings)
	return ms
}

func (s *Service) economySummary(ctx context.Context, clubIDs []uuid.UUID) EconomySummary {
	var es EconomySummary
	if len(clubIDs) == 0 {
		return es
	}
	s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM finance.financial_crisis_states f
		WHERE f.club_id = ANY($1) AND f.resolved_at IS NULL`, clubIDs).
		Scan(&es.CrisisClubs)
	s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(w.weekly_wage)::bigint, 0)
		FROM finance.wage_commitments w
		WHERE w.club_id = ANY($1) AND w.end_date > world.club_world_date(w.club_id)`, clubIDs).
		Scan(&es.WageBill)
	s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'credit')::bigint, 0)
		     - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'debit')::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = ANY($1)`, clubIDs).
		Scan(&es.Cash)

	// Budget capacity = the latest season's envelope per (club, type),
	// summed across the country (matches the wallet's MAX(season) rule).
	rows, err := s.pool.Query(ctx, `
		SELECT b.budget_type, b.allocated_amount, b.committed_amount
		FROM (
			SELECT DISTINCT ON (b.club_id, b.budget_type) b.club_id, b.budget_type,
			       b.allocated_amount, b.committed_amount
			FROM finance.budgets b
			WHERE b.club_id = ANY($1)
			ORDER BY b.club_id, b.budget_type, b.season DESC
		) b`, clubIDs)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var typ string
			var alloc, comm int64
			if err := rows.Scan(&typ, &alloc, &comm); err == nil {
				if typ == "wage" {
					es.WageAllocated += alloc
					es.WageCommitted += comm
				} else if typ == "transfer" {
					es.TransferAllocated += alloc
					es.TransferCommitted += comm
				}
			}
		}
	}
	return es
}

// ---------------------------------------------------------------------------
// Headlines (overview)
// ---------------------------------------------------------------------------

func (s *Service) headlines(ctx context.Context, countryID uuid.UUID, clubIDs []uuid.UUID, country *CountryRef) []Headline {
	if len(clubIDs) == 0 {
		return []Headline{}
	}
	hl := []Headline{}

	rows, err := s.pool.Query(ctx, `
		SELECT pe.display_name, tc.short_name, COALESCE(fc.short_name, ''), t.fee::bigint,
		       p.market_value::bigint, t.completed_at, (t.from_club_id IS NULL)
		FROM transfer.completed_transfers t
		JOIN player.players p ON p.id = t.player_id
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN club.clubs fc ON fc.id = t.from_club_id
		JOIN club.clubs tc ON tc.id = t.to_club_id
		WHERE t.world_id = (SELECT world_id FROM world.countries WHERE id = $2)
		  AND (t.to_club_id = ANY($1) OR t.from_club_id = ANY($1))
		  AND t.completed_at >= now() - interval '90 days'
		ORDER BY t.fee DESC
		LIMIT 3`, clubIDs, countryID)
	if err == nil {
		for rows.Next() {
			var player, to, from string
			var fee, mv int64
			var at time.Time
			var signing bool
			if err := rows.Scan(&player, &to, &from, &fee, &mv, &at, &signing); err != nil {
				break
			}
			if signing {
				hl = append(hl, Headline{Kind: "signing", Title: player + " signed",
					Detail: to, OccurredAt: at})
				continue
			}
			if from == "" {
				hl = append(hl, Headline{Kind: "transfer_in", Title: player + " joined",
					Detail: fmt.Sprintf("%s · fee £%d", to, fee), OccurredAt: at})
			} else {
				hl = append(hl, Headline{Kind: "transfer_out", Title: player + " left",
					Detail: fmt.Sprintf("%s → %s · fee £%d", from, to, fee), OccurredAt: at})
			}
		}
		rows.Close()
	}

	var crisis *CrisisClubRow
	s.pool.QueryRow(ctx, `
		SELECT c.short_name, f.stage, f.started_at
		FROM finance.financial_crisis_states f
		JOIN club.clubs c ON c.id = f.club_id
		WHERE f.club_id = ANY($1) AND f.resolved_at IS NULL
		ORDER BY f.started_at DESC LIMIT 1`, clubIDs).
		Scan(&crisis)
	if crisis != nil {
		hl = append(hl, Headline{Kind: "crisis", Title: crisis.ClubName + " in " + crisis.Stage,
			OccurredAt: crisis.StartedAt})
	}

	var intake *IntakeRow
	s.pool.QueryRow(ctx, `
		SELECT season_number, SUM(player_count) FROM world.country_academy_intakes
		WHERE world_id = (SELECT world_id FROM world.countries WHERE id = $1)
		  AND country_id = $1
		GROUP BY season_number ORDER BY season_number DESC LIMIT 1`, countryID).Scan(&intake)
	if intake != nil {
		hl = append(hl, Headline{Kind: "intake",
			Title:      fmt.Sprintf("%d new youth prospects in season %d", intake.PlayerCount, intake.SeasonNumber),
			OccurredAt: time.Now().UTC()})
	}

	return hl
}
