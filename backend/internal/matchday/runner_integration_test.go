//go:build integration

package matchday

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/match"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// runnerWorld returns a two-tier world whose clubs are AI-seeded from the
// admin declaration (SeedWorld) with both leagues' seasons started, paced at a
// fast tick.match_cadence, plus the competition service. IM16: the world runs
// at a compressed fixed scale (1 game-day per 2 real seconds) and a short
// off-season, so kickoffs mature on the continuous clock without waiting real
// days; kickoffs are still gated on scheduled_at (never on the date alone).
func runnerWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, *competition.Service) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, "matchday-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	if err := worldSvc.SetConfig(ctx, w.ID, "tick.match_cadence", "10ms"); err != nil {
		t.Fatalf("set cadence: %v", err)
	}
	if err := worldSvc.SetConfig(ctx, w.ID, "tick.day_length", 2); err != nil {
		t.Fatalf("set day length: %v", err)
	}
	if err := worldSvc.SetConfig(ctx, w.ID, "season.off_season_ticks", 2); err != nil {
		t.Fatalf("set off-season: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	compSvc := competition.NewService(pool, nil)
	country, err := compSvc.CreateCountry(ctx, w.ID, "eng", "England")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	premier, err := compSvc.CreateLeague(ctx, competition.LeagueParams{CountryID: country.ID, Name: "Premier", Tier: 1, TeamCount: 4, Relegations: 1})
	if err != nil {
		t.Fatalf("create premier: %v", err)
	}
	champ, err := compSvc.CreateLeague(ctx, competition.LeagueParams{CountryID: country.ID, Name: "Championship", Tier: 2, TeamCount: 4, Promotions: 1})
	if err != nil {
		t.Fatalf("create championship: %v", err)
	}
	if err := compSvc.UpdateLeagueAdjacency(ctx, premier.ID, nil, &champ.ID); err != nil {
		t.Fatalf("link premier->champ: %v", err)
	}
	if err := compSvc.UpdateLeagueAdjacency(ctx, champ.ID, &premier.ID, nil); err != nil {
		t.Fatalf("link champ->premier: %v", err)
	}
	if _, err := compSvc.SeedWorld(ctx, w.ID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := compSvc.StartSeason(ctx, w.ID, premier.ID); err != nil {
		t.Fatalf("start premier season: %v", err)
	}
	if _, err := compSvc.StartSeason(ctx, w.ID, champ.ID); err != nil {
		t.Fatalf("start champ season: %v", err)
	}
	return pool, w.ID, compSvc
}

// scaleNow returns the world's current continuous-clock time (IM16) at the
// given instant on the compressed scale configured by runnerWorld.
func scaleNow(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) time.Time {
	t.Helper()
	ctx := context.Background()
	_, epoch, dayLength, err := internalworld.LoadScale(ctx, pool, worldID)
	if err != nil {
		t.Fatalf("load scale: %v", err)
	}
	return internalworld.ScaleNow(time.Now(), epoch, dayLength)
}

// waitForNextDue blocks until the world clock has matured the next scheduled
// matchday (or all scheduled fixtures are gone, in which case nothing else can
// ever become due). This mirrors the worker's intra-day kickoff poll but
// without the worker: the assertion target is the continuous clock maturing
// the fixture's scheduled_at, never the integer day counter.
func waitForNextDue(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var next *time.Time
		if err := pool.QueryRow(ctx,
			`SELECT MIN(scheduled_at) FROM match.fixtures WHERE world_id = $1 AND status = 'scheduled' AND matchday IS NOT NULL`, worldID).
			Scan(&next); err != nil {
			t.Fatalf("peek next scheduled fixture: %v", err)
		}
		if next == nil {
			return
		}
		if now := scaleNow(t, pool, worldID); !now.Before(*next) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the world clock to mature the next matchday")
}

func TestRunnerAdvancesMatchdaysAndRollsOver(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, compSvc)

	countCompleted := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status = 'completed'`, worldID).Scan(&n); err != nil {
			t.Fatalf("count completed: %v", err)
		}
		return n
	}

	// Before the clock has matured anything nothing is due (IM16: the kickoff
	// gate is the continuous world clock, not the integer day counter).
	sum, err := runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("initial run: %v", err)
	}
	if sum.Kicked != 0 {
		t.Fatalf("initial kicked = %d, want 0", sum.Kicked)
	}

	// Season 1: 6 matchdays x 2 leagues x 2 fixtures, each kicked off once the
	// world clock matures its scheduled_at and paced to completion in real
	// time. The clock also advances during pacing, so a pass may mature more
	// than one matchday at a time; the invariants below (whole matchdays only,
	// and every kicked fixture completed with nothing double-kicked) hold no
	// matter how the batches align.
	kicked := 0
	for pass := 0; kicked < 24; pass++ {
		waitForNextDue(t, pool, worldID)
		sum, err := runner.KickoffDue(ctx, worldID)
		if err != nil {
			t.Fatalf("pass %d run: %v", pass, err)
		}
		if sum.Kicked == 0 {
			t.Fatalf("pass %d: clock matured a matchday but nothing kicked", pass)
		}
		if sum.Kicked%4 != 0 {
			t.Fatalf("pass %d: kicked %d fixtures, want whole matchdays of 4", pass, sum.Kicked)
		}

		if pass == 0 {
			// No-overlap gate (OPD-21): a second delivery while a match is
			// still live must skip the next due matchday, never double-kick.
			// Make matchday 2 due (extra scheduled fixture dated the world's
			// current game-day), then deliver again without re-waiting.
			var extra uuid.UUID
			if err := pool.QueryRow(ctx, `
				INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
				SELECT f.world_id, f.competition_id, f.home_club_id, f.away_club_id, 2,
				       date_trunc('day', $2::timestamptz), 'scheduled'
				FROM match.fixtures f WHERE f.world_id = $1 AND f.status = 'live' LIMIT 1
				RETURNING id`,
				worldID, scaleNow(t, pool, worldID)).Scan(&extra); err != nil {
				t.Fatalf("insert extra fixture: %v", err)
			}
			again, err := runner.KickoffDue(ctx, worldID)
			if err != nil {
				t.Fatalf("pass 0 overlap run: %v", err)
			}
			if again.Matchdays != 0 || again.Kicked != 0 || again.Skipped != 1 {
				t.Fatalf("overlap matchdays=%d kicked=%d skipped=%d, want 0/0/1", again.Matchdays, again.Kicked, again.Skipped)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM match.fixtures WHERE id = $1`, extra); err != nil {
				t.Fatalf("drop extra fixture: %v", err)
			}
			var live int
			if err := pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status = 'live'`, worldID).Scan(&live); err != nil {
				t.Fatalf("count live: %v", err)
			}
			if live != 4 {
				t.Fatalf("live fixtures after pass 0 = %d, want 4", live)
			}
		}

		kicked += sum.Kicked
		if err := runner.RunLive(ctx, worldID); err != nil {
			t.Fatalf("pass %d run live: %v", pass, err)
		}
		if got := countCompleted(); got != kicked {
			t.Fatalf("pass %d: completed = %d, want %d (no double-kick)", pass, got, kicked)
		}
	}

	// Every completed fixture carries the engine's scoreline and the
	// standings write; the match row mirrors the fixture score.
	var noApply, mismatch int
	if err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE f.ht_score IS NULL OR f.at_score IS NULL),
			COUNT(*) FILTER (WHERE f.standings_applied_at IS NULL)
		FROM match.fixtures f
		WHERE f.world_id = $1 AND f.status = 'completed'`, worldID).Scan(&noApply, &mismatch); err != nil {
		t.Fatalf("scoreline check: %v", err)
	}
	if mismatch != 0 || noApply != 0 {
		t.Fatalf("missing fixture scores=%d or standings=%d", noApply, mismatch)
	}
	var scoreMismatch int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM match.fixtures f
		JOIN match.matches m ON m.fixture_id = f.id
		WHERE f.ht_score IS DISTINCT FROM m.home_score OR f.at_score IS DISTINCT FROM m.away_score`,
	).Scan(&scoreMismatch); err != nil {
		t.Fatalf("score mirror: %v", err)
	}
	if scoreMismatch != 0 {
		t.Fatalf("%d fixtures disagree with their match rows", scoreMismatch)
	}

	// Season 1 completed and rolled over into season 2 (upcoming) for both
	// leagues, with promotion/relegation movement events recorded.
	var s1Completed, s2Rows, movement int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.seasons WHERE status = 'completed' AND world_id = $1`, worldID).Scan(&s1Completed); err != nil {
		t.Fatalf("season 1 status: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.seasons WHERE season_number = 2 AND world_id = $1`, worldID).Scan(&s2Rows); err != nil {
		t.Fatalf("season 2 rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type IN ('CLUB_PROMOTED','CLUB_RELEGATED')`, worldID).Scan(&movement); err != nil {
		t.Fatalf("movement events: %v", err)
	}
	if s1Completed != 2 || s2Rows != 2 || movement != 2 {
		t.Fatalf("rollover s1completed=%d s2rows=%d movement=%d, want 2/2/2", s1Completed, s2Rows, movement)
	}

	// Redelivery with nothing matured is a no-op.
	sum, err = runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("redelivery run: %v", err)
	}
	if sum.Matchdays != 0 || sum.Kicked != 0 {
		t.Fatalf("redelivery matchdays=%d kicked=%d, want 0/0", sum.Matchdays, sum.Kicked)
	}
	if err := runner.RunLive(ctx, worldID); err != nil {
		t.Fatalf("redelivery live: %v", err)
	}

	// After the off-season gap the world clock matures season 2's first
	// matchday: it activates on the day gate and kicks exactly its 4 fixtures.
	waitForNextDue(t, pool, worldID)
	sum, err = runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("season 2 run: %v", err)
	}
	if sum.Kicked != 4 {
		t.Fatalf("season 2: kicked=%d, want 4", sum.Kicked)
	}
	if err := runner.RunLive(ctx, worldID); err != nil {
		t.Fatalf("season 2 live: %v", err)
	}
	if got := countCompleted(); got != 28 {
		t.Fatalf("completed = %d, want 28", got)
	}

	// Season 2 now has standings and is playing.
	var s2Playing int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM competition.standings st
		JOIN competition.seasons s ON s.id = st.season_id
		WHERE s.season_number = 2 AND s.world_id = $1`, worldID).Scan(&s2Playing); err != nil {
		t.Fatalf("season 2 standings: %v", err)
	}
	if s2Playing != 8 {
		t.Fatalf("season 2 standings rows = %d, want 8", s2Playing)
	}

	// MATCH_PLAYED events cover every simulated match.
	var playedEvents int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'MATCH_PLAYED'`, worldID).Scan(&playedEvents); err != nil {
		t.Fatalf("count MATCH_PLAYED: %v", err)
	}
	if playedEvents != 28 {
		t.Fatalf("MATCH_PLAYED events = %d, want 28", playedEvents)
	}
}

func TestRunnerNoFixturesIsNoop(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "matchday-empty")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, competition.NewService(pool, nil))

	sum, err := runner.KickoffDue(ctx, w.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum.Matchdays != 0 || sum.Kicked != 0 || sum.Skipped != 0 {
		t.Fatalf("noop run matchdays=%d kicked=%d skipped=%d, want 0/0/0", sum.Matchdays, sum.Kicked, sum.Skipped)
	}
}
