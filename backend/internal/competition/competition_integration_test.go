//go:build integration

package competition

import (
	"context"
	"testing"
	"time"

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

// TestOffSeasonGapRolloverAndActivation verifies the IM01 off-season: a league
// whose next season rolls over anchors its calendar after the configured gap
// (world config, overridable per league), no fixtures fall inside the gap, and
// the upcoming season flips to in_progress (with SEASON_STARTED) exactly when
// its first fixture becomes due — and only then.
func TestOffSeasonGapRolloverAndActivation(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)

	// World-wide default gap of 20 ticks; the premier league overrides to 5.
	worldSvc := internalworld.NewService(pool, nil)
	if err := worldSvc.SetConfig(ctx, worldID, "season.off_season_ticks", 20); err != nil {
		t.Fatalf("set off-season config: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE competition.competition_rules
		SET scheduling_rules = '{"off_season_ticks": 5}'
		WHERE competition_id = $1`, premier.ID); err != nil {
		t.Fatalf("override premier off-season: %v", err)
	}

	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, l := range []*League{premier, champ} {
		if _, err := svc.StartSeason(ctx, worldID, l.ID); err != nil {
			t.Fatalf("start %s: %v", l.Name, err)
		}
	}

	// Play the whole season: 6 matchdays x 2 leagues x 2 fixtures.
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
				if err := svc.ApplyResult(ctx, f.ID, 1, 0); err != nil {
					t.Fatalf("apply result: %v", err)
				}
				played[f.ID] = true
			}
		}
	}
	if len(played) != 24 {
		t.Fatalf("played = %d, want 24", len(played))
	}

	var dayZero time.Time
	if err := pool.QueryRow(ctx, `
		SELECT date_trunc('day', COALESCE(launched_at, created_at))
		FROM world.worlds WHERE id = $1`, worldID).Scan(&dayZero); err != nil {
		t.Fatalf("day zero: %v", err)
	}

	// Season 2 anchored at last fixture (day 6) + gap: premier 5, champ 20.
	var premierStart, champStart time.Time
	if err := pool.QueryRow(ctx, `
		SELECT start_date FROM competition.seasons
		WHERE competition_id = $1 AND season_number = 2`, premier.ID).Scan(&premierStart); err != nil {
		t.Fatalf("premier season 2 start: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT start_date FROM competition.seasons
		WHERE competition_id = $1 AND season_number = 2`, champ.ID).Scan(&champStart); err != nil {
		t.Fatalf("champ season 2 start: %v", err)
	}
	if got := int(premierStart.Sub(dayZero).Hours() / 24); got != 11 {
		t.Fatalf("premier season 2 starts at day %d, want 11 (6 matchdays + 5 gap)", got)
	}
	if got := int(champStart.Sub(dayZero).Hours() / 24); got != 26 {
		t.Fatalf("champ season 2 starts at day %d, want 26 (6 matchdays + 20 gap)", got)
	}

	// No scheduled fixture falls inside either gap: nothing pending before its
	// next season's start_date.
	var inGap int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.fixtures f
		JOIN competition.seasons s ON s.competition_id = f.competition_id
		WHERE s.world_id = $1 AND s.season_number = 2
		  AND f.status = 'scheduled' AND f.scheduled_at::date < s.start_date`,
		worldID).Scan(&inGap); err != nil {
		t.Fatalf("count gap fixtures: %v", err)
	}
	if inGap != 0 {
		t.Fatalf("scheduled fixtures inside the off-season gap = %d, want 0", inGap)
	}

	// Activation boundary for the premier league: first fixture at day 12.
	var firstOffset int
	if err := pool.QueryRow(ctx, `
		SELECT (MIN(f.scheduled_at)::date - (date_trunc('day', COALESCE(w.launched_at, w.created_at)))::date)
		FROM match.fixtures f
		JOIN world.worlds w ON w.id = f.world_id
		WHERE f.competition_id = $1 AND f.world_id = $2 AND f.status = 'scheduled'`,
		premier.ID, worldID).Scan(&firstOffset); err != nil {
		t.Fatalf("first fixture offset: %v", err)
	}
	if firstOffset != 12 {
		t.Fatalf("premier season 2 first fixture at day %d, want 12", firstOffset)
	}

	setDay := func(day int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE world.worlds SET current_day = $1 WHERE id = $2`, day, worldID); err != nil {
			t.Fatalf("set current_day=%d: %v", day, err)
		}
	}
	statusOf := func(leagueID uuid.UUID) string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx, `
			SELECT status FROM competition.seasons
			WHERE competition_id = $1 AND season_number = 2`, leagueID).Scan(&s); err != nil {
			t.Fatalf("season 2 status: %v", err)
		}
		return s
	}

	// The day before the first fixture: nothing activates yet.
	setDay(firstOffset - 1)
	if n, err := svc.ActivateDueSeasons(ctx, worldID); err != nil {
		t.Fatalf("activate (gap): %v", err)
	} else if n != 0 {
		t.Fatalf("activated %d season(s) during the gap, want 0", n)
	}
	if statusOf(premier.ID) != "upcoming" || statusOf(champ.ID) != "upcoming" {
		t.Fatalf("statuses during gap = %s/%s, want upcoming/upcoming", statusOf(premier.ID), statusOf(champ.ID))
	}

	// Premier's first fixture day: only the premier league flips.
	setDay(firstOffset)
	if n, err := svc.ActivateDueSeasons(ctx, worldID); err != nil {
		t.Fatalf("activate premier: %v", err)
	} else if n != 1 {
		t.Fatalf("activated %d season(s), want 1", n)
	}
	if statusOf(premier.ID) != "in_progress" || statusOf(champ.ID) != "upcoming" {
		t.Fatalf("statuses after premier = %s/%s, want in_progress/upcoming", statusOf(premier.ID), statusOf(champ.ID))
	}

	// Champ's first fixture day (27) activates the champ league.
	setDay(27)
	if n, err := svc.ActivateDueSeasons(ctx, worldID); err != nil {
		t.Fatalf("activate champ: %v", err)
	} else if n != 1 {
		t.Fatalf("activated %d season(s), want 1", n)
	}
	if statusOf(champ.ID) != "in_progress" {
		t.Fatalf("champ status = %s, want in_progress", statusOf(champ.ID))
	}

	// Idempotent: a repeat pass activates nothing and emits no events.
	if n, err := svc.ActivateDueSeasons(ctx, worldID); err != nil {
		t.Fatalf("activate re-run: %v", err)
	} else if n != 0 {
		t.Fatalf("re-run activated %d season(s), want 0", n)
	}

	var started int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = 'SEASON_STARTED'`, worldID).Scan(&started); err != nil {
		t.Fatalf("count SEASON_STARTED: %v", err)
	}
	if started != 2 {
		t.Fatalf("SEASON_STARTED events = %d, want 2", started)
	}
}
