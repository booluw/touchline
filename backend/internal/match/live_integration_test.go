//go:build integration

package match

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/matchsim"
)

// liveWorld builds a world with one human-managed home club, one AI away club,
// one scheduled matchday-1 fixture, and an active manager owning the home club.
// It returns the pool, the ids, and the fixture id.
func liveWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	worldID, homeID, awayID := worldFor(t, pool, ctx, "live-it", "Harbour City FC", "Northfield Rovers")

	var fixtureID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Live Test League', 'league', 10, 0, 'active') RETURNING id`, worldID).Scan(&fixtureID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $4, 1, now() - interval '1 day', 'scheduled') RETURNING id`,
		worldID, fixtureID, homeID, awayID).Scan(&fixtureID); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	managerID := uuid.MustParse("00000000-0000-0000-0000-0000000000e1")
	if err := pool.QueryRow(ctx, `
		SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`, homeID).
		Scan(&managerID); err != nil {
		t.Fatalf("load home manager: %v", err)
	}
	return pool, worldID, homeID, awayID, fixtureID, managerID
}

// kickoff kicks the world's matchday-1 fixture and returns its live session.
func kickoff(t *testing.T, svc *Service, pool *pgxpool.Pool, worldID uuid.UUID) *LiveSession {
	t.Helper()
	sessions, err := svc.KickoffMatchday(context.Background(), worldID, 1)
	if err != nil {
		t.Fatalf("kickoff: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("kickoff sessions = %d, want 1", len(sessions))
	}
	return sessions[0]
}

// paceToFullTime drives a session to full time one minute at a time.
func paceToFullTime(t *testing.T, svc *Service, sess *LiveSession) {
	t.Helper()
	ctx := context.Background()
	maxTries := 100
	for sess.NextMinute() <= 90 && maxTries > 0 {
		if _, _, err := svc.PaceMinute(ctx, sess); err != nil {
			t.Fatalf("pace: %v", err)
		}
		maxTries--
	}
	if sess.NextMinute() <= 90 {
		t.Fatalf("exhausted pacing attempts at minute %d", sess.NextMinute())
	}
}

// assertReplaysExpected verifies the persisted feed is structurally the pure
// engine's Simulate output over the same (seed, teams, inputs): sequence,
// minute, event type, and club ids must match event-for-event.
func assertReplaysExpected(t *testing.T, pool *pgxpool.Pool, matchID uuid.UUID, expected []matchsim.MatchEvent) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT sequence, minute, event_type, club_id FROM match.match_events
		WHERE match_id = $1 ORDER BY sequence`, matchID)
	if err != nil {
		t.Fatalf("load persisted events: %v", err)
	}
	defer rows.Close()

	var got []struct {
		seq, minute int
		typ         string
		club        *uuid.UUID
	}
	for rows.Next() {
		var e struct {
			seq, minute int
			typ         string
			club        *uuid.UUID
		}
		if err := rows.Scan(&e.seq, &e.minute, &e.typ, &e.club); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		got = append(got, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate events: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("persisted %d events, engine produced %d", len(got), len(expected))
	}
	for i := range expected {
		ev := expected[i]
		g := got[i]
		var clubOK bool
		if ev.ClubID == "" {
			clubOK = g.club == nil
		} else {
			clubOK = g.club != nil && g.club.String() == ev.ClubID
		}
		if g.seq != ev.Sequence || g.minute != ev.Minute || g.typ != ev.Type || !clubOK {
			t.Fatalf("event %d mismatch: got (%d,%d,%s,%v) want (%d,%d,%s,%q)",
				i, g.seq, g.minute, g.typ, clubIDString(g.club), ev.Sequence, ev.Minute, ev.Type, ev.ClubID)
		}
	}
}

func clubIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func TestKickoffReplaysInstantSimulate(t *testing.T) {
	pool, worldID, _, _, _, _ := liveWorld(t)
	svc := newMatchService(pool)
	sess := kickoff(t, svc, pool, worldID)

	paceToFullTime(t, svc, sess)
	if _, err := svc.Finalize(context.Background(), sess); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// The paced stream must equal a single instant Simulate over the same
	// frozen team snapshot — the OPD-21 determinism contract.
	expected := matchsim.Simulate(matchsim.Options{
		Seed: sess.Seed, Home: sess.Home, Away: sess.Away, Tuning: matchsim.DefaultTuning(),
	})
	assertReplaysExpected(t, pool, sess.MatchID, expected.Events)

	var homeScore, awayScore int
	if err := pool.QueryRow(context.Background(), `
		SELECT home_score, away_score FROM match.matches WHERE id = $1`, sess.MatchID).
		Scan(&homeScore, &awayScore); err != nil {
		t.Fatalf("scores: %v", err)
	}
	if homeScore != expected.HomeGoals || awayScore != expected.AwayGoals {
		t.Fatalf("live score %d–%d, instant simulate %d–%d", homeScore, awayScore, expected.HomeGoals, expected.AwayGoals)
	}
}

func TestLiveSubstitutionReplay(t *testing.T) {
	pool, worldID, homeID, _, _, managerID := liveWorld(t)
	svc := newMatchService(pool)
	sess := kickoff(t, svc, pool, worldID)

	if len(sess.homeXI) != 11 || len(sess.homeBench) == 0 {
		t.Fatalf("home squad not materialised for validation (xi=%d bench=%d)", len(sess.homeXI), len(sess.homeBench))
	}
	out := sess.homeXI[0].PlayerID
	in := sess.homeBench[0].PlayerID

	ctx := context.Background()
	// Pace to just before the 60' window, then submit the manager's choice.
	for sess.NextMinute() < 60 {
		if _, _, err := svc.PaceMinute(ctx, sess); err != nil {
			t.Fatalf("pace to 60: %v", err)
		}
	}
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 60, out, in); err != nil {
		t.Fatalf("substitute: %v", err)
	}

	paceToFullTime(t, svc, sess)
	if _, err := svc.Finalize(ctx, sess); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// The live stream equals the instant replay WITH the manager input.
	input := matchsim.LiveInput{
		Minute: 60, ClubID: homeID.String(), Kind: "substitution",
		Detail: map[string]any{"club_id": homeID.String(), "player_in": in.String(), "player_out": out.String()},
	}
	expected := matchsim.Simulate(matchsim.Options{
		Seed: sess.Seed, Home: sess.Home, Away: sess.Away, Tuning: matchsim.DefaultTuning(),
		LiveInputs: []matchsim.LiveInput{input},
	})
	if len(expected.Events) == 0 {
		t.Fatalf("empty engine replay")
	}
	assertReplaysExpected(t, pool, sess.MatchID, expected.Events)

	// The minute-60 feed contains a substitution linked to the manager's exact
	// chosen players (a coincidental natural injury-sub may also occur that
	// minute and land after it, so the check is order-independent).
	var linked int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.match_events
		WHERE match_id = $1 AND minute = 60 AND event_type = 'substitution'
		  AND club_id = $2 AND player_id = $3 AND related_player_id = $4`,
		sess.MatchID, homeID, in, out).Scan(&linked); err != nil {
		t.Fatalf("load substitution: %v", err)
	}
	if linked < 1 {
		t.Fatalf("no minute-60 substitution links player_in=%s player_out=%s", in, out)
	}
}

func TestRehydrateMidMatch(t *testing.T) {
	pool, worldID, _, _, _, _ := liveWorld(t)
	svc := newMatchService(pool)
	sess := kickoff(t, svc, pool, worldID)
	ctx := context.Background()

	for sess.NextMinute() <= 30 {
		if _, _, err := svc.PaceMinute(ctx, sess); err != nil {
			t.Fatalf("pace to 30: %v", err)
		}
	}
	if got := sess.NextMinute(); got != 31 {
		t.Fatalf("next minute after pacing = %d, want 31", got)
	}

	// Rehydrate as a fresh worker would after a crash, and finish the match
	// from the frozen snapshot + partial feed.
	sessions, err := svc.LoadLiveSessions(ctx, worldID)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("rehydrated sessions = %d, want 1", len(sessions))
	}
	rs := sessions[0]
	if rs.MatchID != sess.MatchID || rs.Seed != sess.Seed || rs.NextMinute() != 31 {
		t.Fatalf("rehydrated session mismatch (match=%v seed=%d next=%d)", rs.MatchID == sess.MatchID, rs.Seed, rs.NextMinute())
	}

	paceToFullTime(t, svc, rs)
	if _, err := svc.Finalize(ctx, rs); err != nil {
		t.Fatalf("finalize after rehydrate: %v", err)
	}
	expected := matchsim.Simulate(matchsim.Options{
		Seed: rs.Seed, Home: rs.Home, Away: rs.Away, Tuning: matchsim.DefaultTuning(),
	})
	assertReplaysExpected(t, pool, rs.MatchID, expected.Events)
}

func TestKickoffIdempotent(t *testing.T) {
	pool, worldID, _, _, _, _ := liveWorld(t)
	svc := newMatchService(pool)
	ctx := context.Background()

	sessions, err := svc.KickoffMatchday(ctx, worldID, 1)
	if err != nil {
		t.Fatalf("kickoff: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("first kickoff sessions = %d, want 1", len(sessions))
	}

	// Redelivery: the fixture is live, nothing new is created.
	again, err := svc.KickoffMatchday(ctx, worldID, 1)
	if err != nil {
		t.Fatalf("redelivered kickoff: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("redelivered kickoff sessions = %d, want 0", len(again))
	}
	var matches, liveFixtures int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM match.matches WHERE fixture_id = $1`, sessions[0].FixtureID).Scan(&matches); err != nil {
		t.Fatalf("count matches: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM match.fixtures WHERE world_id = $1 AND status = 'live'`, worldID).Scan(&liveFixtures); err != nil {
		t.Fatalf("count live: %v", err)
	}
	if matches != 1 || liveFixtures != 1 {
		t.Fatalf("matches=%d live=%d, want 1/1", matches, liveFixtures)
	}
}

func TestSubstituteValidation(t *testing.T) {
	pool, worldID, _, _, _, managerID := liveWorld(t)
	svc := newMatchService(pool)
	sess := kickoff(t, svc, pool, worldID)
	ctx := context.Background()

	if len(sess.homeXI) != 11 || len(sess.homeBench) < 2 {
		t.Fatalf("home squad not materialised (xi=%d bench=%d)", len(sess.homeXI), len(sess.homeBench))
	}
	xi0, xi1 := sess.homeXI[0].PlayerID, sess.homeXI[1].PlayerID
	b0, b1 := sess.homeBench[0].PlayerID, sess.homeBench[1].PlayerID

	// A manager who does not own either club is rejected.
	if err := svc.Substitute(ctx, sess.MatchID, uuid.MustParse("00000000-0000-0000-0000-0000000000e2"), 60, xi0, b0); err != ErrManagerNotInvolved {
		t.Fatalf("foreign manager err = %v, want ErrManagerNotInvolved", err)
	}
	// Non-window minutes are rejected.
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 45, xi0, b0); err != ErrNotAtSubWindow {
		t.Fatalf("window err = %v, want ErrNotAtSubWindow", err)
	}
	// The incoming player must be on the bench.
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 60, xi0, xi1); err != ErrPlayerNotOnBench {
		t.Fatalf("on-bench err = %v, want ErrPlayerNotOnBench", err)
	}
	// The outgoing player must be on the pitch.
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 60, b0, b1); err != ErrPlayerNotOnPitch {
		t.Fatalf("on-pitch err = %v, want ErrPlayerNotOnPitch", err)
	}

	// A valid substitution lands in the ordered input stream.
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 60, xi0, b0); err != nil {
		t.Fatalf("valid substitute: %v", err)
	}
	var stored int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM match.match_inputs WHERE match_id = $1`, sess.MatchID).Scan(&stored); err != nil {
		t.Fatalf("count inputs: %v", err)
	}
	if stored != 1 {
		t.Fatalf("inputs stored = %d, want 1", stored)
	}

	// A second substitution at the same window/side is rejected.
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 60, xi1, b1); err != ErrDuplicateWindowSub {
		t.Fatalf("duplicate err = %v, want ErrDuplicateWindowSub", err)
	}

	// After the minute has passed (pace to 61'), a 60' input is closed.
	for sess.NextMinute() < 61 {
		if _, _, err := svc.PaceMinute(ctx, sess); err != nil {
			t.Fatalf("pace: %v", err)
		}
	}
	if err := svc.Substitute(ctx, sess.MatchID, managerID, 60, xi1, b1); err != ErrMinuteClosed {
		t.Fatalf("closed-minute err = %v, want ErrMinuteClosed", err)
	}
}

func TestTacticChangeRecorded(t *testing.T) {
	pool, worldID, homeID, _, _, managerID := liveWorld(t)
	svc := newMatchService(pool)
	sess := kickoff(t, svc, pool, worldID)
	ctx := context.Background()

	// Past minutes are closed to new inputs...
	for sess.NextMinute() < 21 {
		if _, _, err := svc.PaceMinute(ctx, sess); err != nil {
			t.Fatalf("pace to 20: %v", err)
		}
	}
	if err := svc.TacticChange(ctx, sess.MatchID, managerID, 20, map[string]any{"style": "low_block"}); err != ErrMinuteClosed {
		t.Fatalf("closed-minute tactic change err = %v, want ErrMinuteClosed", err)
	}
	// Reject styles outside the approved catalogue.
	if err := svc.TacticChange(ctx, sess.MatchID, managerID, 30, map[string]any{"style": "park_the_bus"}); err != ErrInvalidTacticStyle {
		t.Fatalf("invalid style err = %v, want ErrInvalidTacticStyle", err)
	}
	// ...future minutes accept inputs into the ordered stream.
	if err := svc.TacticChange(ctx, sess.MatchID, managerID, 30, map[string]any{"style": "gegenpress"}); err != nil {
		t.Fatalf("tactic change: %v", err)
	}
	var club string
	if err := pool.QueryRow(ctx, `
		SELECT payload->>'club_id' FROM match.match_inputs
		WHERE match_id = $1 AND kind = 'tactic_change' ORDER BY sequence LIMIT 1`, sess.MatchID).Scan(&club); err != nil {
		t.Fatalf("tactic input: %v", err)
	}
	if club != homeID.String() {
		t.Fatalf("tactic club = %q, want %s", club, homeID)
	}
}
