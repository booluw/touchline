package competition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// SeedCompetition materializes the admin's league declaration for a country:
// for every league in the country it guarantees team_count entries (reusing
// clubs that are not yet entered, starting with the world's starter club in
// starterLeagueID), generates the remaining AI clubs with squads, creates a
// season, and schedules the deterministic double round-robin fixture list.
//
// One seed per league is allowed: any league that already has a season is a
// 409 ErrLeagueAlreadySeeded. The whole run is one transaction and is
// deterministic — a single seeded rand.Rand derived from the world's
// WORLD_BOOTSTRAPPED.random_seed drives names, squads, and fixture order.
func (s *Service) SeedCompetition(ctx context.Context, worldID, countryID, starterLeagueID uuid.UUID) (*SeedResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin seed tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM world.worlds WHERE id = $1 FOR UPDATE`, worldID).Scan(&worldStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return nil, ErrWorldArchived
	}

	var country Country
	err = tx.QueryRow(ctx,
		`SELECT id, world_id, code, name FROM world.countries WHERE id = $1`, countryID).
		Scan(&country.ID, &country.WorldID, &country.Code, &country.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCountryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load country: %w", err)
	}
	if country.WorldID != worldID {
		return nil, ErrCountryWorldMismatch
	}

	leagues, err := s.leaguesByCountry(ctx, tx, countryID)
	if err != nil {
		return nil, err
	}
	if len(leagues) == 0 {
		return nil, ErrCountryHasNoLeagues
	}
	if err := validateAdjacency(leagues); err != nil {
		return nil, err
	}

	starterOK := false
	for _, l := range leagues {
		if l.ID == starterLeagueID {
			starterOK = true
		}
		seeded, err := leagueHasSeason(ctx, tx, l.ID)
		if err != nil {
			return nil, err
		}
		if seeded {
			return nil, ErrLeagueAlreadySeeded
		}
	}
	if !starterOK {
		return nil, ErrCompetitionNotFound
	}

	// Determinism: one master rng seeded from the world bootstrap seed.
	worldSeed, bootRef, err := bootstrapSeed(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}
	master := rand.New(rand.NewSource(worldSeed))

	freeClubs, err := clubsWithoutEntries(ctx, tx, worldID)
	if err != nil {
		return nil, err
	}

	result := &SeedResult{WorldID: worldID, CountryID: countryID}
	for _, l := range leagues {
		entries := []uuid.UUID{}
		if l.ID == starterLeagueID {
			take := min(len(freeClubs), l.TeamCount)
			entries = append(entries, freeClubs[:take]...)
			freeClubs = freeClubs[take:]
		}

		clubSeeds := []ClubSeed{}
		names := map[string]bool{}
		var anyName string
		for _, clubID := range entries {
			if err := tx.QueryRow(ctx, `SELECT name FROM club.clubs WHERE id = $1`, clubID).Scan(&anyName); err != nil {
				return nil, fmt.Errorf("load club name: %w", err)
			}
			names[anyName] = true
			clubSeeds = append(clubSeeds, ClubSeed{ID: clubID, Name: anyName})
		}
		for len(entries) < l.TeamCount {
			subSeed := master.Int63()
			factory, err := squadFactory(ctx, tx, subSeed)
			if err != nil {
				return nil, err
			}
			clubName := nextClubName(master, names)
			generated, err := bootstrap.GenerateAIClub(ctx, tx, worldID, clubName, short(clubName), country.Name, factory)
			if err != nil {
				return nil, fmt.Errorf("generate AI club for %s: %w", l.Name, err)
			}
			entries = append(entries, generated.ClubID)
			names[clubName] = true
			clubSeeds = append(clubSeeds, ClubSeed{ID: generated.ClubID, Name: generated.ClubName})
		}

		season, err := s.createSeason(ctx, tx, worldID, l.ID, bootRef, entries)
		if err != nil {
			return nil, err
		}

		fixtureCount, matchdays, err := s.createFixtures(ctx, tx, worldID, l.ID, entries, bootRef)
		if err != nil {
			return nil, err
		}

		seedEv := &eventbus.Event{
			WorldID:    worldID,
			EventType:  "SEASON_CREATED",
			RandomSeed: &worldSeed,
			Payload: mustJSON(map[string]any{
				"competition_id": l.ID,
				"season_id":      season.ID,
				"season_label":   season.SeasonLabel,
				"season_number":  season.SeasonNumber,
				"team_count":     len(entries),
				"fixture_count":  fixtureCount,
				"matchdays":      matchdays,
			}),
		}
		if err := recordSeedEvent(ctx, tx, seedEv); err != nil {
			return nil, err
		}

		result.Leagues = append(result.Leagues, LeagueSeed{
			LeagueID:     l.ID,
			Name:         l.Name,
			Tier:         l.Tier,
			TeamCount:    len(entries),
			SeasonID:     season.ID,
			SeasonLabel:  season.SeasonLabel,
			FixtureCount: fixtureCount,
			Matchdays:    matchdays,
			Clubs:        clubSeeds,
		})
	}

	if len(freeClubs) > 0 {
		return nil, ErrNoStarterClub
	}

	countryEv := &eventbus.Event{
		WorldID:   worldID,
		EventType: "COMPETITION_SEEDED",
		Payload: mustJSON(map[string]any{
			"country_id": countryID,
			"leagues":    leagueSummaries(result),
		}),
	}
	if err := recordSeedEvent(ctx, tx, countryEv); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit seed: %w", err)
	}
	return result, nil
}

// createSeason inserts the next season for a league with its entries.
func (s *Service) createSeason(ctx context.Context, tx pgx.Tx, worldID, leagueID uuid.UUID, bootRef time.Time, entries []uuid.UUID) (*Season, error) {
	var number int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(season_number), 0) + 1 FROM competition.seasons WHERE competition_id = $1`, leagueID).
		Scan(&number); err != nil {
		return nil, fmt.Errorf("next season number: %w", err)
	}

	label := seasonLabel(bootRef, number)
	status := "in_progress"
	if number > 1 {
		status = "upcoming"
	}
	var season Season
	if err := tx.QueryRow(ctx, `
		INSERT INTO competition.seasons
			(world_id, competition_id, season_label, season_number, start_date, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, competition_id, season_label, season_number, status`,
		worldID, leagueID, label, number, bootRef, status,
	).Scan(&season.ID, &season.CompetitionID, &season.SeasonLabel, &season.SeasonNumber, &season.Status); err != nil {
		return nil, fmt.Errorf("insert season: %w", err)
	}

	for _, clubID := range entries {
		if _, err := tx.Exec(ctx, `
			INSERT INTO competition.competition_entries (season_id, club_id)
			VALUES ($1, $2) ON CONFLICT (season_id, club_id) DO NOTHING`,
			season.ID, clubID); err != nil {
			return nil, fmt.Errorf("insert entry %s: %w", clubID, err)
		}
	}
	return &season, nil
}

// createFixtures schedules the deterministic double round-robin for the given
// entries (already in canonical order) and returns (fixture count, matchdays).
func (s *Service) createFixtures(ctx context.Context, tx pgx.Tx, worldID, leagueID uuid.UUID, entries []uuid.UUID, bootRef time.Time) (int, int, error) {
	rounds := roundRobin(len(entries))
	count := 0
	for r, round := range rounds { // matchday is 1-based
		day := daysTruncate(bootRef).AddDate(0, 0, r+1)
		kickoff := time.Date(day.Year(), day.Month(), day.Day(), KickoffHourUTC, 0, 0, 0, time.UTC)
		for _, p := range round {
			if _, err := tx.Exec(ctx, `
				INSERT INTO match.fixtures
					(world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
				VALUES ($1, $2, $3, $4, $5, $6, 'scheduled')`,
				worldID, leagueID, entries[p[0]], entries[p[1]], r+1, kickoff); err != nil {
				return 0, 0, fmt.Errorf("insert fixture matchday %d: %w", r+1, err)
			}
			count++
		}
	}
	return count, len(rounds), nil
}

// bootstrapSeed returns the world's replay seed and its season reference date.
// The seed comes from the WORLD_BOOTSTRAPPED event; without one the world has
// no starter material to build on.
func bootstrapSeed(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) (int64, time.Time, error) {
	var (
		seedPtr *int64
		ref     time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT
			(SELECT e.random_seed FROM world.events e
			 WHERE e.world_id = w.id AND e.event_type = 'WORLD_BOOTSTRAPPED'
			 ORDER BY e.occurred_at LIMIT 1),
			COALESCE(w.launched_at, w.created_at)
		FROM world.worlds w WHERE w.id = $1`, worldID).Scan(&seedPtr, &ref)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("load bootstrap seed: %w", err)
	}
	if seedPtr == nil {
		return 0, time.Time{}, ErrWorldNotBootstrapped
	}
	return *seedPtr, ref, nil
}

// leaguesByCountry loads the country's leagues ordered by tier (ascending).
func (s *Service) leaguesByCountry(ctx context.Context, tx pgx.Tx, countryID uuid.UUID) ([]League, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.name, c.tier, c.team_count, c.status,
		       r.promotions, r.relegations, r.promotes_to_competition_id, r.relegates_to_competition_id
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		WHERE c.country_id = $1 AND c.competition_type = 'league'
		ORDER BY c.tier, c.name`, countryID)
	if err != nil {
		return nil, fmt.Errorf("leagues by country: %w", err)
	}
	return scanLeagues(rows)
}

// leagueHasSeason reports whether a league already has any season (seeded).
func leagueHasSeason(ctx context.Context, tx pgx.Tx, leagueID uuid.UUID) (bool, error) {
	var has bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM competition.seasons WHERE competition_id = $1)`, leagueID).Scan(&has)
	return has, err
}

// clubsWithoutEntries returns clubs in the world that are not yet entered in
// any league season (ordered by creation).
func clubsWithoutEntries(ctx context.Context, tx pgx.Tx, worldID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id FROM club.clubs c
		WHERE c.world_id = $1
		  AND NOT EXISTS (
			SELECT 1 FROM competition.competition_entries e
			JOIN competition.seasons s ON s.id = e.season_id
			WHERE e.club_id = c.id AND s.world_id = c.world_id)
		ORDER BY c.created_at`, worldID)
	if err != nil {
		return nil, fmt.Errorf("clubs without entries: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan club: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// squadFactory builds a deterministic player factory from a sub-seed, drawing
// name/nationality pools from the reference data inside the caller's tx.
func squadFactory(ctx context.Context, tx pgx.Tx, seed int64) (*playergen.PlayerFactory, error) {
	generator, natPool, err := bootstrap.LoadPools(ctx, tx)
	if err != nil {
		return nil, err
	}
	registry := playergen.NewNameRegistry()
	return playergen.NewPlayerFactory(generator, natPool, rand.New(rand.NewSource(seed))).WithRegistry(registry), nil
}

func leagueSummaries(r *SeedResult) []map[string]any {
	out := make([]map[string]any, 0, len(r.Leagues))
	for _, l := range r.Leagues {
		out = append(out, map[string]any{
			"league_id": l.LeagueID,
			"name":      l.Name,
			"season_id": l.SeasonID,
		})
	}
	return out
}

func mustJSON(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("competition: marshal payload: %v", err))
	}
	return b
}
