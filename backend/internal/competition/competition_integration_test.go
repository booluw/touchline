//go:build integration

package competition

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// seedWorld returns a world with a country and nothing else materialized — no
// clubs, no leagues. The launch model: the admin declares structure, and only
// an explicit SeedWorld call produces clubs + players.
func seedWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	w, err := internalworld.NewService(pool, nil).CreateWorld(context.Background(), "competition-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	country, err := NewService(pool, nil).CreateCountry(context.Background(), w.ID, "eng", "England")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	return pool, w.ID, country.ID
}

// twoTierLeague returns a 4-team top league and a 4-team second tier linked by
// 1 up / 1 down — the minimal symmetric country structure.
func twoTierLeague(t *testing.T, svc *Service, countryID uuid.UUID) (*League, *League) {
	t.Helper()
	ctx := context.Background()

	premier, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "Premier", Tier: 1, TeamCount: 4, Relegations: 1})
	if err != nil {
		t.Fatalf("create premier: %v", err)
	}
	champ, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "Championship", Tier: 2, TeamCount: 4, Promotions: 1})
	if err != nil {
		t.Fatalf("create championship: %v", err)
	}
	if err := svc.UpdateLeagueAdjacency(ctx, premier.ID, nil, &champ.ID); err != nil {
		t.Fatalf("link premier->champ: %v", err)
	}
	if err := svc.UpdateLeagueAdjacency(ctx, champ.ID, &premier.ID, nil); err != nil {
		t.Fatalf("link champ->premier: %v", err)
	}
	return premier, champ
}

func TestSeedWorld(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	res, err := svc.SeedWorld(ctx, worldID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if res.RandomSeed == 0 {
		t.Fatal("seed result must report the world's replay seed")
	}
	if res.NewClubs != 8 {
		t.Fatalf("new clubs = %d, want 8", res.NewClubs)
	}
	if len(res.Countries) != 1 || len(res.Countries[0].Leagues) != 2 {
		t.Fatalf("countries = %+v, want 2 leagues under 1 country", res.Countries)
	}
	for _, l := range res.Countries[0].Leagues {
		if l.TeamCount != 4 || l.NewClubs != 4 {
			t.Fatalf("%s team_count=%d new_clubs=%d, want 4/4", l.Name, l.TeamCount, l.NewClubs)
		}
	}

	// Materialization is clubs + players + memberships ONLY: no seasons, no
	// fixtures, no starter-club special cases — every club is AI-controlled.
	var totalClubs int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM club.clubs WHERE world_id = $1`, worldID).Scan(&totalClubs); err != nil {
		t.Fatalf("count clubs: %v", err)
	}
	if totalClubs != 8 {
		t.Fatalf("clubs = %d, want 8", totalClubs)
	}
	var nonAI int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM club.clubs WHERE world_id = $1 AND is_ai_controlled = FALSE`, worldID).Scan(&nonAI); err != nil {
		t.Fatalf("count non-AI: %v", err)
	}
	if nonAI != 0 {
		t.Fatalf("non-AI clubs = %d, want 0 (all-AI seeding)", nonAI)
	}

	// Memberships: every club in exactly one league; each league at team_count.
	var members int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.club_competitions
		WHERE world_id = $1 AND role = 'league'`, worldID).Scan(&members); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if members != 8 {
		t.Fatalf("league memberships = %d, want 8", members)
	}
	for _, l := range []*League{premier, champ} {
		var n int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM competition.club_competitions
			WHERE competition_id = $1 AND role = 'league'`, l.ID).Scan(&n); err != nil {
			t.Fatalf("count members of %s: %v", l.Name, err)
		}
		if n != 4 {
			t.Fatalf("%s members = %d, want 4", l.Name, n)
		}
	}

	// No seasons, no fixtures yet — running a season is a separate step.
	var seasons, fixtures int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM competition.seasons WHERE world_id = $1`, worldID).Scan(&seasons); err != nil {
		t.Fatalf("count seasons: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1`, worldID).Scan(&fixtures); err != nil {
		t.Fatalf("count fixtures: %v", err)
	}
	if seasons != 0 || fixtures != 0 {
		t.Fatalf("seed must not create seasons/fixtures: seasons=%d fixtures=%d", seasons, fixtures)
	}

	// Re-seeding the same world is an idempotent no-op.
	res2, err := svc.SeedWorld(ctx, worldID)
	if err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if res2.NewClubs != 0 || res2.RandomSeed != res.RandomSeed {
		t.Fatalf("re-seed = %+v, want 0 new clubs and the same seed", res2)
	}

	// Adding a league later: the next seed fills ONLY it.
	third, err := svc.CreateLeague(ctx, LeagueParams{CountryID: countryID, Name: "League One", Tier: 3, TeamCount: 4})
	if err != nil {
		t.Fatalf("create third league: %v", err)
	}
	res3, err := svc.SeedWorld(ctx, worldID)
	if err != nil {
		t.Fatalf("incremental seed: %v", err)
	}
	if res3.NewClubs != 4 {
		t.Fatalf("incremental new clubs = %d, want 4", res3.NewClubs)
	}
	var thirdMembers int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'league'`, third.ID).Scan(&thirdMembers); err != nil {
		t.Fatalf("count members of third: %v", err)
	}
	if thirdMembers != 4 {
		t.Fatalf("third league members = %d, want 4", thirdMembers)
	}

	// Every generated club has a drafted squad (players), not just a row.
	var squadless int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM club.clubs c
		WHERE c.world_id = $1
		  AND NOT EXISTS (SELECT 1 FROM player.players p WHERE p.club_id = c.id)`,
		worldID).Scan(&squadless); err != nil {
		t.Fatalf("count squadless clubs: %v", err)
	}
	if squadless != 0 {
		t.Fatalf("squadless clubs = %d, want 0", squadless)
	}
}

func TestStartSeasonAndApplyResultAndRollover(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A season can only start once members exist.
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ: %v", err)
	}

	// Standings start empty for the started season.
	standing, err := svc.GetStandings(ctx, premier.ID, worldID)
	if err != nil {
		t.Fatalf("standings: %v", err)
	}
	if len(standing.Rows) != 0 {
		t.Fatalf("initial standings rows = %d, want 0", len(standing.Rows))
	}

	// Play every fixture: 6 matchdays x 2 leagues x 2 fixtures.
	// Apply a deterministic result so the table is fully reasoned.
	played := map[uuid.UUID]bool{}
	for round := 0; round < 6; round++ {
		for _, leagueID := range []uuid.UUID{premier.ID, champ.ID} {
			fixtures, err := svc.GetFixtures(ctx, leagueID, worldID, newInt(round+1))
			if err != nil {
				t.Fatalf("matchday %d fixtures: %v", round+1, err)
			}
			for _, f := range fixtures {
				if played[f.ID] {
					continue
				}
				if err := svc.ApplyResult(ctx, f.ID, 2, 1); err != nil {
					t.Fatalf("apply result: %v", err)
				}
				played[f.ID] = true
			}
		}
	}
	if len(played) != 24 {
		t.Fatalf("played = %d, want 24", len(played))
	}

	// Duplicate application is rejected.
	for _, leagueID := range []uuid.UUID{premier.ID, champ.ID} {
		fixtures, err := svc.GetFixtures(ctx, leagueID, worldID, newInt(1))
		if err != nil {
			t.Fatalf("re-query: %v", err)
		}
		err = svc.ApplyResult(ctx, fixtures[0].ID, 1, 0)
		if err != ErrResultAlreadyApplied {
			t.Fatalf("re-apply err = %v, want ErrResultAlreadyApplied", err)
		}
	}

	// Season 2 exists for both leagues.
	var season2 int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.seasons
		WHERE competition_id IN ($1,$2) AND season_number = 2`,
		premier.ID, champ.ID).Scan(&season2); err != nil {
		t.Fatalf("count season 2: %v", err)
	}
	if season2 != 2 {
		t.Fatalf("season 2 rows = %d, want 2", season2)
	}

	// Both seasons have 8 total entries, and the rosters changed across the
	// rollover (promotions/relegations happened).
	var season1, season2Count int
	for _, leagueID := range []uuid.UUID{premier.ID, champ.ID} {
		for num := 1; num <= 2; num++ {
			var n int
			if err := pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM competition.seasons s
				JOIN competition.competition_entries e ON e.season_id = s.id
				WHERE s.competition_id = $1 AND s.season_number = $2`,
				leagueID, num).Scan(&n); err != nil {
				t.Fatalf("entry count: %v", err)
			}
			if num == 1 {
				season1 += n
			} else {
				season2Count += n
			}
		}
	}
	if season1 != 8 || season2Count != 8 {
		t.Fatalf("entries season1=%d season2=%d, want 8/8", season1, season2Count)
	}

	// Movement events: exactly one promoted, one relegated.
	var movement int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type IN ('CLUB_PROMOTED','CLUB_RELEGATED')`,
		worldID).Scan(&movement); err != nil {
		t.Fatalf("movement events: %v", err)
	}
	if movement != 2 {
		t.Fatalf("movement events = %d, want 2 (1 promoted, 1 relegated)", movement)
	}
}

func newInt(v int) *int { return &v }
