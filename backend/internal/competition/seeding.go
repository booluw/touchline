package competition

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/pkg/eventbus"
	"github.com/touchline/backend/pkg/playergen"
)

// SeedWorld materializes the admin's world declaration (launch model): for
// every country, for every league, it guarantees team_count member clubs with
// generated squads and records each club's season-independent membership in
// competition.club_competitions.
//
// It is strictly incremental and idempotent: leagues that already hold
// team_count members are reported as full and skipped, so running it again
// after an admin adds more leagues (or countries) fills only the new ones
// without touching anything already materialized. It creates NO seasons and NO
// fixtures — running a season is the separate StartSeason step.
//
// Determinism: the run draws from one world seed minted on the FIRST
// successful seed of a world and stored in world.worlds.world_seed (recorded
// on the WORLD_SEEDED event; later runs reuse it). Per-league club naming
// draws from rand.New(rand.NewSource(seed ⊕ leagueID)) so a league added and
// seeded later is reproducible regardless of the overall run history. The
// whole run is one transaction.
func (s *Service) SeedWorld(ctx context.Context, worldID uuid.UUID) (*SeedResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin seed tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		worldStatus string
		worldSeed   *int64
		worldRef    time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT status, world_seed, COALESCE(launched_at, created_at)
		FROM world.worlds WHERE id = $1 FOR UPDATE`, worldID,
	).Scan(&worldStatus, &worldSeed, &worldRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return nil, ErrWorldArchived
	}

	// Mint the world's replay seed on first seed; reuse it on every later run
	// so incremental seeding stays reproducible. A failed first run rolls back
	// with the enclosing tx (the WORLD_SEEDED event and the world_seed write
	// are committed atomically with the seeded content, or not at all).
	if worldSeed == nil {
		v := cryptoSeed()
		if _, err := tx.Exec(ctx,
			`UPDATE world.worlds SET world_seed = $2 WHERE id = $1`, worldID, v); err != nil {
			return nil, fmt.Errorf("store world seed: %w", err)
		}
		seedValue := v
		if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
			WorldID:    worldID,
			EventType:  "WORLD_SEEDED",
			RandomSeed: &seedValue,
			Payload: mustJSON(map[string]any{
				"seed": v,
			}),
		}); err != nil {
			return nil, err
		}
		worldSeed = &v
	}
	seed := *worldSeed
	ref := daysTruncate(worldRef)
	log.Printf("seed world=%s: starting (world_seed=%d)", worldID, seed)

	// Club-name pools come from the reference data (data-driven, OPD-13
	// analogue): admins extend ref.club_name_parts via the dashboard or JSON.
	// Each world country may have a regional pool (keyed by its code); the ""
	// pool is the global fallback.
	pools, err := bootstrap.LoadClubNamePools(ctx, tx)
	if err != nil {
		return nil, err
	}

	// Club names must be unique within the world; seed against the existing set.
	used := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT name FROM club.clubs WHERE world_id = $1`, worldID)
	if err != nil {
		return nil, fmt.Errorf("load existing club names: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan club name: %w", err)
		}
		used[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate club names: %w", err)
	}

	// One master factory seeds each country's free-agent pool; the pool is
	// replenished after each club so the market never empties through a big
	// multi-league seed. ReplenishPool is idempotent — it only tops up.
	poolFactory, err := squadFactory(ctx, tx, hashMix(seed, worldID))
	if err != nil {
		return nil, err
	}

	result := &SeedResult{WorldID: worldID, RandomSeed: seed}
	countries, err := tx.Query(ctx, `
		SELECT id, code, name FROM world.countries WHERE world_id = $1 ORDER BY id`, worldID)
	if err != nil {
		return nil, fmt.Errorf("list countries: %w", err)
	}
	type countryRow struct {
		id   uuid.UUID
		code string
		name string
	}
	var countryRows []countryRow
	for countries.Next() {
		var c countryRow
		if err := countries.Scan(&c.id, &c.code, &c.name); err != nil {
			countries.Close()
			return nil, fmt.Errorf("scan country: %w", err)
		}
		countryRows = append(countryRows, c)
	}
	countries.Close()
	if err := countries.Err(); err != nil {
		return nil, fmt.Errorf("iterate countries: %w", err)
	}

	for _, country := range countryRows {
		leagues, err := s.leaguesByCountry(ctx, tx, country.id)
		if err != nil {
			return nil, err
		}
		if len(leagues) == 0 {
			continue // country declared but not yet structured — a later seed fills it
		}

		// Mint the country's free-agent pool before the first club drafts from
		// it — DraftSquad fails with ErrPoolTooSmall otherwise. ReplenishPool is
		// idempotent, so re-seeds just top the pool back up.
		if err := playerpool.ReplenishPool(ctx, tx, s.bus, worldID, &country.id, playerpool.PoolTargetSize, poolFactory, ref); err != nil {
			return nil, fmt.Errorf("seed country pool: %w", err)
		}
		log.Printf("seed world=%s country=%s: %d league(s); free-agent pool at %d", worldID, country.name, len(leagues), playerpool.PoolTargetSize)

		seeded := make([]LeagueSeed, 0, len(leagues))
		for _, l := range leagues {
			result.LeagueCount++
			members, err := leagueMembers(ctx, tx, l.ID)
			if err != nil {
				return nil, err
			}

			need := l.TeamCount - len(members)
			if need <= 0 {
				log.Printf("seed world=%s country=%s league=%s (tier %d): already full (%d/%d), skipping", worldID, country.name, l.Name, l.Tier, len(members), l.TeamCount)
				seeded = append(seeded, LeagueSeed{
					LeagueID:  l.ID,
					Name:      l.Name,
					Tier:      l.Tier,
					TeamCount: l.TeamCount,
					NewClubs:  0,
					Clubs:     clubSeeds(ctx, tx, members), //nolint:errcheck
				})
				continue
			}

			// Per-league name stream: deterministic in the league, independent
			// of how many other leagues the world has already seeded. The
			// country's regional pool wins when present; the global pool is
			// the fallback.
			pool := pools[country.code]
			if len(pool.Stems) == 0 || len(pool.Suffixes) == 0 {
				pool = pools[""]
			}
			lrng := rand.New(rand.NewSource(hashMix(seed, l.ID)))
			created := make([]uuid.UUID, 0, need)
			names := map[string]bool{}
			log.Printf("seed world=%s country=%s league=%s (tier %d): %d/%d teams present, creating %d club(s)", worldID, country.name, l.Name, l.Tier, len(members), l.TeamCount, need)
			for i := 0; i < need; i++ {
				name := nextClubName(lrng, pool.Stems, pool.Suffixes, used)
				generated, err := bootstrap.GenerateAIClub(ctx, s.bus, tx, worldID, name, short(name), country.name, &country.id)
				if err != nil {
					return nil, fmt.Errorf("generate AI club for %s: %w", l.Name, err)
				}
				log.Printf("seed world=%s country=%s league=%s: creating AI club %q (%d/%d)", worldID, country.name, l.Name, generated.ClubName, i+1, need)
				created = append(created, generated.ClubID)
				used[name] = true
				names[generated.ClubName] = true

				if _, err := tx.Exec(ctx, `
					INSERT INTO competition.club_competitions (world_id, club_id, competition_id, role)
					VALUES ($1, $2, $3, 'league')`,
					worldID, generated.ClubID, l.ID); err != nil {
					return nil, fmt.Errorf("record league membership for %s: %w", generated.ClubName, err)
				}

				// The draft consumed 24 players; top the country pool back up
				// so later clubs (and the signing market) still have supply.
				if err := playerpool.ReplenishPool(ctx, tx, s.bus, worldID, &country.id, playerpool.PoolTargetSize, poolFactory, ref); err != nil {
					return nil, fmt.Errorf("replenish country pool: %w", err)
				}
			}

			members = append(members, created...)
			seeded = append(seeded, LeagueSeed{
				LeagueID:  l.ID,
				Name:      l.Name,
				Tier:      l.Tier,
				TeamCount: l.TeamCount,
				NewClubs:  need,
				Clubs:     clubSeeds(ctx, tx, members), //nolint:errcheck
			})
			result.NewClubs += need
		}

		countryClubs := 0
		for _, ls := range seeded {
			countryClubs += ls.NewClubs
		}
		poolCount, _ := playerpool.PoolCount(ctx, tx, worldID, &country.id)
		log.Printf("seed world=%s country=%s: done — %d club(s) seeded, pool=%d free agents", worldID, country.name, countryClubs, poolCount)

		result.Countries = append(result.Countries, CountrySeed{
			CountryID:   country.id,
			CountryName: country.name,
			Leagues:     seeded,
		})
	}

	if result.LeagueCount == 0 {
		return nil, ErrWorldHasNoLeagues
	}

	if err := s.recordSeedEvent(ctx, tx, &eventbus.Event{
		WorldID:   worldID,
		EventType: "COMPETITION_SEEDED",
		Payload: mustJSON(map[string]any{
			"world_id":      worldID,
			"country_count": len(result.Countries),
			"league_count":  result.LeagueCount,
			"new_clubs":     result.NewClubs,
		}),
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit seed: %w", err)
	}
	log.Printf("seed world=%s: committed — %d new club(s) across %d league(s)", worldID, result.NewClubs, result.LeagueCount)
	return result, nil
}

// ValidateSeedWorld checks the pre-conditions for an async world seed without
// doing the heavy materialization. It mirrors the synchronous SeedWorld guards
// (world exists, is not archived, has at least one declared league) so the HTTP
// layer can 4xx a bad request immediately instead of after a queued job fails.
func (s *Service) ValidateSeedWorld(ctx context.Context, worldID uuid.UUID) error {
	var worldStatus string
	err := s.pool.QueryRow(ctx, `SELECT status FROM world.worlds WHERE id = $1`, worldID).Scan(&worldStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWorldNotFound
	}
	if err != nil {
		return fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return ErrWorldArchived
	}
	var n int
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM competition.competitions c
		JOIN world.countries wc ON wc.id = c.country_id
		WHERE wc.world_id = $1`, worldID).Scan(&n)
	if err != nil {
		return fmt.Errorf("count leagues: %w", err)
	}
	if n == 0 {
		return ErrWorldHasNoLeagues
	}
	return nil
}

// StartSeason materializes the next season for a competition from its current
// league memberships: the season row, its competition_entries, and the
// deterministic double round-robin fixture list begun at the world's season
// reference date. It is the launcher for a playable year — separate from
// SeedWorld, which only guarantees clubs + members.
func (s *Service) StartSeason(ctx context.Context, worldID, competitionID uuid.UUID) (*Season, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin start-season tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var worldStatus string
	var worldRef time.Time
	err = tx.QueryRow(ctx,
		`SELECT status, COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1 FOR UPDATE`, worldID).
		Scan(&worldStatus, &worldRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load world: %w", err)
	}
	if worldStatus == "archived" {
		return nil, ErrWorldArchived
	}

	var compWorld uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT world_id FROM competition.competitions WHERE id = $1`, competitionID).Scan(&compWorld)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCompetitionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load competition: %w", err)
	}
	if compWorld != worldID {
		return nil, ErrCompetitionWorldMismatch
	}

	seeded, err := leagueHasSeason(ctx, tx, competitionID)
	if err != nil {
		return nil, err
	}
	if seeded {
		return nil, ErrLeagueAlreadySeeded
	}

	members, err := leagueMembers(ctx, tx, competitionID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, ErrCompetitionNotSeeded
	}

	season, err := s.createSeason(ctx, tx, worldID, competitionID, worldRef, members)
	if err != nil {
		return nil, err
	}

	fixtureCount, matchdays, err := s.createFixtures(ctx, tx, worldID, competitionID, members, worldRef)
	if err != nil {
		return nil, err
	}

	var seedPtr *int64
	if err := tx.QueryRow(ctx, `SELECT world_seed FROM world.worlds WHERE id = $1`, worldID).Scan(&seedPtr); err != nil {
		return nil, fmt.Errorf("load world seed: %w", err)
	}
	seedEvent := &eventbus.Event{
		WorldID:    worldID,
		EventType:  "SEASON_CREATED",
		RandomSeed: seedPtr,
		Payload: mustJSON(map[string]any{
			"competition_id": competitionID,
			"season_id":      season.ID,
			"season_label":   season.SeasonLabel,
			"season_number":  season.SeasonNumber,
			"team_count":     len(members),
			"fixture_count":  fixtureCount,
			"matchdays":      matchdays,
		}),
	}
	if err := s.recordSeedEvent(ctx, tx, seedEvent); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit start-season: %w", err)
	}
	return season, nil
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
	).Scan(&season.ID, &season.Competition.ID, &season.SeasonLabel, &season.SeasonNumber, &season.Status); err != nil {
		return nil, fmt.Errorf("insert season: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT name FROM competition.competitions WHERE id = $1`, leagueID).
		Scan(&season.Competition.Name); err != nil {
		return nil, fmt.Errorf("load competition name: %w", err)
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
// IM03 pacing: matchdays spread across the game-week per the league's
// scheduling parameters (3 in a default 7-game-day week), each at a kickoff
// hour from the league's rotation derived from the world seed — all
// deterministic, so identical inputs reproduce identical calendars.
func (s *Service) createFixtures(ctx context.Context, tx pgx.Tx, worldID, leagueID uuid.UUID, entries []uuid.UUID, bootRef time.Time) (int, int, error) {
	rounds := roundRobin(len(entries))

	p, err := s.scheduleParams(ctx, tx, leagueID, worldID)
	if err != nil {
		return 0, 0, err
	}
	var seed int64
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(world_seed, 0) FROM world.worlds WHERE id = $1`, worldID).Scan(&seed); err != nil {
		return 0, 0, fmt.Errorf("load world seed: %w", err)
	}

	count := 0
	for r, round := range rounds { // matchday is 1-based
		kickoff := scheduledAtFromDay(bootRef, r+1, p.daysPerWeek, p.matchdaysPerWeek,
			kickoffHour(seed, leagueID, p.kickoffHours, r+1))
		for _, pair := range round {
			if _, err := tx.Exec(ctx, `
				INSERT INTO match.fixtures
					(world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
				VALUES ($1, $2, $3, $4, $5, $6, 'scheduled')`,
				worldID, leagueID, entries[pair[0]], entries[pair[1]], r+1, kickoff); err != nil {
				return 0, 0, fmt.Errorf("insert fixture matchday %d: %w", r+1, err)
			}
			count++
		}
	}
	return count, len(rounds), nil
}

// leagueMembers returns a competition's current league-role member club ids,
// in deterministic joined order (stable replay).
func leagueMembers(ctx context.Context, tx pgx.Tx, competitionID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT club_id FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'league'
		ORDER BY joined_at, club_id`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("league members: %w", err)
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan league member: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// clubSeeds resolves club ids to their names for a seed response.
func clubSeeds(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) []ClubSeed {
	if len(ids) == 0 {
		return []ClubSeed{}
	}
	names := map[uuid.UUID]string{}
	rows, err := tx.Query(ctx,
		`SELECT id, name FROM club.clubs WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return []ClubSeed{} // response enrichment is best-effort
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		names[id] = name
	}
	out := make([]ClubSeed, 0, len(ids))
	for _, id := range ids {
		out = append(out, ClubSeed{ID: id, Name: names[id]})
	}
	return out
}

// leaguesByCountry loads the country's leagues ordered by tier (ascending).
func (s *Service) leaguesByCountry(ctx context.Context, tx pgx.Tx, countryID uuid.UUID) ([]League, error) {
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.world_id, c.country_id, c.name, c.tier, c.team_count, c.status,
		       r.promotions, r.relegations,
		       r.promotes_to_competition_id, r.relegates_to_competition_id,
		       COALESCE(ptc.name, ''), COALESCE(rtc.name, ''),
		       wc.name, wc.code
		FROM competition.competitions c
		JOIN competition.competition_rules r ON r.competition_id = c.id
		LEFT JOIN competition.competitions ptc ON ptc.id = r.promotes_to_competition_id
		LEFT JOIN competition.competitions rtc ON rtc.id = r.relegates_to_competition_id
		JOIN world.countries wc ON wc.id = c.country_id
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

// hashMix folds a UUID into a seed so per-league / per-world sub-streams are
// deterministic and wire-independent of run ordering.
func hashMix(seed int64, id uuid.UUID) int64 {
	h := fnv.New64a()
	_, _ = h.Write(id[:])
	return seed ^ int64(h.Sum64())
}

// cryptoSeed returns a fresh crypto-random int64 replay seed.
func cryptoSeed() int64 {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func mustJSON(v map[string]any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("competition: marshal payload: %v", err))
	}
	return b
}
