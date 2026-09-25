//go:build integration

package competition

import (
	"context"
	"encoding/json"
	"errors"
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

// TestFixturePacing locks the IM03 paced calendar: a 4-team league's 6
// matchdays land at 1,3,5,8,10,12 under the default 3-matchdays-per-7-day-week;
// the pacing follows the league's scheduling_rules override, and days_per_week
// falls back to the world's calendar.days_per_week config. Every matchday
// shares one game-day, and kickoff hours stay inside the league's rotation.
func TestFixturePacing(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, champ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Premier: default pacing (3 matchdays per 7 game-days, kickoffs 15/18/20).
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	wantDays := []int{1, 3, 5, 8, 10, 12}
	assertPacedDays(t, pool, premier.ID, worldID, wantDays)
	assertKickoffHours(t, pool, premier.ID, []int{15, 18, 20})
	assertSingleDayPerMatchday(t, pool, premier.ID)

	// Championship: per-league override — 4 matchdays per week at 16:00/20:00.
	if _, err := pool.Exec(ctx, `
		UPDATE competition.competition_rules
		SET scheduling_rules = '{"matchdays_per_week": 4, "kickoff_hours": [16, 20]}'
		WHERE competition_id = $1`, champ.ID); err != nil {
		t.Fatalf("override champ pacing: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, champ.ID); err != nil {
		t.Fatalf("start champ: %v", err)
	}
	assertPacedDays(t, pool, champ.ID, worldID, []int{1, 2, 4, 6, 8, 9, 11, 13})
	assertKickoffHours(t, pool, champ.ID, []int{16, 20})
	assertSingleDayPerMatchday(t, pool, champ.ID)
}

// TestFixturePacingWorldCalendarFallback verifies days_per_week reads the
// world's calendar.days_per_week config when the league sets none: with a
// 5-game-day week and 3 matchdays the season plays days 1,2,4,6,7,9.
func TestFixturePacingWorldCalendarFallback(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	worldSvc := internalworld.NewService(pool, nil)
	if err := worldSvc.SetConfig(ctx, worldID, "calendar.days_per_week", 5); err != nil {
		t.Fatalf("set calendar.days_per_week: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	assertPacedDays(t, pool, premier.ID, worldID, []int{1, 2, 4, 6, 7, 9})
}

// TestSeasonCalendar verifies the IM03 read model: the week grouping matches
// the paced fixtures, weeks carry the week's first day, and matchdays hold
// their fixtures in matchday order.
func TestSeasonCalendar(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seg, err := svc.StartSeason(ctx, worldID, premier.ID)
	if err != nil {
		t.Fatalf("start premier: %v", err)
	}

	cal, err := svc.GetSeasonCalendar(ctx, worldID, premier.ID, nil)
	if err != nil {
		t.Fatalf("calendar: %v", err)
	}
	if cal.Season.ID != seg.ID || cal.Season.Number != 1 || cal.Season.Label != seg.SeasonLabel {
		t.Fatalf("calendar season = %+v, want %+v", cal.Season, seg)
	}

	// Default 3/7 pacing: matchdays 1-3 sit in week 0 (days 1,3,5) and
	// matchdays 4-6 in week 1 (days 8,10,12).
	if len(cal.Weeks) != 2 {
		t.Fatalf("calendar weeks = %d, want 2 (got %+v)", len(cal.Weeks), cal.Weeks)
	}
	if cal.Weeks[0].Week != 0 || cal.Weeks[1].Week != 1 {
		t.Fatalf("week numbers = %d/%d, want 0/1", cal.Weeks[0].Week, cal.Weeks[1].Week)
	}
	var start time.Time
	if err := pool.QueryRow(ctx,
		`SELECT start_date FROM competition.seasons WHERE id = $1`, seg.ID).Scan(&start); err != nil {
		t.Fatalf("season start: %v", err)
	}
	if !cal.Weeks[0].FirstDay.Equal(daysTruncate(start)) ||
		!cal.Weeks[1].FirstDay.Equal(daysTruncate(start).AddDate(0, 0, 7)) {
		t.Fatalf("week first days = %v/%v, want start / start+7", cal.Weeks[0].FirstDay, cal.Weeks[1].FirstDay)
	}
	wantPerWeek := []int{3, 3}
	for i, w := range cal.Weeks {
		if len(w.Matchdays) != wantPerWeek[i] {
			t.Fatalf("week %d matchdays = %d, want %d", w.Week, len(w.Matchdays), wantPerWeek[i])
		}
		for j, md := range w.Matchdays {
			mdNum := 1 + 3*i + j
			if md.Matchday != mdNum {
				t.Fatalf("week %d matchday %d = %d, want %d", w.Week, j+1, md.Matchday, mdNum)
			}
			if len(md.Fixtures) != 2 {
				t.Fatalf("matchday %d fixtures = %d, want 2", md.Matchday, len(md.Fixtures))
			}
			wantDay := []int{1, 3, 5, 8, 10, 12}[mdNum-1]
			if got := daysBetween(daysTruncate(start), daysTruncate(md.ScheduledAt)); got != wantDay {
				t.Fatalf("matchday %d day = %d, want %d", md.Matchday, got, wantDay)
			}
		}
	}

	// The numbered-season form resolves season 1 explicitly and 404s on an
	// unknown number. It must serve the same fixtures as the active-season read.
	one := 1
	filtered, err := svc.GetSeasonCalendar(ctx, worldID, premier.ID, &one)
	if err != nil {
		t.Fatalf("calendar season=1: %v", err)
	}
	if filtered.Season.ID != seg.ID {
		t.Fatalf("season=1 calendar season = %+v, want %+v", filtered.Season, seg)
	}
	if len(filtered.Weeks) != len(cal.Weeks) {
		t.Fatalf("season=1 weeks = %d, want %d", len(filtered.Weeks), len(cal.Weeks))
	}
	for i := range filtered.Weeks {
		if len(filtered.Weeks[i].Matchdays) != len(cal.Weeks[i].Matchdays) {
			t.Fatalf("season=1 week %d matchdays = %d, want %d",
				i, len(filtered.Weeks[i].Matchdays), len(cal.Weeks[i].Matchdays))
		}
	}
	missing := 42
	if _, err := svc.GetSeasonCalendar(ctx, worldID, premier.ID, &missing); !errors.Is(err, ErrSeasonNotFound) {
		t.Fatalf("calendar season=42 err = %v, want ErrSeasonNotFound", err)
	}

	// Simulate the post-rollover state (season 2 attached after season 1's last
	// matchday, like the rollover does) and verify the window filter: the active
	// calendar serves season 2's (empty) weeks — never season-1 fixtures — while
	// season=1 stays bounded by season 2's start_date.
	start13 := daysTruncate(start).AddDate(0, 0, 13)
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.seasons (world_id, competition_id, season_label, season_number, start_date, status)
		VALUES ($1, $2, 'sim/27', 2, $3, 'upcoming')`, worldID, premier.ID, start13); err != nil {
		t.Fatalf("insert season 2: %v", err)
	}
	active2, err := svc.GetSeasonCalendar(ctx, worldID, premier.ID, nil)
	if err != nil {
		t.Fatalf("calendar with season 2: %v", err)
	}
	if active2.Season.Number != 2 {
		t.Fatalf("active season with season-2 row = %d, want 2", active2.Season.Number)
	}
	if len(active2.Weeks) != 0 {
		t.Fatalf("season-2 weeks = %d, want 0 (no season-1 leak): %+v", len(active2.Weeks), active2.Weeks)
	}
	two := 2
	season2, err := svc.GetSeasonCalendar(ctx, worldID, premier.ID, &two)
	if err != nil {
		t.Fatalf("calendar season=2: %v", err)
	}
	if season2.Season.Number != 2 || len(season2.Weeks) != 0 {
		t.Fatalf("season=2 = %s/%d weeks, want 2/0", season2.Season.Label, len(season2.Weeks))
	}
	season1, err := svc.GetSeasonCalendar(ctx, worldID, premier.ID, &one)
	if err != nil {
		t.Fatalf("calendar season=1 after season 2: %v", err)
	}
	if season1.Season.Number != 1 || len(season1.Weeks) != 2 {
		t.Fatalf("season=1 after season 2 = %d/%d weeks, want 1/2",
			season1.Season.Number, len(season1.Weeks))
	}
}

// TestListClubFixtures verifies the IM03 club read: world-scoped, earliest
// kickoff first, and cross-world clubs stay invisible.
func TestListClubFixtures(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}

	var clubID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY name LIMIT 1`, worldID).Scan(&clubID); err != nil {
		t.Fatalf("pick club: %v", err)
	}
	fx, err := svc.ListClubFixtures(ctx, worldID, clubID, 0)
	if err != nil {
		t.Fatalf("list club fixtures: %v", err)
	}
	if len(fx) != 6 {
		t.Fatalf("club fixtures = %d, want 6 (a 4-team round robin)", len(fx))
	}
	for i := 1; i < len(fx); i++ {
		if fx[i].ScheduledAt.Before(fx[i-1].ScheduledAt) {
			t.Fatalf("fixtures out of order: %v before %v", fx[i].ScheduledAt, fx[i-1].ScheduledAt)
		}
	}

	// A club from another world is invisible: reading through another world's
	// scope rejects it, and an unknown club id 404s.
	if _, err := svc.ListClubFixtures(ctx, uuid.New(), clubID, 30); err != ErrClubWorldMismatch {
		t.Fatalf("cross-world read err = %v, want ErrClubWorldMismatch", err)
	}
	if _, err := svc.ListClubFixtures(ctx, worldID, uuid.New(), 30); err != ErrClubNotFound {
		t.Fatalf("unknown club err = %v, want ErrClubNotFound", err)
	}
}

// assertPacedDays checks that the league's matchday days (relative to the
// world's launch day) equal want exactly, proving the pacing formula.
func assertPacedDays(t *testing.T, pool *pgxpool.Pool, leagueID, worldID uuid.UUID, want []int) {
	t.Helper()
	ctx := context.Background()
	var dayZero time.Time
	if err := pool.QueryRow(ctx, `
		SELECT date_trunc('day', COALESCE(launched_at, created_at))
		FROM world.worlds WHERE id = $1`, worldID).Scan(&dayZero); err != nil {
		t.Fatalf("day zero: %v", err)
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT matchday, scheduled_at::date
		FROM match.fixtures
		WHERE competition_id = $1 AND world_id = $2
		ORDER BY matchday`, leagueID, worldID)
	if err != nil {
		t.Fatalf("query paced matchdays: %v", err)
	}
	defer rows.Close()
	got := []int{}
	for rows.Next() {
		var md int
		var date time.Time
		if err := rows.Scan(&md, &date); err != nil {
			t.Fatalf("scan matchday: %v", err)
		}
		got = append(got, int(date.Sub(dayZero).Hours()/24))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate matchdays: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("matchday days = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matchday days = %v, want %v", got, want)
		}
	}
}

// assertKickoffHours checks every scheduled kickoff hour lives in allowed and
// that the rotation actually varies across the season's first matchdays.
func assertKickoffHours(t *testing.T, pool *pgxpool.Pool, leagueID uuid.UUID, allowed []int) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT EXTRACT(HOUR FROM scheduled_at)::int
		FROM match.fixtures WHERE competition_id = $1`, leagueID)
	if err != nil {
		t.Fatalf("query kickoff hours: %v", err)
	}
	defer rows.Close()
	seen := map[int]bool{}
	for rows.Next() {
		var h int
		if err := rows.Scan(&h); err != nil {
			t.Fatalf("scan kickoff hour: %v", err)
		}
		seen[h] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate kickoff hours: %v", err)
	}
	for h := range seen {
		ok := false
		for _, a := range allowed {
			if h == a {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("kickoff hour %d outside allowed %v", h, allowed)
		}
	}
	if len(allowed) > 1 && len(seen) < 2 {
		t.Fatalf("kickoff hours = %v, want rotation across %v", seen, allowed)
	}
}

// assertSingleDayPerMatchday fails if any matchday spans more than one game-day
// (every fixture of a matchday shares one scheduled day).
func assertSingleDayPerMatchday(t *testing.T, pool *pgxpool.Pool, leagueID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var multi int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT matchday FROM match.fixtures
			WHERE competition_id = $1
			GROUP BY matchday HAVING COUNT(DISTINCT scheduled_at::date) > 1) bad`, leagueID).
		Scan(&multi); err != nil {
		t.Fatalf("count multi-day matchdays: %v", err)
	}
	if multi != 0 {
		t.Fatalf("%d matchday(s) span multiple days, want 0", multi)
	}
}

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

	// Season 2 anchored at last fixture (day 12 under IM03 pacing) + gap:
	// premier 5, champ 20.
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
	if got := int(premierStart.Sub(dayZero).Hours() / 24); got != 17 {
		t.Fatalf("premier season 2 starts at day %d, want 17 (last paced fixture day 12 + 5 gap)", got)
	}
	if got := int(champStart.Sub(dayZero).Hours() / 24); got != 32 {
		t.Fatalf("champ season 2 starts at day %d, want 32 (last paced fixture day 12 + 20 gap)", got)
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

	// Activation boundary for the premier league: first fixture at day 18.
	var firstOffset int
	if err := pool.QueryRow(ctx, `
		SELECT (MIN(f.scheduled_at)::date - (date_trunc('day', COALESCE(w.launched_at, w.created_at)))::date)
		FROM match.fixtures f
		JOIN world.worlds w ON w.id = f.world_id
		WHERE f.competition_id = $1 AND f.world_id = $2 AND f.status = 'scheduled'`,
		premier.ID, worldID).Scan(&firstOffset); err != nil {
		t.Fatalf("first fixture offset: %v", err)
	}
	if firstOffset != 18 {
		t.Fatalf("premier season 2 first fixture at day %d, want 18", firstOffset)
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

	// Champ's first fixture day (33) activates the champ league.
	setDay(33)
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

// matchdayDays returns each matchday's single scheduled calendar day.
func matchdayDays(t *testing.T, pool *pgxpool.Pool, competitionID uuid.UUID) map[int]time.Time {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT matchday, MIN(scheduled_at)::date
		FROM match.fixtures WHERE competition_id = $1 GROUP BY matchday`, competitionID)
	if err != nil {
		t.Fatalf("query matchday days: %v", err)
	}
	defer rows.Close()
	out := map[int]time.Time{}
	for rows.Next() {
		var md int
		var d time.Time
		if err := rows.Scan(&md, &d); err != nil {
			t.Fatalf("scan matchday day: %v", err)
		}
		out[md] = d
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate matchday days: %v", err)
	}
	return out
}

// assertWeekdayInvariant enforces IM05 on a competition's fixtures: every
// matchday lands on an allowed ISO weekday and consecutive matchdays are at
// least two game-days apart (the structural two-day rest floor).
func assertWeekdayInvariant(t *testing.T, pool *pgxpool.Pool, competitionID uuid.UUID, allowed []int) {
	t.Helper()
	days := matchdayDays(t, pool, competitionID)
	n := len(days)
	if n < 2 {
		t.Fatalf("weekday invariant needs >=2 matchdays, got %d", n)
	}
	sorted := make([]time.Time, 0, n)
	for md := 1; md <= n; md++ {
		d, ok := days[md]
		if !ok {
			t.Fatalf("matchday %d missing from %v", md, days)
		}
		if !containsWeekday(allowed, isoWeekday(d)) {
			t.Fatalf("matchday %d on %s (ISO weekday %d), not in allowed %v", md, d.Format("2006-01-02"), isoWeekday(d), allowed)
		}
		sorted = append(sorted, daysTruncate(d))
	}
	for i := 1; i < len(sorted); i++ {
		if daysBetween(sorted[i-1], sorted[i]) < 2 {
			t.Fatalf("matchdays %d/%d only %d days apart: %s → %s",
				i, i+1, daysBetween(sorted[i-1], sorted[i]), sorted[i-1].Format("2006-01-02"), sorted[i].Format("2006-01-02"))
		}
	}
}

func containsWeekday(list []int, w int) bool {
	for _, x := range list {
		if x == w {
			return true
		}
	}
	return false
}

// TestWeekdayPacingAndRePace covers the IM05 league calendar end to end: a
// country wideweekend default (Fri/Sat/Sun) drives a freshly materialized
// season onto those days, then a mid-season league override (Mon/Wed) freezes
// the started matchday and slides every unstarted matchday forward onto the
// new weekdays, keeping the two-day rest floor and staging a 'scheduling'
// news story for the country.
func TestWeekdayPacingAndRePace(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	weekend := []int{5, 6, 7} // Fri, Sat, Sun
	res, err := svc.UpdateCountryScheduling(ctx, worldID, countryID, weekend)
	if err != nil {
		t.Fatalf("set country weekdays: %v", err)
	}
	if !res.CalendarUpdated {
		t.Fatal("country scheduling must report an update")
	}

	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	assertSingleDayPerMatchday(t, pool, premier.ID)
	assertWeekdayInvariant(t, pool, premier.ID, weekend)
	before := matchdayDays(t, pool, premier.ID)

	// Freeze matchday 1 (played) and re-pace the rest onto Mon/Wed.
	if _, err := pool.Exec(ctx, `
		UPDATE match.fixtures SET status = 'completed'
		WHERE competition_id = $1 AND matchday = 1`, premier.ID); err != nil {
		t.Fatalf("complete matchday 1: %v", err)
	}
	weekMid := []int{1, 3} // Mon, Wed
	rp, err := svc.UpdateLeagueScheduling(ctx, premier.ID, weekMid)
	if err != nil {
		t.Fatalf("re-pace premier: %v", err)
	}
	if rp.MatchdaysRePaced == 0 || rp.FixturesMoved == 0 {
		t.Fatalf("re-pacing moved nothing: %+v", rp)
	}
	after := matchdayDays(t, pool, premier.ID)

	if !before[1].Equal(after[1]) {
		t.Fatalf("frozen matchday 1 moved: %v → %v", before[1], after[1])
	}
	for md := 2; md <= len(before); md++ {
		if !containsWeekday(weekMid, isoWeekday(after[md])) {
			t.Fatalf("re-paced matchday %d on %s (weekday %d), want Mon/Wed",
				md, after[md].Format("2006-01-02"), isoWeekday(after[md]))
		}
	}
	if daysBetween(after[1], after[2]) < 2 {
		t.Fatalf("first unstarted matchday too close to frozen one: %v → %v", after[1], after[2])
	}
	assertWeekdayInvariant(t, pool, premier.ID, weekMid)

	// The re-pace staged a country-scoped scheduling story.
	var stories int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.news_stories
		WHERE category = 'scheduling' AND country_id = $1`, countryID).Scan(&stories); err != nil {
		t.Fatalf("count scheduling news: %v", err)
	}
	if stories == 0 {
		t.Fatal("expected a country-scoped 'scheduling' news story after re-pacing")
	}
}

// TestCupCalendarAnchoredToLeagueEnd covers IM05 cup anchoring: with a
// country weekend default, a cup campaign's rounds land only on allowed
// weekdays, keep two-day gaps, avoid league days, and pull the final a few
// days after the league season's last fixture.
func TestCupCalendarAnchoredToLeagueEnd(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	weekend := []int{5, 6, 7}
	if _, err := svc.UpdateCountryScheduling(ctx, worldID, countryID, weekend); err != nil {
		t.Fatalf("set country weekdays: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}

	cup, err := svc.CreateCup(ctx, CupParams{
		WorldID:           worldID,
		CountryID:         countryID,
		Name:              "FA Cup",
		FirstTierBye:      0,
		SurvivorThreshold: 2,
	})
	if err != nil {
		t.Fatalf("create cup: %v", err)
	}
	if _, err := svc.StartCupCampaign(ctx, worldID, countryID, cup.ID); err != nil {
		t.Fatalf("start campaign: %v", err)
	}

	leagueDays := map[time.Time]bool{}
	for _, l := range []uuid.UUID{premier.ID} {
		for _, d := range matchdayDays(t, pool, l) {
			leagueDays[daysTruncate(d)] = true
		}
	}
	leagueEnd := time.Time{}
	for d := range leagueDays {
		if d.After(leagueEnd) {
			leagueEnd = d
		}
	}

	cupDays := matchdayDays(t, pool, cup.ID)
	if len(cupDays) == 0 {
		t.Fatal("cup campaign produced no fixtures")
	}
	var oldest time.Time
	for md, d := range cupDays {
		dd := daysTruncate(d)
		if !containsWeekday(weekend, isoWeekday(dd)) {
			t.Fatalf("cup round %d on %s (weekday %d), not allowed %v", md, dd.Format("2006-01-02"), isoWeekday(dd), weekend)
		}
		if leagueDays[dd] {
			t.Fatalf("cup round %d collides with a league day %s", md, dd.Format("2006-01-02"))
		}
		if oldest.IsZero() || dd.After(oldest) {
			oldest = dd
		}
	}
	if !oldest.After(leagueEnd.AddDate(0, 0, 2)) {
		t.Fatalf("cup final %v must clear the league end %v by a few days", oldest, leagueEnd)
	}

	sorted := make([]time.Time, 0, len(cupDays))
	for md := 1; md <= len(cupDays); md++ {
		sorted = append(sorted, daysTruncate(cupDays[md]))
	}
	for i := 1; i < len(sorted); i++ {
		if daysBetween(sorted[i], sorted[i-1]) < 2 {
			t.Fatalf("cup rounds %d/%d only %d days apart",
				i, i+1, daysBetween(sorted[i], sorted[i-1]))
		}
	}
}

// TestMyClubCompetitions verifies the manager club-overview read: the league
// dossier carries the full base, started flag and league table; the cup
// dossier carries the campaign season, stage, round and next fixture; both
// report "not started" before a season/campaign exists.
func TestMyClubCompetitions(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var clubID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT club_id FROM competition.club_competitions
		WHERE competition_id = $1 AND role = 'league'
		ORDER BY club_id LIMIT 1`, premier.ID).Scan(&clubID); err != nil {
		t.Fatalf("pick premier member: %v", err)
	}

	// Pre-season: exactly the league dossier, not started, no standings.
	items, err := svc.MyClubCompetitions(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("overview pre-season: %v", err)
	}
	if len(items) != 1 || items[0].League == nil || items[0].Cup != nil {
		t.Fatalf("pre-season items = %+v, want exactly one league dossier", items)
	}
	lg := items[0].League
	if lg.Competition.ID != premier.ID || lg.Competition.Tier != 1 || lg.Competition.TeamCount != 4 {
		t.Fatalf("league base = %+v, want premier tier 1 with 4 teams", lg.Competition)
	}
	if lg.Started || lg.Season != nil || lg.Standings != nil || lg.NextFixture != nil {
		t.Fatalf("pre-season dossier = started %v season %v standings %v next %v, want all empty",
			lg.Started, lg.Season, lg.Standings, lg.NextFixture)
	}

	// A club with no memberships yields an empty list.
	loner, err := svc.MyClubCompetitions(ctx, worldID, uuid.New())
	if err != nil {
		t.Fatalf("overview no memberships: %v", err)
	}
	if len(loner) != 0 {
		t.Fatalf("no-membership overview = %+v, want empty", loner)
	}

	// In season: started flips, standings rows appear once a result is in.
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start season: %v", err)
	}
	items, err = svc.MyClubCompetitions(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("overview in-season: %v", err)
	}
	lg = items[0].League
	if !lg.Started || lg.Season == nil || lg.Season.Number != 1 {
		t.Fatalf("in-season started=%v season=%+v, want started + season 1", lg.Started, lg.Season)
	}
	if lg.Standings == nil || len(lg.Standings.Rows) != 0 {
		t.Fatalf("standings before results = %+v, want empty rows", lg.Standings)
	}
	if lg.NextFixture == nil {
		t.Fatal("in-season league next fixture must be scheduled")
	}

	var clubTie uuid.UUID
	fx1, err := svc.GetFixtures(ctx, premier.ID, worldID, newInt(1))
	if err != nil {
		t.Fatalf("matchday 1 fixtures: %v", err)
	}
	for _, f := range fx1 {
		if f.HomeClub.ID == clubID || f.AwayClub.ID == clubID {
			clubTie = f.ID
		}
	}
	if clubTie == uuid.Nil {
		t.Fatal("club has no matchday-1 fixture")
	}
	if err := svc.ApplyResult(ctx, clubTie, 2, 1); err != nil {
		t.Fatalf("apply result: %v", err)
	}
	items, err = svc.MyClubCompetitions(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("overview after result: %v", err)
	}
	lg = items[0].League
	if lg.Standings == nil || len(lg.Standings.Rows) != 1 {
		t.Fatalf("standings after one result = %+v, want one row", lg.Standings)
	}
	found := false
	for _, r := range lg.Standings.Rows {
		if r.Club.ID == clubID {
			found = true
			if r.Points != 3 {
				t.Fatalf("club points = %d, want 3 (win)", r.Points)
			}
		}
	}
	if !found {
		t.Fatalf("standings rows %+v do not include the club", lg.Standings.Rows)
	}

	// Cup campaign: joining the cup adds a second dossier.
	cup, err := svc.CreateCup(ctx, CupParams{
		WorldID:           worldID,
		CountryID:         countryID,
		Name:              "FA Cup",
		FirstTierBye:      0,
		SurvivorThreshold: 2,
	})
	if err != nil {
		t.Fatalf("create cup: %v", err)
	}
	if _, err := svc.StartCupCampaign(ctx, worldID, countryID, cup.ID); err != nil {
		t.Fatalf("start campaign: %v", err)
	}
	items, err = svc.MyClubCompetitions(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("overview with cup: %v", err)
	}
	var cupItem *ClubCupView
	for i := range items {
		if items[i].Cup != nil {
			cupItem = items[i].Cup
		}
	}
	if cupItem == nil {
		t.Fatalf("items %+v have no cup dossier", items)
	}
	if !cupItem.Started || cupItem.Season == nil {
		t.Fatalf("cup started=%v season=%v, want started with campaign", cupItem.Started, cupItem.Season)
	}
	if cupItem.Stage != "playing" || cupItem.CurrentRound != 1 {
		t.Fatalf("cup stage=%s round=%d, want playing/1 right after campaign", cupItem.Stage, cupItem.CurrentRound)
	}
	if cupItem.TotalRounds == 0 {
		t.Fatal("cup total_rounds must be > 0")
	}
	if cupItem.NextFixture == nil || cupItem.NextFixture.Competition.Name != cup.Name {
		t.Fatalf("cup next fixture = %+v, want a FA Cup tie", cupItem.NextFixture)
	}

	// Club loses its cup tie: stage flips to eliminated, no next fixture.
	cupFx, err := svc.GetFixtures(ctx, cup.ID, worldID, nil)
	if err != nil {
		t.Fatalf("cup fixtures: %v", err)
	}
	var tieID uuid.UUID
	var clubIsHome bool
	for _, f := range cupFx {
		switch {
		case f.HomeClub.ID == clubID:
			tieID, clubIsHome = f.ID, true
		case f.AwayClub.ID == clubID:
			tieID, clubIsHome = f.ID, false
		}
	}
	if tieID == uuid.Nil {
		t.Fatal("club has no round-1 cup tie")
	}
	if err := svc.ApplyResult(ctx, tieID, boolInt(!clubIsHome), boolInt(clubIsHome)); err != nil {
		t.Fatalf("apply losing cup result: %v", err)
	}
	items, err = svc.MyClubCompetitions(ctx, worldID, clubID)
	if err != nil {
		t.Fatalf("overview after cup loss: %v", err)
	}
	for i := range items {
		if items[i].Cup != nil {
			cupItem = items[i].Cup
		}
	}
	if cupItem.Stage != "eliminated" || cupItem.NextFixture != nil {
		t.Fatalf("after elimination stage=%s next=%v, want eliminated with no fixture",
			cupItem.Stage, cupItem.NextFixture)
	}
}

// boolInt maps a flag to a 1/0 score (aim one goal at the winning side).
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// cupPlanLadder reads the stored campaign ladder for a live cup.
func cupPlanLadder(t *testing.T, pool *pgxpool.Pool, cupID uuid.UUID) []roundPlan {
	t.Helper()
	var raw json.RawMessage
	if err := pool.QueryRow(context.Background(),
		`SELECT qualification_rules FROM competition.competition_rules WHERE competition_id = $1`, cupID).Scan(&raw); err != nil {
		t.Fatalf("load cup rules: %v", err)
	}
	plan, ok := planFromQual(raw)
	if !ok {
		t.Fatalf("cup %s has no stored campaign plan", cupID)
	}
	return plan.Ladder
}

// playOutCupRounds plays every cup round up to (but not including) the final,
// so the final's fixtures materialize. Cup ties go 2-1 to the home side
// (golden goal must decide draws, so scores never tie).
func playOutCupRounds(t *testing.T, svc *Service, worldID, cupID uuid.UUID, total int) {
	t.Helper()
	ctx := context.Background()
	for r := 1; r < total; r++ {
		fxs, err := svc.GetFixtures(ctx, cupID, worldID, newInt(r))
		if err != nil {
			t.Fatalf("cup round %d fixtures: %v", r, err)
		}
		for _, f := range fxs {
			if err := svc.ApplyResult(ctx, f.ID, 2, 1); err != nil {
				t.Fatalf("apply cup result round %d: %v", r, err)
			}
		}
	}
}

func TestCupFinalDateDefaultsAndValidation(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	cup, err := svc.CreateCup(ctx, CupParams{
		WorldID: worldID, CountryID: countryID, Name: "FA Cup", FirstTierBye: 0, SurvivorThreshold: 2,
	})
	if err != nil {
		t.Fatalf("create cup: %v", err)
	}
	if cup.FinalDateMode != cupFinalModeCalculated {
		t.Fatalf("default final_date_mode = %q, want calculated", cup.FinalDateMode)
	}
	if cup.FinalDate != nil {
		t.Fatalf("default final_date = %v, want nil", cup.FinalDate)
	}
	if cup.FinalOffsetDays == nil || *cup.FinalOffsetDays != cupDefaultFinalOffsetDays {
		t.Fatalf("default offset = %v, want %d", cup.FinalOffsetDays, cupDefaultFinalOffsetDays)
	}
	listed, err := svc.ListCups(ctx, worldID)
	if err != nil {
		t.Fatalf("list cups: %v", err)
	}
	roundTrip := false
	for _, c := range listed {
		if c.ID == cup.ID {
			roundTrip = c.FinalDateMode == cupFinalModeCalculated && c.FinalDate == nil
		}
	}
	if !roundTrip {
		t.Fatalf("ListCups did not round-trip the final-date policy: %+v", listed)
	}

	// Fixed without a date is rejected at creation.
	if _, err := svc.CreateCup(ctx, CupParams{
		WorldID: worldID, CountryID: countryID, Name: "Broken Cup", FirstTierBye: 0, SurvivorThreshold: 2,
		FinalDatePolicy: FinalDatePolicy{FinalDateMode: cupFinalModeFixed},
	}); !errors.Is(err, ErrCupFinalDateInvalid) {
		t.Fatalf("fixed without date err = %v, want ErrCupFinalDateInvalid", err)
	}
	// Calculated ignores a supplied date at creation; fixed stores its pin.
	date := "2031-09-14"
	derived, err := svc.CreateCup(ctx, CupParams{
		WorldID: worldID, CountryID: countryID, Name: "Derived Cup", FirstTierBye: 0, SurvivorThreshold: 2,
		FinalDatePolicy: FinalDatePolicy{FinalDateMode: cupFinalModeCalculated, FinalDate: &date},
	})
	if err != nil {
		t.Fatalf("create calculated with supplied date: %v", err)
	}
	if derived.FinalDate != nil {
		t.Fatalf("calculated cup stored supplied date %v, want nil", derived.FinalDate)
	}
	pinned, err := svc.CreateCup(ctx, CupParams{
		WorldID: worldID, CountryID: countryID, Name: "Pinned Cup", FirstTierBye: 0, SurvivorThreshold: 2,
		FinalDatePolicy: FinalDatePolicy{FinalDateMode: cupFinalModeFixed, FinalDate: &date},
	})
	if err != nil {
		t.Fatalf("create fixed cup: %v", err)
	}
	if pinned.FinalDate == nil || *pinned.FinalDate != date {
		t.Fatalf("fixed final_date = %v, want %s", pinned.FinalDate, date)
	}
}

// TestCupFinalDateFixedPins covers IM10 fixed mode: the campaign's final lands
// exactly on the declared date regardless of the league calendar, earlier
// rounds still anchor backward, offsets are rejected on a fixed cup, and once
// the final round has materialized any edit is refused.
func TestCupFinalDateFixedPins(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	weekend := []int{5, 6, 7}
	if _, err := svc.UpdateCountryScheduling(ctx, worldID, countryID, weekend); err != nil {
		t.Fatalf("set country weekdays: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}

	pinned := "2032-05-08"
	cup, err := svc.CreateCup(ctx, CupParams{
		WorldID: worldID, CountryID: countryID, Name: "Pinned Cup", FirstTierBye: 0, SurvivorThreshold: 2,
		FinalDatePolicy: FinalDatePolicy{FinalDateMode: cupFinalModeFixed, FinalDate: &pinned},
	})
	if err != nil {
		t.Fatalf("create cup: %v", err)
	}
	if _, err := svc.StartCupCampaign(ctx, worldID, countryID, cup.ID); err != nil {
		t.Fatalf("start campaign: %v", err)
	}

	ladder := cupPlanLadder(t, pool, cup.ID)
	finalRound := ladder[len(ladder)-1]
	if finalRound.Date == nil || finalRound.Date.Format("2006-01-02") != pinned {
		t.Fatalf("plan final = %v, want pinned %s", finalRound.Date, pinned)
	}
	if ladder[0].Date == nil {
		t.Fatal("fixed cup left round 1 unanchored")
	}

	// Fixed cups reject offset edits and null/absent dates.
	off := 5
	if _, err := svc.SetCupFinalDate(ctx, cup.ID, nil, false, &off); !errors.Is(err, ErrCupFinalDateInvalid) {
		t.Fatalf("fixed offset edit err = %v, want ErrCupFinalDateInvalid", err)
	}
	if _, err := svc.SetCupFinalDate(ctx, cup.ID, nil, true, nil); !errors.Is(err, ErrCupFinalDateInvalid) {
		t.Fatalf("fixed null-date edit err = %v, want ErrCupFinalDateInvalid", err)
	}

	// Play the whole bracket out: the final materializes on the pinned date.
	cam, err := svc.GetCup(ctx, worldID, cup.ID)
	if err != nil {
		t.Fatalf("get cup: %v", err)
	}
	playOutCupRounds(t, svc, worldID, cup.ID, cam.TotalRounds)
	fxs, err := svc.GetFixtures(ctx, cup.ID, worldID, newInt(cam.TotalRounds))
	if err != nil {
		t.Fatalf("final fixtures: %v", err)
	}
	if len(fxs) != 1 {
		t.Fatalf("final fixtures = %d, want 1", len(fxs))
	}
	if got := daysTruncate(fxs[0].ScheduledAt).Format("2006-01-02"); got != pinned {
		t.Fatalf("final fixture date = %s, want pinned %s", got, pinned)
	}
	// The final is now scheduled: any final-date edit is locked.
	newPin := "2032-05-22"
	if _, err := svc.SetCupFinalDate(ctx, cup.ID, &newPin, true, nil); !errors.Is(err, ErrCupFinalDateLocked) {
		t.Fatalf("post-final edit err = %v, want ErrCupFinalDateLocked", err)
	}
}

// TestSetCupFinalDateOverrideAndClear covers IM10 calculated-mode edits: a
// per-campaign override moves the unstamped tail (round-1 fixtures never
// move), an edit landing behind the played history is locked, and clearing the
// override re-derives the original anchored final. Every moved final publishes
// a scheduling story.
func TestSetCupFinalDateOverrideAndClear(t *testing.T) {
	pool, worldID, countryID := seedWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	premier, _ := twoTierLeague(t, svc, countryID)
	if _, err := svc.SeedWorld(ctx, worldID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	weekend := []int{5, 6, 7}
	if _, err := svc.UpdateCountryScheduling(ctx, worldID, countryID, weekend); err != nil {
		t.Fatalf("set country weekdays: %v", err)
	}
	if _, err := svc.StartSeason(ctx, worldID, premier.ID); err != nil {
		t.Fatalf("start premier: %v", err)
	}
	cup, err := svc.CreateCup(ctx, CupParams{
		WorldID: worldID, CountryID: countryID, Name: "FA Cup", FirstTierBye: 0, SurvivorThreshold: 2,
	})
	if err != nil {
		t.Fatalf("create cup: %v", err)
	}
	if _, err := svc.StartCupCampaign(ctx, worldID, countryID, cup.ID); err != nil {
		t.Fatalf("start campaign: %v", err)
	}

	firstFinal := cupPlanLadder(t, pool, cup.ID)
	lastRound := firstFinal[len(firstFinal)-1]
	if lastRound.Date == nil {
		t.Fatal("calculated campaign left the final unanchored")
	}
	derivedFinal := lastRound.Date.Format("2006-01-02")
	round1Before := matchdayDays(t, pool, cup.ID)[1]
	if round1Before.IsZero() {
		t.Fatal("round 1 has no fixture")
	}

	var beforeStories int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.news_stories
		WHERE category = 'scheduling' AND headline LIKE '%final date set%'`).Scan(&beforeStories); err != nil {
		t.Fatalf("count final-date news: %v", err)
	}

	// Override the final a week later than the derived anchor.
	derivedPlus7, err := time.Parse("2006-01-02", derivedFinal)
	if err != nil {
		t.Fatalf("parse derived final: %v", err)
	}
	override := derivedPlus7.AddDate(0, 0, 7).Format("2006-01-02")
	if _, err := svc.SetCupFinalDate(ctx, cup.ID, &override, true, nil); err != nil {
		t.Fatalf("override: %v", err)
	}
	if round1After := matchdayDays(t, pool, cup.ID)[1]; !round1After.Equal(round1Before) {
		t.Fatalf("round 1 fixture moved %v -> %v; history must stand", round1Before, round1After)
	}
	if got := cupPlanLadder(t, pool, cup.ID)[len(firstFinal)-1].Date.Format("2006-01-02"); got != override {
		t.Fatalf("stored final = %s, want override %s", got, override)
	}

	// An override landing before the played rounds is locked.
	early := "2000-01-10"
	if _, err := svc.SetCupFinalDate(ctx, cup.ID, &early, true, nil); !errors.Is(err, ErrCupFinalDateLocked) {
		t.Fatalf("early-override err = %v, want ErrCupFinalDateLocked", err)
	}

	// Clearing restores the derived anchor without touching the declaration.
	if _, err := svc.SetCupFinalDate(ctx, cup.ID, nil, true, nil); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if got := cupPlanLadder(t, pool, cup.ID)[len(firstFinal)-1].Date.Format("2006-01-02"); got != derivedFinal {
		t.Fatalf("cleared final = %s, want derived %s", got, derivedFinal)
	}
	after, err := svc.GetCup(ctx, worldID, cup.ID)
	if err != nil {
		t.Fatalf("get cup: %v", err)
	}
	if after.Cup.FinalDateMode != cupFinalModeCalculated || after.Cup.FinalDate != nil {
		t.Fatalf("declaration after edits = mode %q date %v, want calculated/nil", after.Cup.FinalDateMode, after.Cup.FinalDate)
	}

	var afterStories int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.news_stories
		WHERE category = 'scheduling' AND headline LIKE '%final date set%'`).Scan(&afterStories); err != nil {
		t.Fatalf("count final-date news: %v", err)
	}
	if afterStories <= beforeStories {
		t.Fatalf("expected a scheduling story per moved final (was %d, now %d)", beforeStories, afterStories)
	}
}
