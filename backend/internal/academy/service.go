package academy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/explanation"
	"github.com/touchline/backend/pkg/playergen"
)

// Service is the academy domain service: configuration, maintenance and the
// seasonal procedural intake.
type Service struct {
	pool  *pgxpool.Pool
	bus   eventbus.Publisher
	store *store
}

// NewService builds the academy service.
func NewService(pool *pgxpool.Pool, bus eventbus.Publisher) *Service {
	return &Service{pool: pool, bus: bus, store: newStore(pool)}
}

// EnsureAcademies creates a default tier-1 academy row for every club in the
// world that does not already have one. Idempotent; called during seeding and
// available as a repair pass.
func (s *Service) EnsureAcademies(ctx context.Context, worldID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO club.academies (club_id, world_id)
		SELECT c.id, c.world_id FROM club.clubs c
		WHERE c.world_id = $1
		ON CONFLICT (club_id) DO NOTHING`, worldID); err != nil {
		return fmt.Errorf("ensure academies: %w", err)
	}
	return nil
}

// GetAcademy returns a club's academy config, lazily creating the default row
// on first read so every club has an academy without a seeding dependency.
func (s *Service) GetAcademy(ctx context.Context, clubID uuid.UUID) (Academy, error) {
	a, err := s.store.loadAcademy(ctx, clubID)
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, errAcademyMissing) {
		return Academy{}, err
	}
	var worldID uuid.UUID
	if err := s.pool.QueryRow(ctx, `SELECT world_id FROM club.clubs WHERE id = $1`, clubID).Scan(&worldID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Academy{}, ErrClubNotFound
		}
		return Academy{}, fmt.Errorf("get academy: load club: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Academy{}, fmt.Errorf("get academy: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := ensureAcademyTx(ctx, tx, worldID, clubID); err != nil {
		return Academy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Academy{}, fmt.Errorf("get academy: commit: %w", err)
	}
	return s.store.loadAcademy(ctx, clubID)
}

// RequireOwnership enforces that the actor currently manages the club.
func (s *Service) RequireOwnership(ctx context.Context, managerID, clubID uuid.UUID) error {
	var current uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT current_club_id FROM manager.managers
		WHERE id = $1 AND status = 'active' AND current_club_id IS NOT NULL`, managerID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) || current != clubID {
		return ErrNotOwned
	}
	if err != nil {
		return fmt.Errorf("academy: ownership: %w", err)
	}
	return nil
}

// SetInvestment changes a club's academy investment tier on behalf of its
// manager (ownership-gated). Tier drives facility/scouting/staff levels, the
// annual maintenance cost, and the talent profile of future intakes.
func (s *Service) SetInvestment(ctx context.Context, managerID, clubID uuid.UUID, tier int) (Academy, error) {
	if err := s.RequireOwnership(ctx, managerID, clubID); err != nil {
		return Academy{}, err
	}
	actorType, actorID := "manager", &managerID
	return s.configure(ctx, clubID, AcademyConfig{InvestmentTier: tier}, actorType, actorID)
}

// SetActive shuts down or reopens a club's academy on behalf of its manager.
// Shutting down is immediate cash relief with a supporter-sentiment cost.
func (s *Service) SetActive(ctx context.Context, managerID, clubID uuid.UUID, active bool) (Academy, error) {
	if err := s.RequireOwnership(ctx, managerID, clubID); err != nil {
		return Academy{}, err
	}
	actorType, actorID := "manager", &managerID
	return s.setActive(ctx, clubID, active, actorType, actorID)
}

// ConfigureAcademy is the un-gated configuration core (seeding/admin): it
// applies any non-zero field of cfg, leaving others untouched.
func (s *Service) ConfigureAcademy(ctx context.Context, clubID uuid.UUID, cfg AcademyConfig) (Academy, error) {
	return s.configure(ctx, clubID, cfg, "system", nil)
}

// configure applies an AcademyConfig inside one transaction, posting the
// one-off upgrade cost when the tier rises and emitting the change event.
func (s *Service) configure(ctx context.Context, clubID uuid.UUID, cfg AcademyConfig, actorType string, actorID *uuid.UUID) (Academy, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Academy{}, fmt.Errorf("configure academy: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ctxClub, err := loadClubContext(ctx, tx, clubID)
	if err != nil {
		return Academy{}, err
	}
	if err := ensureAcademyTx(ctx, tx, ctxClub.WorldID, clubID); err != nil {
		return Academy{}, err
	}
	a, err := lockAcademyTx(ctx, tx, clubID)
	if err != nil {
		return Academy{}, fmt.Errorf("configure academy: lock: %w", err)
	}
	oldTier := a.InvestmentTier

	if cfg.InvestmentTier != 0 {
		if cfg.InvestmentTier < MinTier || cfg.InvestmentTier > MaxTier {
			return Academy{}, ErrInvalidTier
		}
		a.InvestmentTier = cfg.InvestmentTier
	}
	if cfg.FacilityLevel > 0 {
		a.FacilityLevel = clampTier(cfg.FacilityLevel)
	}
	if cfg.ScoutingLevel > 0 {
		a.ScoutingLevel = clampTier(cfg.ScoutingLevel)
	}
	if cfg.StaffQuality > 0 {
		a.StaffQuality = clampTier(cfg.StaffQuality)
	}
	if cfg.RegionalReach != nil {
		a.RegionalReach = cfg.RegionalReach
	}
	// A tier move restates the derived levels so the cost and the UI agree.
	if cfg.InvestmentTier != 0 && cfg.FacilityLevel == 0 && cfg.ScoutingLevel == 0 && cfg.StaffQuality == 0 {
		a.FacilityLevel = a.InvestmentTier
		a.ScoutingLevel = a.InvestmentTier
		a.StaffQuality = a.InvestmentTier
	}
	a.AnnualCost = AnnualCostByTier[clampTier(a.InvestmentTier)]

	if _, err := tx.Exec(ctx, `
		UPDATE club.academies SET
			investment_tier = $2, facility_level = $3, scouting_level = $4,
			staff_quality = $5, annual_cost = $6, regional_reach = $7
		WHERE club_id = $1`,
		clubID, a.InvestmentTier, a.FacilityLevel, a.ScoutingLevel,
		a.StaffQuality, a.AnnualCost, a.RegionalReach); err != nil {
		return Academy{}, fmt.Errorf("configure academy: update: %w", err)
	}

	upgradeCost := int64(0)
	if a.InvestmentTier > oldTier {
		upgradeCost = UpgradeCostByTier[a.InvestmentTier]
		if upgradeCost > 0 {
			accountID, err := finance.EnsureAccount(ctx, tx, ctxClub.WorldID, clubID)
			if err != nil {
				return Academy{}, err
			}
			if _, err := finance.Post(ctx, tx, accountID, "debit", "facilities", upgradeCost,
				fmt.Sprintf("Academy upgrade to tier %d", a.InvestmentTier), nil, time.Now().UTC(),
				fmt.Sprintf("academy:upgrade:%s:%d", clubID, a.InvestmentTier)); err != nil {
				return Academy{}, err
			}
		}
	}

	payload := mustJSON(map[string]any{
		"club_id":      clubID,
		"from_tier":    oldTier,
		"to_tier":      a.InvestmentTier,
		"annual_cost":  a.AnnualCost,
		"upgrade_cost": upgradeCost,
	})
	if err := s.recordEvent(ctx, tx, ctxClub.WorldID, EventInvestmentChange, actorType, actorID, payload, nil); err != nil {
		return Academy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Academy{}, fmt.Errorf("configure academy: commit: %w", err)
	}
	a.ClubID, a.WorldID = clubID, ctxClub.WorldID
	return a, nil
}

// setActive flips an academy's operational flag, stamping the shutdown/reopen
// time and applying the supporter-sentiment penalty on shutdown.
func (s *Service) setActive(ctx context.Context, clubID uuid.UUID, active bool, actorType string, actorID *uuid.UUID) (Academy, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Academy{}, fmt.Errorf("set academy active: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ctxClub, err := loadClubContext(ctx, tx, clubID)
	if err != nil {
		return Academy{}, err
	}
	if err := ensureAcademyTx(ctx, tx, ctxClub.WorldID, clubID); err != nil {
		return Academy{}, err
	}
	a, err := lockAcademyTx(ctx, tx, clubID)
	if err != nil {
		return Academy{}, fmt.Errorf("set academy active: lock: %w", err)
	}
	if a.IsActive == active {
		a.ClubID, a.WorldID = clubID, ctxClub.WorldID
		return a, nil // no-op; nothing to write or emit
	}

	if _, err := tx.Exec(ctx, `
		UPDATE club.academies SET
			is_active = $2,
			shutdown_at = CASE WHEN $2 THEN shutdown_at ELSE now() END,
			reopened_at = CASE WHEN $2 THEN now() ELSE reopened_at END
		WHERE club_id = $1`, clubID, active); err != nil {
		return Academy{}, fmt.Errorf("set academy active: update: %w", err)
	}
	a.IsActive = active

	var exJSON []byte
	if !active {
		// Shutting down: immediate relief, immediate backlash.
		if _, err := tx.Exec(ctx, `
			UPDATE club.supporter_groups
			SET current_sentiment = GREATEST(0, current_sentiment - $2)
			WHERE club_id = $1`, clubID, ShutdownSentimentPenalty); err != nil {
			return Academy{}, fmt.Errorf("academy shutdown: sentiment: %w", err)
		}
		exp := explanation.New("academy_shutdown", -ShutdownSentimentPenalty).
			Add("Academy closed: supporter sentiment", -ShutdownSentimentPenalty).
			Add(fmt.Sprintf("Annual saving £%d", a.AnnualCost), 0)
		exJSON, _ = json.Marshal(exp)
	}

	payload := mustJSON(map[string]any{
		"club_id":       clubID,
		"is_active":     active,
		"annual_cost":   a.AnnualCost,
		"sentiment_hit": boolToInt(!active) * ShutdownSentimentPenalty,
	})
	eventType := EventReopened
	if !active {
		eventType = EventShutdown
	}
	if err := s.recordEvent(ctx, tx, ctxClub.WorldID, eventType, actorType, actorID, payload, exJSON); err != nil {
		return Academy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Academy{}, fmt.Errorf("set academy active: commit: %w", err)
	}
	a.ClubID, a.WorldID = clubID, ctxClub.WorldID
	return a, nil
}

// MaintenanceResult summarises one monthly maintenance sweep.
type MaintenanceResult struct {
	Academies int   `json:"academies"`
	Total     int64 `json:"total"`
}

// Maintenance debits annual_cost/12 from every active academy in the world for
// one monthly tick. Idempotent per (club, tick) via the ledger dedup key, so a
// redelivered tick posts nothing and emits no duplicate event.
func (s *Service) Maintenance(ctx context.Context, worldID uuid.UUID, tick int64) (*MaintenanceResult, error) {
	if err := s.EnsureAcademies(ctx, worldID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT club_id, COALESCE(annual_cost, 0)::bigint
		FROM club.academies
		WHERE world_id = $1 AND is_active AND annual_cost > 0
		ORDER BY club_id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("academy maintenance: list: %w", err)
	}
	defer rows.Close()

	type target struct {
		clubID uuid.UUID
		cost   int64
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.clubID, &t.cost); err != nil {
			return nil, fmt.Errorf("academy maintenance: scan: %w", err)
		}
		targets = append(targets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("academy maintenance: iterate: %w", err)
	}

	res := &MaintenanceResult{}
	for _, t := range targets {
		posted, amount, err := s.maintainClub(ctx, worldID, t.clubID, t.cost, tick)
		if err != nil {
			return res, fmt.Errorf("academy maintenance club %s: %w", t.clubID, err)
		}
		if posted {
			res.Academies++
			res.Total += amount
		}
	}
	return res, nil
}

func (s *Service) maintainClub(ctx context.Context, worldID, clubID uuid.UUID, annualCost, tick int64) (bool, int64, error) {
	monthly := annualCost / 12
	if monthly <= 0 {
		return false, 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	accountID, err := finance.EnsureAccount(ctx, tx, worldID, clubID)
	if err != nil {
		return false, 0, err
	}
	inserted, err := finance.Post(ctx, tx, accountID, "debit", "academy", monthly,
		"Academy monthly maintenance", nil, time.Now().UTC(),
		fmt.Sprintf("academy:maintenance:%s:%d", clubID, tick))
	if err != nil {
		return false, 0, err
	}
	if !inserted {
		return false, 0, nil // redelivered tick: nothing to record
	}

	payload := mustJSON(map[string]any{
		"club_id": clubID,
		"amount":  monthly,
		"tick":    tick,
	})
	if err := s.recordEvent(ctx, tx, worldID, EventMaintenance, "system", nil, payload, nil); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, 0, fmt.Errorf("commit: %w", err)
	}
	return true, monthly, nil
}

// IntakeResult summarises an intake run.
type IntakeResult struct {
	Clubs     int `json:"clubs"`
	Prospects int `json:"prospects"`
	Street    int `json:"street"`
}

// IntakeForClub generates one club's youth cohort for a season. It is
// idempotent per (club, season) via last_intake_season, skipped for a
// shuttered academy, and returns the new player ids.
func (s *Service) IntakeForClub(ctx context.Context, clubID uuid.UUID, seasonNumber int, ref time.Time) ([]uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("intake: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	ctxClub, err := loadClubContext(ctx, tx, clubID)
	if err != nil {
		return nil, err
	}
	if err := ensureAcademyTx(ctx, tx, ctxClub.WorldID, clubID); err != nil {
		return nil, err
	}
	a, err := lockAcademyTx(ctx, tx, clubID)
	if err != nil {
		return nil, fmt.Errorf("intake: lock: %w", err)
	}
	if !a.IsActive || a.LastIntakeSeason >= seasonNumber {
		return nil, nil
	}

	factory, err := s.intakeFactory(ctx, tx, hashSeed(ctxClub.WorldID, clubID, uuid.Nil, seasonNumber))
	if err != nil {
		return nil, err
	}
	natBias := ""
	if code := strings.ToLower(ctxClub.CountryCode); validNationality(ctx, tx, code) {
		natBias = code
	}

	count := ProspectCountForTier(a.InvestmentTier)
	offset := QualityOffsetForTier(a.InvestmentTier, a.Reputation)
	odds := TalentOddsForTier(a.InvestmentTier)
	rng := factory.Rng()

	ids := make([]uuid.UUID, 0, count)
	seeds := make([]finance.AcademyProspectSeed, 0, count)
	wage := finance.YouthWeeklyWage(a.InvestmentTier)
	for i := 0; i < count; i++ {
		age := YouthIntakeMinAge + rng.Intn(YouthIntakeMaxAge-YouthIntakeMinAge+1)
		gp, err := factory.CreatePlayerWithOptions(playergen.CreatePlayerOptions{
			MinAge:         age,
			MaxAge:         age,
			QualityOffset:  offset,
			Nationality:    natBias,
			Origin:         "club_academy",
			AcademyProduct: true,
			TalentOdds:     odds,
		})
		if err != nil {
			return nil, fmt.Errorf("intake: generate prospect %d: %w", i, err)
		}
		playerID, _, err := playerpool.PersistGeneratedPlayer(ctx, tx, ctxClub.WorldID, &clubID, ctxClub.CountryID, 0, gp, ref)
		if err != nil {
			return nil, err
		}
		ids = append(ids, playerID)
		seeds = append(seeds, finance.AcademyProspectSeed{PlayerID: playerID, WeeklyWage: wage})
	}

	if err := finance.SignAcademyProspects(ctx, tx, ctxClub.WorldID, clubID, ref, seeds); err != nil {
		return nil, err
	}
	if err := markClubIntake(ctx, tx, clubID, seasonNumber); err != nil {
		return nil, err
	}

	payload := mustJSON(map[string]any{
		"club_id":        clubID,
		"country_id":     ctxClub.CountryID,
		"season_number":  seasonNumber,
		"prospect_count": len(ids),
		"player_ids":     ids,
	})
	if err := s.recordEvent(ctx, tx, ctxClub.WorldID, EventIntake, "system", nil, payload, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("intake: commit: %w", err)
	}
	return ids, nil
}

// IntakeForCountry runs the seasonal country hook: the street discovery for
// the country followed by the club intake for every active academy in it.
func (s *Service) IntakeForCountry(ctx context.Context, worldID, countryID uuid.UUID, seasonNumber int, ref time.Time) (*IntakeResult, error) {
	res := &IntakeResult{}
	if err := s.EnsureAcademies(ctx, worldID); err != nil {
		return res, err
	}

	street, err := s.streetIntake(ctx, worldID, countryID, seasonNumber, ref)
	if err != nil {
		return res, err
	}
	res.Street = len(street)

	clubs, err := s.clubsForCountry(ctx, worldID, countryID)
	if err != nil {
		return res, err
	}
	for _, clubID := range clubs {
		ids, err := s.IntakeForClub(ctx, clubID, seasonNumber, ref)
		if err != nil {
			return res, fmt.Errorf("country intake club %s: %w", clubID, err)
		}
		if len(ids) > 0 {
			res.Clubs++
			res.Prospects += len(ids)
		}
	}
	return res, nil
}

// IntakeForWorld runs the club intake for every active academy in the world
// (the league-less-world seasonal fallback) plus each country's street hook.
func (s *Service) IntakeForWorld(ctx context.Context, worldID uuid.UUID, seasonNumber int, ref time.Time) (*IntakeResult, error) {
	res := &IntakeResult{}
	if err := s.EnsureAcademies(ctx, worldID); err != nil {
		return res, err
	}
	clubs, err := s.activeClubs(ctx, worldID)
	if err != nil {
		return res, err
	}
	for _, clubID := range clubs {
		ids, err := s.IntakeForClub(ctx, clubID, seasonNumber, ref)
		if err != nil {
			return res, fmt.Errorf("world intake club %s: %w", clubID, err)
		}
		if len(ids) > 0 {
			res.Clubs++
			res.Prospects += len(ids)
		}
	}
	countries, err := s.countriesForWorld(ctx, worldID)
	if err != nil {
		return res, err
	}
	for _, countryID := range countries {
		street, err := s.streetIntake(ctx, worldID, countryID, seasonNumber, ref)
		if err != nil {
			return res, err
		}
		res.Street += len(street)
	}
	return res, nil
}

// streetIntake runs the country street discovery in its own transaction with
// a deterministic per-(world, country, season) factory.
func (s *Service) streetIntake(ctx context.Context, worldID, countryID uuid.UUID, seasonNumber int, ref time.Time) ([]uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("street intake: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	factory, err := s.intakeFactory(ctx, tx, hashSeed(worldID, uuid.Nil, countryID, seasonNumber))
	if err != nil {
		return nil, err
	}
	ids, err := playerpool.StreetIntake(ctx, tx, s.bus, worldID, countryID, seasonNumber, factory, ref, playerpool.DefaultStreetIntakeConfig)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("street intake: commit: %w", err)
	}
	return ids, nil
}

// intakeFactory builds a deterministic player factory whose rng is seeded from
// the given sub-seed; the reference pools are loaded inside the caller's tx.
func (s *Service) intakeFactory(ctx context.Context, tx pgx.Tx, seed int64) (*playergen.PlayerFactory, error) {
	generator, natPool, err := bootstrap.LoadPools(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("intake: load ref pools: %w", err)
	}
	registry := playergen.NewNameRegistry()
	return playergen.NewPlayerFactory(generator, natPool, rand.New(rand.NewSource(seed))).WithRegistry(registry), nil
}

func (s *Service) activeClubs(ctx context.Context, worldID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.club_id
		FROM club.academies a
		JOIN club.clubs c ON c.id = a.club_id
		WHERE a.world_id = $1 AND a.is_active
		ORDER BY a.club_id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list active academies: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Service) clubsForCountry(ctx context.Context, worldID, countryID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.club_id
		FROM club.academies a
		JOIN club.clubs c ON c.id = a.club_id
		JOIN world.countries wc ON wc.id = $2 AND wc.name = c.country
		WHERE a.world_id = $1 AND a.is_active
		ORDER BY a.club_id`, worldID, countryID)
	if err != nil {
		return nil, fmt.Errorf("list country academies: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (s *Service) countriesForWorld(ctx context.Context, worldID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM world.countries WHERE world_id = $1 ORDER BY id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list countries: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows)
}

func scanIDs(rows pgx.Rows) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// recordEvent appends one world.events row inside the caller's transaction.
func (s *Service) recordEvent(ctx context.Context, tx pgx.Tx, worldID uuid.UUID, eventType, actorType string, actorID *uuid.UUID, payload, explanationJSON []byte) error {
	if s.bus == nil {
		return nil
	}
	e := eventbus.Event{
		WorldID:     worldID,
		EventType:   eventType,
		ActorType:   &actorType,
		ActorID:     actorID,
		Payload:     payload,
		Explanation: explanationJSON,
	}
	if err := eventbus.WriteTx(ctx, s.bus, tx, &e); err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// hashSeed folds the intake coordinates into a deterministic rng seed, so a
// given (world, club, country, season) always reproduces the same cohort.
func hashSeed(worldID, clubID, countryID uuid.UUID, season int) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("academy-intake:"))
	for _, id := range []uuid.UUID{worldID, clubID, countryID} {
		_, _ = h.Write(id[:])
	}
	_, _ = h.Write([]byte(fmt.Sprintf(":%d", season)))
	return int64(h.Sum64())
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
