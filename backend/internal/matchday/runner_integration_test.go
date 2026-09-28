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
	// time. IM22 staggering gives each fixture its own kickoff slot (the two
	// ties of a league round mature a couple of game-hours apart), so passes
	// carry whatever slice is due — the invariants below (every kicked fixture
	// completed, nothing double-kicked) hold no matter how the batches align.
	kicked := 0
	for pass := 0; kicked < 24; pass++ {
		waitForNextDue(t, pool, worldID)
		sum, err := runner.KickoffDue(ctx, worldID)
		if err != nil {
			t.Fatalf("pass %d run: %v", pass, err)
		}
		if sum.Kicked == 0 {
			t.Fatalf("pass %d: clock matured a fixture but nothing kicked", pass)
		}

		if pass == 0 {
			// Round-order gate (IM22): a fixture of a later matchday must not
			// kick while an earlier round of the same competition is still
			// live. Make matchday 2 due (extra scheduled fixture dated the
			// world's current game-day) and re-deliver without re-waiting.
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
				t.Fatalf("overlap matchdays=%d kicked=%d skipped=%d, want 0/0/1 (round-order gate)", again.Matchdays, again.Kicked, again.Skipped)
			}
			if _, err := pool.Exec(ctx, `DELETE FROM match.fixtures WHERE id = $1`, extra); err != nil {
				t.Fatalf("drop extra fixture: %v", err)
			}
			var live int
			if err := pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status = 'live'`, worldID).Scan(&live); err != nil {
				t.Fatalf("count live: %v", err)
			}
			// Staggered rounds mature per fixture: the pass kicked each
			// competition's first lane (2 leagues x their first tie), with the
			// second lane maturing a few game-hours later.
			if live != 2 && live != 4 {
				t.Fatalf("live fixtures after pass 0 = %d, want the first staggered lane (2) or both lanes (4)", live)
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
	// matchday: it activates on the day gate and kicks its due fixtures. With
	// IM22 staggering each league's first lane (its 12:00 tie) matures ahead of
	// the second (15:00), so the pass carries one or both lanes.
	waitForNextDue(t, pool, worldID)
	sum, err = runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("season 2 run: %v", err)
	}
	if sum.Kicked != 2 && sum.Kicked != 4 {
		t.Fatalf("season 2: kicked=%d, want the first staggered lane (2) or both lanes (4)", sum.Kicked)
	}
	s2kicked := sum.Kicked
	if err := runner.RunLive(ctx, worldID); err != nil {
		t.Fatalf("season 2 live: %v", err)
	}

	// The second lane of season 2's first matchday matures a few game-hours
	// later; kick it and pace it out too so the season is actually playing.
	waitForNextDue(t, pool, worldID)
	sum, err = runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("season 2 lane-2 run: %v", err)
	}
	s2kicked += sum.Kicked
	if err := runner.RunLive(ctx, worldID); err != nil {
		t.Fatalf("season 2 lane-2 live: %v", err)
	}
	if s2kicked != 4 {
		t.Fatalf("season 2 matchday 1 kicked = %d, want all 4 fixtures", s2kicked)
	}
	if got := countCompleted(); got != 24+s2kicked {
		t.Fatalf("completed = %d, want %d (season 1 + season 2 matchday 1)", got, 24+s2kicked)
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
		t.Fatalf("MATCH_PLAYED events = %d, want %d", playedEvents, 24+s2kicked)
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

// TestRunnerResumesStalledLiveMatch is the regression test for "matches never
// end". A pacing loop that dies mid-match (transient error, worker restart) used
// to strand the fixture: the worker's poll only started RunLive right after a
// kickoff, and the no-overlap gate then blocked every later matchday, so the
// match sat at its last persisted minute forever. RunLive is resumable by
// design — every step is durable per simulated minute — and the poll now enters
// it for every world on every pass, so a fresh call must carry a stalled match
// the rest of the way to a completed fixture.
func TestRunnerResumesStalledLiveMatch(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, compSvc)

	waitForNextDue(t, pool, worldID)
	if _, err := runner.KickoffDue(ctx, worldID); err != nil {
		t.Fatalf("kickoff: %v", err)
	}

	// Stall the match the way a dead loop would: pace a handful of minutes and
	// then walk away, with the fixture still 'live'.
	sessions, err := matches.LoadLiveSessions(ctx, worldID)
	if err != nil {
		t.Fatalf("load live sessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("no live sessions after kickoff")
	}
	const stalledAt = 30
	for _, sess := range sessions {
		for m := 1; m <= stalledAt; m++ {
			if _, _, err := matches.PaceMinute(ctx, sess); err != nil {
				t.Fatalf("pace minute %d: %v", m, err)
			}
		}
		if got := sess.NextMinute(); got != stalledAt+1 {
			t.Fatalf("next minute after stalling = %d, want %d", got, stalledAt+1)
		}
		// The real-time cost of a match is 90 x pacing; the configured 10ms
		// cadence is what makes this test fast, and the same arithmetic is
		// 30 minutes at the seeded 20s.
		if got := sess.Pacing(); got != 10*time.Millisecond {
			t.Fatalf("pacing = %s, want 10ms", got)
		}
	}

	// A fresh call — exactly what the kickoff poll now does on every pass —
	// resumes and finishes the stalled match.
	done := make(chan error, 1)
	go func() { done <- runner.RunLive(ctx, worldID) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resume live: %v", err)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("RunLive did not return: the match never ended")
	}

	// Every match the world kicked must be completed, parked exactly on the
	// regulation bound, and still carrying the pacing frozen at kickoff.
	var total, completed, atFullTime, atPacing int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE m.status = 'completed'),
		       COUNT(*) FILTER (WHERE m.current_minute = $2),
		       COUNT(*) FILTER (WHERE m.pacing_millis = 10)
		FROM match.matches m JOIN match.fixtures f ON f.id = m.fixture_id
		WHERE f.world_id = $1`, worldID, match.RegulationMinutes).
		Scan(&total, &completed, &atFullTime, &atPacing); err != nil {
		t.Fatalf("match status: %v", err)
	}
	if total == 0 {
		t.Fatal("no matches in the world to resume")
	}
	if completed != total || atFullTime != total || atPacing != total {
		t.Fatalf("of %d matches: completed=%d at full time=%d at 10ms pacing=%d",
			total, completed, atFullTime, atPacing)
	}

	// The feed is complete and structurally sound: every match has exactly one
	// kickoff (1'), one half-time (45') and one full-time (90'), nothing outside
	// 1..90, and a gapless 1..N run of sequences.
	//
	// Note what is deliberately NOT asserted: distinct minutes == 90, and "no
	// minute holds two events". Most simulated minutes produce no event at all,
	// and several events in one minute is ordinary football, so both would fail
	// on a healthy match. The real double-pacing canary is the next check.
	var brokenSpine, offRange, brokenSeq int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM match.matches m
		JOIN match.fixtures f ON f.id = m.fixture_id
		JOIN LATERAL (
			SELECT COUNT(*) FILTER (WHERE e.event_type = 'kickoff')    AS ko,
			       COUNT(*) FILTER (WHERE e.event_type = 'half_time')  AS ht,
			       COUNT(*) FILTER (WHERE e.event_type = 'full_time')  AS ft,
			       COALESCE(MIN(e.minute) FILTER (WHERE e.event_type = 'kickoff'), -1)   AS ko_min,
			       COALESCE(MIN(e.minute) FILTER (WHERE e.event_type = 'half_time'), -1) AS ht_min,
			       COALESCE(MIN(e.minute) FILTER (WHERE e.event_type = 'full_time'), -1)  AS ft_min
			FROM match.match_events e WHERE e.match_id = m.id
		) s ON true
		WHERE f.world_id = $1
		  AND (s.ko <> 1 OR s.ht <> 1 OR s.ft <> 1
		       OR s.ko_min <> 1 OR s.ht_min <> 45 OR s.ft_min <> 90)`, worldID).Scan(&brokenSpine); err != nil {
		t.Fatalf("feed spine: %v", err)
	}
	if brokenSpine != 0 {
		t.Fatalf("%d matches lack a single kickoff/half-time/full-time at 1'/45'/90'", brokenSpine)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.match_events e
		JOIN match.matches m ON m.id = e.match_id
		WHERE m.world_id = $1 AND (e.minute < 1 OR e.minute > $2)`,
		worldID, match.RegulationMinutes).Scan(&offRange); err != nil {
		t.Fatalf("feed minutes in range: %v", err)
	}
	if offRange != 0 {
		t.Fatalf("%d events persisted outside regulation minutes 1..%d", offRange, match.RegulationMinutes)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT e.match_id FROM match.match_events e
			JOIN match.matches m ON m.id = e.match_id
			WHERE m.world_id = $1
			GROUP BY e.match_id
			HAVING COUNT(*) <> COUNT(DISTINCT e.sequence)
			    OR MIN(e.sequence) <> 1
			    OR MAX(e.sequence) <> COUNT(*)
		) g`, worldID).Scan(&brokenSeq); err != nil {
		t.Fatalf("feed sequences: %v", err)
	}
	if brokenSeq != 0 {
		t.Fatalf("%d matches have a gapped or duplicated event sequence", brokenSeq)
	}

	// The double-pacing canary: the goals the manager watched in the feed must add
	// up to the scoreline on the record. Finalize re-runs the engine once over the
	// same seed and input stream, so this only holds if the paced feed holds
	// exactly one event per engine event — a resumed loop that re-persisted a
	// minute would write that minute's events a second time under *fresh*
	// sequence numbers (the (match_id, sequence) unique index would never fire)
	// and the feed's goal tally would outrun the recorded score.
	var scoreDisagree int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT m.id
			FROM match.matches m
			JOIN match.fixtures f ON f.id = m.fixture_id
			LEFT JOIN match.match_events e ON e.match_id = m.id
			WHERE f.world_id = $1
			GROUP BY m.id, m.home_score, m.away_score, f.home_club_id, f.away_club_id
			HAVING COUNT(*) FILTER (WHERE e.event_type IN ('goal', 'penalty_scored')
			                         AND e.club_id = f.home_club_id) <> m.home_score
			    OR COUNT(*) FILTER (WHERE e.event_type IN ('goal', 'penalty_scored')
			                         AND e.club_id = f.away_club_id) <> m.away_score
		) d`, worldID).Scan(&scoreDisagree); err != nil {
		t.Fatalf("feed goals vs recorded score: %v", err)
	}
	if scoreDisagree != 0 {
		t.Fatalf("%d matches have a feed whose goals disagree with the recorded scoreline (a resumed loop double-paced)", scoreDisagree)
	}

	// Nothing live is left, so the poll's repeated RunLive calls are a no-op
	// rather than a hot loop.
	rest, err := matches.LoadLiveSessions(ctx, worldID)
	if err != nil {
		t.Fatalf("reload live sessions: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("%d live sessions after completion", len(rest))
	}
	if err := runner.RunLive(ctx, worldID); err != nil {
		t.Fatalf("idle run live: %v", err)
	}
}

// insertDueFixtures plants n already-due fixtures for toComp at a matchday,
// borrowing club pairings from fromComp's own scheduled fixtures so the pair
// never collides with toComp's real calendar. The injected fixtures are due now
// (kickoff moment already passed), so a KickoffDue pass admits them without
// waiting for the world clock.
func insertDueFixtures(t *testing.T, pool *pgxpool.Pool, worldID, fromComp, toComp uuid.UUID, matchday, n int, at time.Time) int {
	t.Helper()
	tag, err := pool.Exec(context.Background(), `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		SELECT f.world_id, $2, f.home_club_id, f.away_club_id, $3, $4, 'scheduled'
		FROM match.fixtures f
		WHERE f.world_id = $1 AND f.competition_id = $5 AND f.status = 'scheduled'
		LIMIT $6`,
		worldID, toComp, matchday, at, fromComp, n)
	if err != nil {
		t.Fatalf("insert due fixtures: %v", err)
	}
	return int(tag.RowsAffected())
}

// leagueIDs returns the world's two competition ids in creation order.
func leagueIDs(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) (first, second uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx,
		`SELECT id FROM competition.competitions WHERE world_id = $1 ORDER BY name, id`, worldID)
	if err != nil {
		t.Fatalf("load league ids: %v", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan league id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate league ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("found %d competitions, want 2", len(ids))
	}
	return ids[0], ids[1]
}

// optOutStaggered pins a competition's scheduling_rules->'staggered' to false
// so a test exercises the single-weekday kickoff path instead of the IM22
// staggered default.
func optOutStaggered(t *testing.T, pool *pgxpool.Pool, competitionID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		UPDATE competition.competition_rules
		SET scheduling_rules = COALESCE(scheduling_rules, '{}'::jsonb) || '{"staggered": false}'::jsonb
		WHERE competition_id = $1`, competitionID); err != nil {
		t.Fatalf("opt out of staggered scheduling: %v", err)
	}
}

// TestRunnerCapOnlyForStaggered pins the IM22 cap at kickoff time: a staggered
// competition (2+ allowed weekdays) never runs more than its
// max_simultaneous_matches (default 3) of a round live at once, so the 4-th
// due fixture is deferred; opt the same competition back to a single weekday
// and the deferred fixture kicks immediately with no cap.
func TestRunnerCapOnlyForStaggered(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, compSvc)

	first, second := leagueIDs(t, pool, worldID)
	target, donor := second, first // Premier (staggered default) is the cap target
	optOutStaggered(t, pool, donor)

	dueAt := scaleNow(t, pool, worldID)
	if n := insertDueFixtures(t, pool, worldID, donor, target, 5, 4, dueAt); n != 4 {
		t.Fatalf("inserted %d due fixtures, want 4", n)
	}

	sum, err := runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("cap run: %v", err)
	}
	if sum.Matchdays != 1 || sum.Kicked != 3 || sum.Skipped != 1 {
		t.Fatalf("cap run matchdays=%d kicked=%d skipped=%d, want 1/3/1 (cap 3 of 4 due)", sum.Matchdays, sum.Kicked, sum.Skipped)
	}
	var live, stillScheduled int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status = 'live'),
		       COUNT(*) FILTER (WHERE status = 'scheduled')
		FROM match.fixtures
		WHERE world_id = $1 AND competition_id = $2 AND matchday = 5`,
		worldID, target).Scan(&live, &stillScheduled); err != nil {
		t.Fatalf("post-cap state: %v", err)
	}
	if live != 3 || stillScheduled != 1 {
		t.Fatalf("after cap: live=%d still_scheduled=%d, want 3 live and 1 deferred", live, stillScheduled)
	}

	// Opt the target competition out of staggering: the deferred fixture now
	// kicks — single-day rounds cap at nothing.
	optOutStaggered(t, pool, target)
	sum, err = runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("uncapped run: %v", err)
	}
	if sum.Matchdays != 1 || sum.Kicked != 1 || sum.Skipped != 0 {
		t.Fatalf("uncapped run matchdays=%d kicked=%d skipped=%d, want 1/1/0", sum.Matchdays, sum.Kicked, sum.Skipped)
	}
}

// TestRunnerFinalRoundBypassesCap: the season-final matchday of a staggered
// league is excluded from the cap — every tie of the final round kicks at once
// (StaggeredCap returns 0 when the open matchday is the competition's last).
func TestRunnerFinalRoundBypassesCap(t *testing.T) {
	pool, worldID, compSvc := runnerWorld(t)
	ctx := context.Background()

	matches := match.NewService(pool, nil, squad.NewStore(pool), form.NewStore(pool))
	runner := NewRunner(pool, matches, compSvc)

	first, second := leagueIDs(t, pool, worldID)
	target, donor := second, first

	dueAt := scaleNow(t, pool, worldID)
	if n := insertDueFixtures(t, pool, worldID, donor, target, 6, 4, dueAt); n != 4 {
		t.Fatalf("inserted %d due fixtures, want 4", n)
	}

	sum, err := runner.KickoffDue(ctx, worldID)
	if err != nil {
		t.Fatalf("final run: %v", err)
	}
	if sum.Matchdays != 1 || sum.Kicked != 4 || sum.Skipped != 0 {
		t.Fatalf("final run matchdays=%d kicked=%d skipped=%d, want 1/4/0 (final round bypasses cap)",
			sum.Matchdays, sum.Kicked, sum.Skipped)
	}
}
