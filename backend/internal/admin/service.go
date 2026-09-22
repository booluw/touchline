package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/pkg/apiref"
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
		WHERE w.club_id = ANY($1) AND w.end_date > CURRENT_DATE`, clubIDs).
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
// Pyramid
// ---------------------------------------------------------------------------

// Pyramid returns the country's leagues ordered by tier then name, each with
// its latest season (if any) and registered club count.
func (s *Service) Pyramid(ctx context.Context, worldID, countryID uuid.UUID) (*Pyramid, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.tier, c.team_count, c.status,
		       r.promotions, r.relegations,
		       (SELECT COUNT(*) FROM competition.club_competitions cc
		         WHERE cc.competition_id = c.id AND cc.role = 'league')
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.world_id = $1 AND c.country_id = $2 AND c.competition_type = 'league'
		ORDER BY c.tier, c.name`, worldID, countryID)
	if err != nil {
		return nil, fmt.Errorf("admin: pyramid: %w", err)
	}
	defer rows.Close()

	py := &Pyramid{Country: country, Leagues: []LeagueRow{}}
	for rows.Next() {
		var l LeagueRow
		if err := rows.Scan(&l.ID, &l.Name, &l.Tier, &l.TeamCount, &l.Status,
			&l.Promotions, &l.Relegations, &l.ClubCount); err != nil {
			return nil, fmt.Errorf("admin: scan pyramid: %w", err)
		}
		py.Leagues = append(py.Leagues, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: iterate pyramid: %w", err)
	}

	// Attach each league's latest (non-completed first, else latest) season.
	for i := range py.Leagues {
		py.Leagues[i].Season = s.latestSeason(ctx, py.Leagues[i].ID)
	}
	return py, nil
}

func (s *Service) latestSeason(ctx context.Context, competitionID uuid.UUID) *SeasonRef {
	var sr SeasonRef
	err := s.pool.QueryRow(ctx, `
		SELECT id, season_label, season_number, status
		FROM competition.seasons
		WHERE competition_id = $1
		ORDER BY (status = 'completed') ASC, season_number DESC LIMIT 1`, competitionID).
		Scan(&sr.SeasonID, &sr.SeasonLabel, &sr.SeasonNumber, &sr.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return &sr
}

// ---------------------------------------------------------------------------
// Clubs
// ---------------------------------------------------------------------------

// Clubs returns the country's clubs with league, squad size, finance sanity
// and crisis state per line.
func (s *Service) Clubs(ctx context.Context, worldID, countryID uuid.UUID) (*ClubsPanels, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	if len(clubIDs) == 0 {
		return &ClubsPanels{Country: country, Clubs: []ClubRow{}}, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.short_name, c.is_ai_controlled, c.tier,
		       cc.competition_id, COALESCE(ccomp.name, ''), COALESCE(ccomp.tier, 0),
		       (SELECT COUNT(*) FROM player.players p WHERE p.club_id = c.id),
		       COALESCE((SELECT COALESCE(SUM(w.weekly_wage)::bigint, 0)
		                  FROM finance.wage_commitments w
		                  WHERE w.club_id = c.id AND w.end_date > CURRENT_DATE), 0),
		       COALESCE((SELECT f.stage FROM finance.financial_crisis_states f
		                  WHERE f.club_id = c.id AND f.resolved_at IS NULL
		                  ORDER BY f.started_at DESC LIMIT 1), ''),
		       COALESCE(top.display_name, ''), COALESCE(top.mv, 0)
		FROM club.clubs c
		JOIN world.countries wc ON wc.world_id = c.world_id AND wc.name = c.country
		LEFT JOIN competition.club_competitions cc
		       ON cc.club_id = c.id AND cc.role = 'league'
		LEFT JOIN competition.competitions ccomp ON ccomp.id = cc.competition_id
		LEFT JOIN LATERAL (
			SELECT pe.display_name, p.market_value::bigint AS mv
			FROM player.players p
			JOIN person.people pe ON pe.id = p.person_id
			WHERE p.club_id = c.id
			ORDER BY p.market_value DESC NULLS LAST
			LIMIT 1
		) top ON TRUE
		WHERE wc.id = $1
		ORDER BY c.name`, countryID)
	if err != nil {
		return nil, fmt.Errorf("admin: clubs: %w", err)
	}
	defer rows.Close()

	panel := &ClubsPanels{Country: country, Clubs: []ClubRow{}}
	for rows.Next() {
		var r ClubRow
		if err := rows.Scan(&r.ID, &r.Name, &r.ShortName, &r.IsAIControlled, &r.Tier,
			&r.LeagueID, &r.LeagueName, &r.LeagueTier,
			&r.SquadSize, &r.WageBill, &r.CrisisStage, &r.TopPlayerName, &r.TopPlayerMarket); err != nil {
			return nil, fmt.Errorf("admin: scan club: %w", err)
		}
		if r.CrisisStage != nil && *r.CrisisStage == "" {
			r.CrisisStage = nil
		}
		if r.LeagueID != nil {
			r.League = &apiref.LeagueRef{ID: *r.LeagueID, Name: r.LeagueName}
		}
		panel.Clubs = append(panel.Clubs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: iterate clubs: %w", err)
	}

	// Budget envelopes (latest season per club+type) + cash, batch joined.
	budgets, err := s.clubBudgets(ctx, clubIDs)
	if err != nil {
		return nil, err
	}
	cash, err := s.clubCash(ctx, clubIDs)
	if err != nil {
		return nil, err
	}
	for i := range panel.Clubs {
		if b, ok := budgets[panel.Clubs[i].ID]; ok {
			panel.Clubs[i].WageAllocated = b.WageAllocated
			panel.Clubs[i].WageCommitted = b.WageCommitted
			panel.Clubs[i].TransferAllocated = b.TransferAllocated
			panel.Clubs[i].TransferCommitted = b.TransferCommitted
		}
		panel.Clubs[i].Cash = cash[panel.Clubs[i].ID]
	}
	return panel, nil
}

type clubBudgetLine struct {
	WageAllocated, WageCommitted, TransferAllocated, TransferCommitted int64
}

func (s *Service) clubBudgets(ctx context.Context, clubIDs []uuid.UUID) (map[uuid.UUID]clubBudgetLine, error) {
	out := map[uuid.UUID]clubBudgetLine{}
	rows, err := s.pool.Query(ctx, `
		SELECT b.club_id, b.budget_type, b.allocated_amount::bigint, b.committed_amount::bigint
		FROM (
			SELECT DISTINCT ON (b.club_id, b.budget_type) b.club_id, b.budget_type,
			       b.allocated_amount, b.committed_amount
			FROM finance.budgets b
			WHERE b.club_id = ANY($1)
			ORDER BY b.club_id, b.budget_type, b.season DESC
		) b`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("admin: club budgets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var typ string
		var alloc, comm int64
		if err := rows.Scan(&id, &typ, &alloc, &comm); err != nil {
			return nil, fmt.Errorf("admin: scan budget: %w", err)
		}
		line := out[id]
		if typ == "wage" {
			line.WageAllocated, line.WageCommitted = alloc, comm
		} else {
			line.TransferAllocated, line.TransferCommitted = alloc, comm
		}
		out[id] = line
	}
	return out, rows.Err()
}

func (s *Service) clubCash(ctx context.Context, clubIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	rows, err := s.pool.Query(ctx, `
		SELECT a.club_id,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'credit')::bigint, 0)
		         - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'debit')::bigint, 0)
		FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = ANY($1)
		GROUP BY a.club_id`, clubIDs)
	if err != nil {
		return nil, fmt.Errorf("admin: club cash: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var cash int64
		if err := rows.Scan(&id, &cash); err != nil {
			return nil, fmt.Errorf("admin: scan cash: %w", err)
		}
		out[id] = cash
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Players
// ---------------------------------------------------------------------------

// PlayerSummary aggregates the country's player population.
func (s *Service) PlayerSummary(ctx context.Context, worldID, countryID uuid.UUID) (*PlayerSummary, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	ps := &PlayerSummary{Country: country,
		ByStatus: map[string]int{}, ByOrigin: map[string]int{},
		ByPosition: []PositionBreakdown{}, Nationalities: []NationalityMix{}, Intakes: []IntakeRow{}}

	s.pool.QueryRow(ctx, `
		SELECT COUNT(*), ROUND(AVG(EXTRACT(EPOCH FROM (age('now'::date, pe.date_of_birth))) / 31536000.0)::numeric, 1)::float8
		FROM player.players p JOIN person.people pe ON pe.id = p.person_id
		WHERE p.world_id = $1 AND p.country_id = $2`, worldID, countryID).
		Scan(&ps.Total, &ps.AvgAge)

	ps.ByStatus = s.scanCountBy(ctx, `
		SELECT status, COUNT(*) FROM player.players
		WHERE world_id = $1 AND country_id = $2 GROUP BY status`, worldID, countryID)
	ps.ByOrigin = s.scanCountBy(ctx, `
		SELECT origin, COUNT(*) FROM player.players
		WHERE world_id = $1 AND country_id = $2 GROUP BY origin`, worldID, countryID)

	if err := s.scanPositions(ctx, worldID, countryID, ps); err != nil {
		return nil, err
	}
	if err := s.scanNationalities(ctx, worldID, countryID, ps); err != nil {
		return nil, err
	}
	s.scanIntakes(ctx, worldID, countryID, ps)
	return ps, nil
}

func (s *Service) scanCountBy(ctx context.Context, query string, args ...any) map[string]int {
	out := map[string]int{}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err == nil && key != "" {
			out[key] = n
		}
	}
	return out
}

func (s *Service) scanPositions(ctx context.Context, worldID, countryID uuid.UUID, ps *PlayerSummary) error {
	rows, err := s.pool.Query(ctx, `
		SELECT p.primary_position,
		       COUNT(*),
		       ROUND(COALESCE(AVG(a.technical), 0)::numeric, 1)::float8, ROUND(COALESCE(AVG(a.physical), 0)::numeric, 1)::float8,
		       ROUND(COALESCE(AVG(a.mental), 0)::numeric, 1)::float8, ROUND(COALESCE(AVG(a.tactical), 0)::numeric, 1)::float8,
		       ROUND(COALESCE(AVG(a.goalkeeping), 0)::numeric, 1)::float8, ROUND(COALESCE(AVG(a.positional), 0)::numeric, 1)::float8,
		       ROUND(COALESCE(AVG(ht.potential), 0)::numeric, 1)::float8
		FROM player.players p
		LEFT JOIN LATERAL (
			SELECT AVG(value) FILTER (WHERE attribute_category = 'technical')  AS technical,
			       AVG(value) FILTER (WHERE attribute_category = 'physical')  AS physical,
			       AVG(value) FILTER (WHERE attribute_category = 'mental')    AS mental,
			       AVG(value) FILTER (WHERE attribute_category = 'tactical')  AS tactical,
			       AVG(value) FILTER (WHERE attribute_category = 'goalkeeping') AS goalkeeping,
			       AVG(value) FILTER (WHERE attribute_category = 'positional') AS positional
			FROM player.player_attributes pa WHERE pa.player_id = p.id
		) a ON TRUE
		LEFT JOIN player.player_hidden_traits ht ON ht.player_id = p.id
		WHERE p.world_id = $1 AND p.country_id = $2
		GROUP BY p.primary_position
		ORDER BY p.primary_position`, worldID, countryID)
	if err != nil {
		return fmt.Errorf("admin: positions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b PositionBreakdown
		var tech, phy, men, tac, gk, pos, pot float64
		if err := rows.Scan(&b.Position, &b.Count, &tech, &phy, &men, &tac, &gk, &pos, &pot); err != nil {
			return fmt.Errorf("admin: scan position: %w", err)
		}
		b.AvgOverall = squad.PositionalOverall(b.Position, squad.AttributeSnapshot{
			Technical:   int(math.Round(tech)),
			Physical:    int(math.Round(phy)),
			Mental:      int(math.Round(men)),
			Tactical:    int(math.Round(tac)),
			Goalkeeping: int(math.Round(gk)),
			Positional:  int(math.Round(pos)),
		})
		b.AvgPotential = int(math.Round(pot))
		ps.ByPosition = append(ps.ByPosition, b)
	}
	return rows.Err()
}

func (s *Service) scanNationalities(ctx context.Context, worldID, countryID uuid.UUID, ps *PlayerSummary) error {
	rows, err := s.pool.Query(ctx, `
		SELECT pe.nationality_code, COALESCE(n.name, pe.nationality_code), COUNT(*)
		FROM player.players p
		JOIN person.people pe ON pe.id = p.person_id
		LEFT JOIN ref.nationalities n ON n.code = pe.nationality_code
		WHERE p.world_id = $1 AND p.country_id = $2
		GROUP BY pe.nationality_code, n.name
		ORDER BY COUNT(*) DESC`, worldID, countryID)
	if err != nil {
		return fmt.Errorf("admin: nationalities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m NationalityMix
		if err := rows.Scan(&m.Code, &m.Name, &m.Count); err != nil {
			return fmt.Errorf("admin: scan nationality: %w", err)
		}
		ps.Nationalities = append(ps.Nationalities, m)
	}
	return rows.Err()
}

func (s *Service) scanIntakes(ctx context.Context, worldID, countryID uuid.UUID, ps *PlayerSummary) {
	rows, err := s.pool.Query(ctx, `
		SELECT season_number, SUM(player_count) FROM world.country_academy_intakes
		WHERE world_id = $1 AND country_id = $2
		GROUP BY season_number ORDER BY season_number`, worldID, countryID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ir IntakeRow
		if err := rows.Scan(&ir.SeasonNumber, &ir.PlayerCount); err == nil {
			ps.Intakes = append(ps.Intakes, ir)
		}
	}
}

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

// ---------------------------------------------------------------------------
// Finance
// ---------------------------------------------------------------------------

// Finance returns the country economy: crisis clubs, wage bill and budgets.
func (s *Service) Finance(ctx context.Context, worldID, countryID uuid.UUID) (*FinancePanels, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	f := &FinancePanels{Country: country, CrisisClubs: []CrisisClubRow{}, TopWageBills: []WageRow{}}
	if len(clubIDs) == 0 {
		return f, nil
	}
	f.CrisisClubs = s.crisisClubs(ctx, clubIDs)
	es := s.economySummary(ctx, clubIDs)
	f.WageBill = es.WageBill
	f.Cash = es.Cash
	f.WageBudget.Allocated = es.WageAllocated
	f.WageBudget.Committed = es.WageCommitted
	f.WageBudget.Available = es.WageAllocated - es.WageCommitted
	f.WageBudget.UtilizedPct = pct(es.WageCommitted, es.WageAllocated)
	f.TransferBudget.Allocated = es.TransferAllocated
	f.TransferBudget.Committed = es.TransferCommitted
	f.TransferBudget.Available = es.TransferAllocated - es.TransferCommitted
	f.TransferBudget.UtilizedPct = pct(es.TransferCommitted, es.TransferAllocated)
	f.TopWageBills = s.topWageBills(ctx, clubIDs)
	return f, nil
}

func (s *Service) crisisClubs(ctx context.Context, clubIDs []uuid.UUID) []CrisisClubRow {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.short_name, f.stage, f.started_at
		FROM finance.financial_crisis_states f
		JOIN club.clubs c ON c.id = f.club_id
		WHERE f.club_id = ANY($1) AND f.resolved_at IS NULL
		ORDER BY f.started_at DESC`, clubIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []CrisisClubRow{}
	for rows.Next() {
		var r CrisisClubRow
		if err := rows.Scan(&r.ClubID, &r.ClubName, &r.Stage, &r.StartedAt); err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

func (s *Service) topWageBills(ctx context.Context, clubIDs []uuid.UUID) []WageRow {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.short_name, COALESCE(SUM(w.weekly_wage)::bigint, 0), COUNT(w.id)
		FROM club.clubs c
		LEFT JOIN finance.wage_commitments w
		  ON w.club_id = c.id AND w.end_date > CURRENT_DATE
		WHERE c.id = ANY($1)
		GROUP BY c.id
		ORDER BY COALESCE(SUM(w.weekly_wage), 0) DESC
		LIMIT 20`, clubIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []WageRow{}
	for rows.Next() {
		var r WageRow
		if err := rows.Scan(&r.ClubID, &r.ClubName, &r.WeeklyBill, &r.Count); err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

func pct(part, whole int64) int {
	if whole <= 0 {
		return 0
	}
	return int(float64(part) / float64(whole) * 100)
}

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

// Timeline returns the country's recent events. Payload-relevant ids are
// matched against the country's clubs and player population (origin pool or
// current club), so a transfer between two foreign clubs of an event whose
// player is English-origin still lands in England's feed.
func (s *Service) Timeline(ctx context.Context, worldID, countryID uuid.UUID, days int, limit int) (*Timeline, error) {
	country, err := s.ResolveCountry(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 14
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	since := time.Now().UTC().AddDate(0, 0, -days)

	clubIDs, err := s.CountryClubIDs(ctx, worldID, countryID)
	if err != nil {
		return nil, err
	}
	relevant := map[uuid.UUID]bool{countryID: true}
	for _, id := range clubIDs {
		relevant[id] = true
	}
	if err := s.markPlayerRelevance(ctx, worldID, countryID, clubIDs, relevant); err != nil {
		return nil, err
	}

	names := s.nameMaps(ctx, worldID, clubIDs, relevant)

	rows, err := s.pool.Query(ctx, `
		SELECT id, event_type, occurred_at, actor_type, payload
		FROM world.events
		WHERE world_id = $1
		  AND event_type IN ('TRANSFER_COMPLETED','BID_PLACED','BID_ACCEPTED','BID_COUNTERED',
		                         'PLAYER_SIGNED','PLAYER_RELEASED','PLAYER_CLAIMED_FROM_POOL',
		                         'COUNTRY_ACADEMY_INTAKE','PLAYER_RETIRED','PLAYER_LISTED',
		                         'PLAYER_LISTING_WITHDRAWN','CLUB_RENAMED')
		  AND occurred_at >= $2
		ORDER BY occurred_at DESC
		LIMIT $3`, worldID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("admin: timeline: %w", err)
	}
	defer rows.Close()

	tl := &Timeline{Country: country, Since: since, Items: []TimelineItem{}}
	for rows.Next() {
		var it TimelineItem
		var actorType *string
		var raw []byte
		if err := rows.Scan(&it.ID, &it.EventType, &it.OccurredAt, &actorType, &raw); err != nil {
			return nil, fmt.Errorf("admin: scan timeline: %w", err)
		}
		it.ActorType = actorType
		_ = json.Unmarshal(raw, &it.Payload)
		if !referencesCountry(it.Payload, relevant) {
			continue
		}
		it.Title = headlineFor(it.EventType, it.Payload, names)
		tl.Items = append(tl.Items, it)
	}
	return tl, rows.Err()
}

func (s *Service) markPlayerRelevance(ctx context.Context, worldID, countryID uuid.UUID, clubIDs []uuid.UUID, relevant map[uuid.UUID]bool) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM player.players
		WHERE world_id = $1 AND (country_id = $2 OR club_id = ANY($3))`,
		worldID, countryID, clubIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		relevant[id] = true
	}
	return rows.Err()
}

type nameMaps struct {
	players map[uuid.UUID]string
	clubs   map[uuid.UUID]string
}

func (s *Service) nameMaps(ctx context.Context, worldID uuid.UUID, clubIDs []uuid.UUID, relevant map[uuid.UUID]bool) nameMaps {
	nm := nameMaps{players: map[uuid.UUID]string{}, clubs: map[uuid.UUID]string{}}
	rows, err := s.pool.Query(ctx, `SELECT id, short_name FROM club.clubs WHERE id = ANY($1)`, clubIDs)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err == nil {
				nm.clubs[id] = name
			}
		}
		rows.Close()
	}
	rows, err = s.pool.Query(ctx, `
		SELECT p.id, pe.display_name
		FROM player.players p JOIN person.people pe ON pe.id = p.person_id
		WHERE p.world_id = $1 AND p.id = ANY($2)`, worldID, keysToSlice(relevant))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var name string
			if err := rows.Scan(&id, &name); err == nil {
				nm.players[id] = name
			}
		}
	}
	return nm
}

func keysToSlice(set map[uuid.UUID]bool) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// referencesCountry reports whether any uuid-looking payload value matches a
// country-relevant entity (club, player, or the country itself).
func referencesCountry(payload map[string]any, relevant map[uuid.UUID]bool) bool {
	for _, v := range payload {
		switch t := v.(type) {
		case string:
			if id, err := uuid.Parse(t); err == nil && relevant[id] {
				return true
			}
		case []any:
			for _, e := range t {
				if s, ok := e.(string); ok {
					if id, err := uuid.Parse(s); err == nil && relevant[id] {
						return true
					}
				}
			}
		}
	}
	return false
}

// headlineFor renders a readable one-liner from the payload and known names.
func headlineFor(eventType string, payload map[string]any, names nameMaps) string {
	first := func(keys ...string) string {
		for _, k := range keys {
			if raw, ok := payload[k]; ok {
				if s, ok := raw.(string); ok {
					if id, err := uuid.Parse(s); err == nil {
						if n, ok := names.players[id]; ok {
							return n
						}
						if n, ok := names.clubs[id]; ok {
							return n
						}
					}
				}
			}
		}
		return ""
	}
	str := func(key string) string {
		if raw, ok := payload[key]; ok {
			if s, ok := raw.(string); ok {
				return s
			}
		}
		return ""
	}
	players := first("player_id", "player_ids")
	clubs := first("to_club_id", "from_club_id", "club_id", "selling_club_id", "bidding_club_id", "listing_club_id")

	switch eventType {
	case "TRANSFER_COMPLETED":
		return joinNames(players, clubs)
	case "CLUB_RENAMED":
		oldN := str("old_name")
		newN := str("new_name")
		if oldN != "" && newN != "" {
			return fmt.Sprintf("%s renamed to %s", oldN, newN)
		}
		return eventType
	case "PLAYER_SIGNED", "BID_PLACED", "BID_ACCEPTED", "BID_COUNTERED", "PLAYER_RELEASED",
		"PLAYER_CLAIMED_FROM_POOL", "PLAYER_LISTED", "PLAYER_LISTING_WITHDRAWN":
		return fmt.Sprintf("%s: %s", eventType, joinNames(players, clubs))
	default:
		if players != "" {
			return fmt.Sprintf("%s: %s", eventType, players)
		}
		return eventType
	}
}

func joinNames(a, b string) string {
	switch {
	case a == "" && b == "":
		return ""
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " / " + b
	}
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
