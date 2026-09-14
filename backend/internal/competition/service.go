// Package competition implements the MVP league/standings layer (S04-01).
//
// It records the OPD-01 resolution (OPD-20): competition composition is
// admin-configured per country, per world. Nothing invents a league size,
// tier count, or promotion/relegation rule — an admin declares every value:
//
//   - every world owns its countries (world.countries);
//   - every country owns its leagues (competition.competitions +
//     competition_rules, scoped by country_id);
//   - every league owns its tier, team count, and its promotion/relegation
//     adjacency (explicit promotes_to/relegates_to links, never inferred).
//
// Materialization is a two-step, deterministic process (launch model):
//
//  1. SeedWorld materializes the declaration: it generates AI clubs (with
//     squads, via bootstrap.GenerateAIClub) until every league reaches
//     team_count and records each club's season-independent membership in
//     competition.club_competitions (one league per club). Seeding is
//     incremental — re-running it only fills leagues that are not yet full
//     (e.g. leagues added after the initial seed).
//  2. StartSeason consumes membership to create the league's season, its
//     competition_entries, and a deterministic home-and-away round-robin
//     fixture list scheduled one matchday per day from the world's season
//     reference date.
//
// Determinism: SeedWorld draws everything from one world seed minted on the
// first seed run and stored in world.worlds.world_seed (recorded on the
// WORLD_SEEDED event), with per-league sub-streams derived from seed ⊕
// leagueID, so identical inputs reproduce identical worlds and later seeds
// only extend them.
package competition

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/eventbus"
)

// Sentinel errors. Handlers map these to HTTP status codes.
var (
	ErrWorldNotFound            = errors.New("world not found")
	ErrWorldArchived            = errors.New("world is archived")
	ErrCountryNotFound          = errors.New("country not found")
	ErrCountryWorldMismatch     = errors.New("country does not belong to this world")
	ErrCompetitionNotFound      = errors.New("competition not found")
	ErrCompetitionWorldMismatch = errors.New("competition does not belong to this world")
	ErrNameCollision            = errors.New("a competition with this name already exists in this country")
	ErrInvalidTeamCount         = errors.New("team_count must be an even number >= 4")
	ErrInvalidTier              = errors.New("tier must be >= 1")
	ErrInvalidCounts            = errors.New("promotions/relegations must be non-negative and fewer than team_count")
	ErrBadAdjacency             = errors.New("promotes_to/relegates_to must reference a league in the same country, and a positive movement count requires a link")
	ErrLeagueAlreadySeeded      = errors.New("this league already has a season; one season per league at a time")
	ErrCompetitionNotSeeded     = errors.New("competition has no member clubs — seed the world first")
	ErrWorldHasNoLeagues        = errors.New("world has no leagues to seed — create countries and leagues first")
	ErrAdjacencyMismatch        = errors.New("promotion/relegation adjacency is inconsistent: the league above must relegate as many as the league below promotes")
	ErrFixtureNotFound          = errors.New("fixture not found")
	ErrResultAlreadyApplied     = errors.New("result already recorded for this fixture")
	ErrNoSeason                 = errors.New("competition has no season yet")
	ErrInvalidResult            = errors.New("result scores must be non-negative")
	ErrManagerNotInMatch        = errors.New("manager does not own a club in this match")
	ErrMatchNotInProgress       = errors.New("match is not in progress")
)

var errInternalRollover = errors.New("competition: internal rollover error")

// Country is a world-scoped country that owns its leagues.
type Country struct {
	ID      uuid.UUID `json:"id"`
	WorldID uuid.UUID `json:"world_id"`
	Code    string    `json:"code"`
	Name    string    `json:"name"`
}

// LeagueParams declares a new league (admin input; nothing is invented).
type LeagueParams struct {
	CountryID   uuid.UUID  `json:"country_id"`
	Name        string     `json:"name"`
	Tier        int        `json:"tier"`
	TeamCount   int        `json:"team_count"`
	Promotions  int        `json:"promotions"`
	Relegations int        `json:"relegations"`
	PromotesTo  *uuid.UUID `json:"promotes_to,omitempty"`
	RelegatesTo *uuid.UUID `json:"relegates_to,omitempty"`
}

// League is a competition row joined with its 1:1 rules row.
type League struct {
	ID          uuid.UUID  `json:"id"`
	WorldID     uuid.UUID  `json:"world_id"`
	CountryID   uuid.UUID  `json:"country_id"`
	Name        string     `json:"name"`
	Tier        int        `json:"tier"`
	TeamCount   int        `json:"team_count"`
	Status      string     `json:"status"`
	Promotions  int        `json:"promotions"`
	Relegations int        `json:"relegations"`
	PromotesTo  *uuid.UUID `json:"promotes_to,omitempty"`
	RelegatesTo *uuid.UUID `json:"relegates_to,omitempty"`
}

// Season is one league season with its entries implied by competition_entries.
type Season struct {
	ID            uuid.UUID `json:"id"`
	CompetitionID uuid.UUID `json:"competition_id"`
	SeasonLabel   string    `json:"season_label"`
	SeasonNumber  int       `json:"season_number"`
	Status        string    `json:"status"`
}

// Fixture is one scheduled matchday fixture.
type Fixture struct {
	ID            uuid.UUID `json:"id"`
	WorldID       uuid.UUID `json:"world_id"`
	CompetitionID uuid.UUID `json:"competition_id"`
	HomeClubID    uuid.UUID `json:"home_club_id"`
	HomeClubName  string    `json:"home_club_name"`
	AwayClubID    uuid.UUID `json:"away_club_id"`
	AwayClubName  string    `json:"away_club_name"`
	Matchday      int       `json:"matchday"`
	ScheduledAt   time.Time `json:"scheduled_at"`
	Status        string    `json:"status"`
	HomeScore     *int      `json:"home_score,omitempty"`
	AwayScore     *int      `json:"away_score,omitempty"`
}

// StandingRow is one club's league-table line.
type StandingRow struct {
	ClubName     string `json:"club_name"`
	ClubShort    string `json:"club_short"`
	Played       int    `json:"played"`
	Won          int    `json:"won"`
	Drawn        int    `json:"drawn"`
	Lost         int    `json:"lost"`
	GoalsFor     int    `json:"goals_for"`
	GoalsAgainst int    `json:"goals_against"`
	Points       int    `json:"points"`
}

// StandingRowSet is a competition standings view for its active season.
type StandingRowSet struct {
	SeasonID     uuid.UUID     `json:"season_id"`
	SeasonLabel  string        `json:"season_label"`
	SeasonNumber int           `json:"season_number"`
	Status       string        `json:"status"`
	Rows         []StandingRow `json:"rows"`
}

// ClubSeed reports one club placed into a seeded league.
type ClubSeed struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// LeagueSeed reports one league's seeding result.
type LeagueSeed struct {
	LeagueID  uuid.UUID  `json:"league_id"`
	Name      string     `json:"name"`
	Tier      int        `json:"tier"`
	TeamCount int        `json:"team_count"`
	NewClubs  int        `json:"new_clubs"`
	Clubs     []ClubSeed `json:"clubs"`
}

// CountrySeed reports one country's seeding result (its leagues + members).
type CountrySeed struct {
	CountryID   uuid.UUID    `json:"country_id"`
	CountryName string       `json:"country_name"`
	Leagues     []LeagueSeed `json:"leagues"`
}

// SeedResult is the summary of a whole-world seeding run. RandomSeed is the
// world's stored replay seed (minted on the first successful run).
type SeedResult struct {
	WorldID     uuid.UUID     `json:"world_id"`
	RandomSeed  int64         `json:"random_seed"`
	Countries   []CountrySeed `json:"countries"`
	NewClubs    int           `json:"new_clubs"`
	LeagueCount int           `json:"league_count"`
}

// KickoffHourUTC is the fixed, deterministic kickoff time (19:00 UTC) for
// every fixture of a season. Product-owned pacing data, documented in the
// S04-01 delivery evidence.
const KickoffHourUTC = 19

// Service orchestrates competition administration, seeding, standings, and
// season rollover.
type Service struct {
	pool *pgxpool.Pool
	bus  Publishable
}

// Publishable mirrors the event sink used elsewhere. May be nil: the event
// log (world.events) is always written transactionally regardless of the bus.
// Only the tx-scoped outbox method is required (OPD-23).
type Publishable interface {
	eventbus.Publisher
}

// NewService builds the competition service.
func NewService(pool *pgxpool.Pool, bus Publishable) *Service {
	return &Service{pool: pool, bus: bus}
}

// ---------------------------------------------------------------------------
// Countries
// ---------------------------------------------------------------------------

// CreateCountry adds a world-scoped country. Codes are unique per world.
func (s *Service) CreateCountry(ctx context.Context, worldID uuid.UUID, code, name string) (*Country, error) {
	code = strings.TrimSpace(strings.ToLower(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, errors.New("code and name are required")
	}
	var c Country
	err := s.pool.QueryRow(ctx, `
		INSERT INTO world.countries (world_id, code, name)
		VALUES ($1, $2, $3) RETURNING id, world_id, code, name`,
		worldID, code, name,
	).Scan(&c.ID, &c.WorldID, &c.Code, &c.Name)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errors.New("a country with this code already exists in this world")
		}
		return nil, fmt.Errorf("create country: %w", err)
	}
	return &c, nil
}

// ListCountries returns a world's countries in name order.
func (s *Service) ListCountries(ctx context.Context, worldID uuid.UUID) ([]Country, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, world_id, code, name FROM world.countries
		WHERE world_id = $1 ORDER BY name`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list countries: %w", err)
	}
	defer rows.Close()
	out := []Country{}
	for rows.Next() {
		var c Country
		if err := rows.Scan(&c.ID, &c.WorldID, &c.Code, &c.Name); err != nil {
			return nil, fmt.Errorf("scan country: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Leagues
// ---------------------------------------------------------------------------

// CreateLeague declares a league under a country, with its rules row. All
// sizes and movement numbers are admin input — nothing is inferred.
func (s *Service) CreateLeague(ctx context.Context, p LeagueParams) (*League, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if p.Tier < 1 {
		return nil, ErrInvalidTier
	}
	if p.TeamCount < 4 || p.TeamCount%2 != 0 {
		return nil, ErrInvalidTeamCount
	}
	if p.Promotions < 0 || p.Relegations < 0 || p.Promotions+p.Relegations >= p.TeamCount {
		return nil, ErrInvalidCounts
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create league: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// The country must exist; the league inherits its world.
	var worldID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT world_id FROM world.countries WHERE id = $1`, p.CountryID).Scan(&worldID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load country: %w", err)
	}

	// Adjacent leagues must exist in the same country (world-scoping follows).
	for _, ref := range []struct {
		id   *uuid.UUID
		name string
	}{{p.PromotesTo, "promotes_to"}, {p.RelegatesTo, "relegates_to"}} {
		if ref.id == nil {
			continue
		}
		var refWorld uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT world_id FROM competition.competitions WHERE id = $1`, *ref.id).Scan(&refWorld)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBadAdjacency
		}
		if err != nil {
			return nil, fmt.Errorf("validate %s: %w", ref.name, err)
		}
		if refWorld != worldID {
			return nil, ErrBadAdjacency
		}
	}

	// One same-named league per country.
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.competitions
			WHERE country_id = $1 AND lower(name) = lower($2))`,
		p.CountryID, name).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check name: %w", err)
	}
	if exists {
		return nil, ErrNameCollision
	}

	var league League
	if err := tx.QueryRow(ctx, `
		INSERT INTO competition.competitions
			(world_id, country_id, name, competition_type, tier, team_count, status)
		VALUES ($1, $2, $3, 'league', $4, $5, 'active')
		RETURNING id, world_id, country_id, name, tier, team_count, status`,
		worldID, p.CountryID, name, p.Tier, p.TeamCount,
	).Scan(&league.ID, &league.WorldID, &league.CountryID, &league.Name, &league.Tier, &league.TeamCount, &league.Status); err != nil {
		return nil, fmt.Errorf("insert competition: %w", err)
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO competition.competition_rules
			(competition_id, format, is_home_and_away, promotions, relegations,
			 promotes_to_competition_id, relegates_to_competition_id)
		VALUES ($1, 'round_robin', TRUE, $2, $3, $4, $5)
		RETURNING promotions, relegations, promotes_to_competition_id, relegates_to_competition_id`,
		league.ID, p.Promotions, p.Relegations, p.PromotesTo, p.RelegatesTo,
	).Scan(&league.Promotions, &league.Relegations, &league.PromotesTo, &league.RelegatesTo); err != nil {
		return nil, fmt.Errorf("insert rules: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create league: %w", err)
	}
	return &league, nil
}

// ListLeagues returns the world's leagues ordered by country, then tier.
func (s *Service) ListLeagues(ctx context.Context, worldID uuid.UUID) ([]League, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.name, c.tier, c.team_count, c.status,
		       r.promotions, r.relegations, r.promotes_to_competition_id, r.relegates_to_competition_id
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.world_id = $1 AND c.competition_type = 'league'
		ORDER BY c.country_id, c.tier, c.name`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list leagues: %w", err)
	}
	return scanLeagues(rows)
}

// GetLeague returns one world-scoped league (404 across worlds via the
// caller's world scope).
func (s *Service) GetLeague(ctx context.Context, worldID uuid.UUID, id uuid.UUID) (*League, error) {
	league, err := s.getLeague(ctx, s.pool, id)
	if err != nil {
		return nil, err
	}
	if league.WorldID != worldID {
		return nil, ErrCompetitionNotFound
	}
	return league, nil
}

// UpdateLeagueAdjacency wires (or clears) a league's promotion/relegation
// links. Links must reference leagues in the same country; the country-wide
// symmetry is enforced when a competition is seeded.
func (s *Service) UpdateLeagueAdjacency(ctx context.Context, leagueID uuid.UUID, promotesTo, relegatesTo *uuid.UUID) error {
	league, err := s.getLeague(ctx, s.pool, leagueID)
	if err != nil {
		return err
	}
	for _, ref := range []*uuid.UUID{promotesTo, relegatesTo} {
		if ref == nil {
			continue
		}
		other, err := s.getLeague(ctx, s.pool, *ref)
		if err != nil {
			return ErrBadAdjacency
		}
		if other.CountryID != league.CountryID {
			return ErrBadAdjacency
		}
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE competition.competition_rules
		SET promotes_to_competition_id = $2, relegates_to_competition_id = $3
		WHERE competition_id = $1`, leagueID, promotesTo, relegatesTo); err != nil {
		return fmt.Errorf("update adjacency: %w", err)
	}
	return nil
}

func (s *Service) getLeague(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id uuid.UUID) (*League, error) {
	row := q.QueryRow(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.name, c.tier, c.team_count, c.status,
		       r.promotions, r.relegations, r.promotes_to_competition_id, r.relegates_to_competition_id
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.id = $1 AND c.competition_type = 'league'`, id)
	var l League
	err := row.Scan(&l.ID, &l.WorldID, &l.CountryID, &l.Name, &l.Tier, &l.TeamCount, &l.Status,
		&l.Promotions, &l.Relegations, &l.PromotesTo, &l.RelegatesTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompetitionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}
