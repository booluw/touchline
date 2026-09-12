//go:build integration

package competition

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// seedWorld returns a bootstrapped world with a starter club and a country.
func seedWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)

	w, err := internalworld.NewService(pool, nil).CreateWorld(context.Background(), "competition-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	if _, err := bootstrap.NewService(pool, nil).BootstrapWorld(context.Background(), w.ID, "Harbour City FC", ""); err != nil {
		t.Fatalf("bootstrap: %v", err)
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

func TestSeedCompetition(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	res, err := svc.SeedCompetition(ctx, worldID, countryID, premier.ID)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(res.Leagues) != 2 {
		t.Fatalf("seeded leagues = %d, want 2", len(res.Leagues))
	}
	for _, l := range res.Leagues {
		if l.TeamCount != 4 {
			t.Fatalf("%s team_count = %d, want 4", l.Name, l.TeamCount)
		}
		if l.FixtureCount != 12 {
			t.Fatalf("%s fixtures = %d, want 12", l.Name, l.FixtureCount)
		}
		if l.Matchdays != 6 {
			t.Fatalf("%s matchdays = %d, want 6", l.Name, l.Matchdays)
		}
	}

	// The starter club goes into the starter league.
	var starterClub uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM club.clubs WHERE name = 'Harbour City FC' AND world_id = $1`, worldID).Scan(&starterClub); err != nil {
		t.Fatalf("starter club: %v", err)
	}
	var inPremier bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM competition.seasons s
			JOIN competition.competition_entries e ON e.season_id = s.id
			WHERE s.competition_id = $1 AND e.club_id = $2)`,
		premier.ID, starterClub).Scan(&inPremier); err != nil {
		t.Fatalf("starter placement: %v", err)
	}
	if !inPremier {
		t.Fatal("starter club not placed in the starter league")
	}

	// 8 entries across the two leagues.
	var total int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.seasons s
		JOIN competition.competition_entries e ON e.season_id = s.id
		WHERE s.competition_id IN ($1,$2)`, premier.ID, champ.ID).Scan(&total); err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if total != 8 {
		t.Fatalf("total entries = %d, want 8", total)
	}

	// Events are recorded auditably: SEASON_CREATED x2 + COMPETITION_SEEDED.
	var events int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type IN ('SEASON_CREATED','COMPETITION_SEEDED')`,
		worldID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 3 {
		t.Fatalf("seed events = %d, want 3", events)
	}

	// Double-seeding a league is rejected.
	_, err = svc.SeedCompetition(ctx, worldID, countryID, premier.ID)
	if err != ErrLeagueAlreadySeeded {
		t.Fatalf("second seed err = %v, want ErrLeagueAlreadySeeded", err)
	}

	// Every club plays exactly 6 fixtures; all at 19:00 UTC.
	fixtures, err := svc.GetFixtures(ctx, premier.ID, worldID, nil)
	if err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	if len(fixtures) != 12 {
		t.Fatalf("fixtures = %d, want 12", len(fixtures))
	}
	homeCount := map[uuid.UUID]int{}
	awayCount := map[uuid.UUID]int{}
	for _, f := range fixtures {
		if f.ScheduledAt.UTC().Hour() != 19 {
			t.Fatalf("fixture %s hour = %d, want 19 UTC", f.ID, f.ScheduledAt.UTC().Hour())
		}
		homeCount[f.HomeClubID]++
		awayCount[f.AwayClubID]++
	}
	for clubID := range homeCount {
		if homeCount[clubID] != 3 || awayCount[clubID] != 3 {
			t.Fatalf("club %s plays %d home / %d away, want 3/3", clubID, homeCount[clubID], awayCount[clubID])
		}
	}
}

func TestApplyResultAndRollover(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	if _, err := svc.SeedCompetition(ctx, worldID, countryID, premier.ID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Standings start empty for the seeded season.
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
