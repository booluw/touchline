//go:build integration

package player_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/player"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/internal/transfertest"
)

// newPlayerFixture boots the S06-01 world and wires the transfer + player
// engines onto it (the player service needs the transfer service both to list
// approved requests and to drive the weekly auto-list sweep).
func newPlayerFixture(t *testing.T, name string) (*player.Service, *transfer.Service, *pgxpool.Pool, transfertest.World) {
	t.Helper()
	pool := testdb.New(t)
	tw := transfertest.Provision(t, pool, name, name+"-owner@example.com")
	ts := transfer.NewService(pool, nil)
	return player.NewService(pool, nil, ts), ts, pool, tw
}

func pickPlayer(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		clubID).Scan(&id); err != nil {
		t.Fatalf("pick player of %s: %v", clubID, err)
	}
	return id
}

func setRole(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID, role string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE player.contracts SET squad_role = $2 WHERE player_id = $1 AND status = 'active'`,
		playerID, role); err != nil {
		t.Fatalf("set role %s on %s: %v", role, playerID, err)
	}
}

func setCondition(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID, morale, share float64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO player.player_condition (player_id, morale, playing_time_pct, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (player_id) DO UPDATE
		SET morale = EXCLUDED.morale, playing_time_pct = EXCLUDED.playing_time_pct, updated_at = now()`,
		playerID, morale, share); err != nil {
		t.Fatalf("set condition %s: %v", playerID, err)
	}
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

// seedCompletedMatch materializes one completed fixture + match row for the
// club so the whole-season denominator (90 x club matches) is well-defined.
func seedCompletedMatch(t *testing.T, pool *pgxpool.Pool, worldID, clubID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var competitionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Morale Test League', 'league', 10, 0, 'active') RETURNING id`, worldID).Scan(&competitionID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	var fixtureID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $3, 1, now() - interval '2 days', 'completed') RETURNING id`,
		worldID, competitionID, clubID).Scan(&fixtureID); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.matches (fixture_id, world_id, seed, engine_version, home_score, away_score, status, started_at, ended_at)
		VALUES ($1, $2, 0, 'test', 1, 1, 'completed', now() - interval '1 day', now() - interval '1 day')
		RETURNING id`, fixtureID, worldID).Scan(&matchID); err != nil {
		t.Fatalf("insert match: %v", err)
	}
	return matchID
}

func TestRecordMatchAppearancesInsideTx(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "appearances-tx")
	ctx := context.Background()

	pid := pickPlayer(t, pool, tw.HumanClub)
	setRole(t, pool, pid, player.SquadRoleKeyPlayer)
	matchID := seedCompletedMatch(t, pool, tw.WorldID, tw.HumanClub)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := svc.RecordMatchAppearances(ctx, tx, matchID, []player.Appearance{
		{PlayerID: pid, Started: true, Minutes: 90},
	}); err != nil {
		t.Fatalf("record appearances: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Whole-season share for a lone completed match: 90 / (90 x 1) = 1.
	if n := count(t, pool,
		`SELECT COUNT(*) FROM player.player_appearances WHERE player_id = $1 AND started AND minutes = 90`, pid); n != 1 {
		t.Errorf("appearance row = %d, want 1", n)
	}
	row := pool.QueryRow(ctx,
		`SELECT morale, playing_time_pct FROM player.player_condition WHERE player_id = $1`, pid)
	var morale, share float64
	if err := row.Scan(&morale, &share); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	if share != 1.0 {
		t.Errorf("share = %v, want 1.0", share)
	}
	if morale < player.MoraleTargetSatisfied-1e-4 {
		t.Errorf("key player at full share: morale = %v, want ≥ %v", morale, player.MoraleTargetSatisfied)
	}

	// The roster and detail reads expose the same numbers.
	rows, err := svc.ListSquadMorale(ctx, tw.WorldID, tw.HumanMgr)
	if err != nil {
		t.Fatalf("list squad: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.PlayerID == pid {
			found = true
			if r.SquadRole != player.SquadRoleKeyPlayer {
				t.Errorf("row role = %q, want key_player", r.SquadRole)
			}
			if r.Morale != morale {
				t.Errorf("row morale = %v, want %v", r.Morale, morale)
			}
		}
	}
	if !found {
		t.Fatalf("player not in squad read")
	}
	detail, err := svc.GetPlayerMoraleDetail(ctx, tw.WorldID, tw.HumanMgr, pid)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Expectations[0].Status != "satisfied" {
		t.Errorf("detail status = %q, want satisfied", detail.Expectations[0].Status)
	}
}

func TestWeeklyTickRaisesTransferRequestForDeepShortfall(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "request-trigger")
	ctx := context.Background()

	pid := pickPlayer(t, pool, tw.HumanClub)
	setRole(t, pool, pid, player.SquadRoleSquadPlayer)
	setCondition(t, pool, pid, 0.30, 0.0) // deep shortfall: 0 < 0.5 × 0.2

	if err := svc.WeeklyTick(ctx, tw.WorldID, 1); err != nil {
		t.Fatalf("weekly tick: %v", err)
	}

	if n := count(t, pool,
		`SELECT COUNT(*) FROM player.player_transfer_requests WHERE player_id = $1 AND status = 'pending'`, pid); n != 1 {
		t.Errorf("pending request = %d, want 1", n)
	}
	if n := count(t, pool,
		`SELECT COUNT(*) FROM world.events WHERE event_type = 'PLAYER_TRANSFER_REQUESTED' AND payload::text LIKE '%' || $1::text || '%'`,
		pid); n < 1 {
		t.Errorf("requested event = %d, want ≥ 1", n)
	}
	var mgr uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT manager_id FROM player.player_transfer_requests WHERE player_id = $1 AND status = 'pending'`, pid).Scan(&mgr); err != nil {
		t.Fatalf("load request: %v", err)
	}
	if mgr != tw.HumanMgr {
		t.Errorf("request manager = %v, want human %v", mgr, tw.HumanMgr)
	}
}

func TestDenyPlayerRequestDropsMoraleAndSetsCooldown(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "deny")
	ctx := context.Background()

	pid := pickPlayer(t, pool, tw.HumanClub)
	setRole(t, pool, pid, player.SquadRoleSquadPlayer)
	setCondition(t, pool, pid, 0.30, 0.0)
	if err := svc.WeeklyTick(ctx, tw.WorldID, 1); err != nil {
		t.Fatalf("weekly tick: %v", err)
	}

	before := 0.31 // post-recovery read from the weekly pass
	req, err := svc.DenyPlayerRequest(ctx, tw.WorldID, tw.HumanMgr, pid)
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	if req.Status != player.TransferRequestDenied {
		t.Errorf("status = %q, want denied", req.Status)
	}
	if got := count(t, pool,
		`SELECT COUNT(*) FROM social.relationship_events
		 WHERE player_id = $1 AND event_type = 'transfer_denied' AND sentiment_delta = -25`, pid); got != 1 {
		t.Errorf("denial memory rows = %d, want 1", got)
	}
	var morale float64
	if err := pool.QueryRow(ctx,
		`SELECT morale FROM player.player_condition WHERE player_id = $1`, pid).Scan(&morale); err != nil {
		t.Fatalf("load morale: %v", err)
	}
	if want := before - player.DenyMoraleDrop; morale > want+1e-4 {
		t.Errorf("morale after deny = %v, want ≤ %v", morale, want)
	}
	if n := count(t, pool,
		`SELECT COUNT(*) FROM player.player_condition
		 WHERE player_id = $1 AND transfer_request_cooldown_until > now()`, pid); n != 1 {
		t.Errorf("cooldown not armed after deny")
	}
	if n := count(t, pool,
		`SELECT COUNT(*) FROM social.relationships
		 WHERE entity_a_id = $1 AND entity_b_id = $2 AND relationship_type = 'dislike' AND sentiment < 0`,
		pid, tw.HumanMgr); n != 1 {
		t.Errorf("canonical sentiment not flipped to dislike")
	}
}

func TestApprovePlayerRequestListsPlayer(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "approve")
	ctx := context.Background()

	pid := pickPlayer(t, pool, tw.HumanClub)
	setRole(t, pool, pid, player.SquadRoleSquadPlayer)
	setCondition(t, pool, pid, 0.30, 0.0)
	if err := svc.WeeklyTick(ctx, tw.WorldID, 1); err != nil {
		t.Fatalf("weekly tick: %v", err)
	}

	view, err := svc.ApprovePlayerRequest(ctx, tw.WorldID, tw.HumanMgr, pid)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if view.Request.Status != player.TransferRequestApproved {
		t.Errorf("request status = %q, want approved", view.Request.Status)
	}
	if view.Listing == nil || view.Listing.PlayerID != pid {
		t.Errorf("approve should create a market listing for the player, got %+v", view.Listing)
	}
	if n := count(t, pool,
		`SELECT COUNT(*) FROM transfer.listings WHERE player_id = $1 AND status = 'open'`, pid); n != 1 {
		t.Errorf("open listings = %d, want 1", n)
	}
	if n := count(t, pool,
		`SELECT COUNT(*) FROM social.relationship_events
		 WHERE player_id = $1 AND event_type = 'transfer_approved' AND sentiment_delta = 15`, pid); n != 1 {
		t.Errorf("approval memory rows = %d, want 1", n)
	}
	// The approved player is free to request again only elsewhere — the open
	// request is fully closed.
	if n := count(t, pool,
		`SELECT COUNT(*) FROM player.player_transfer_requests WHERE player_id = $1 AND status = 'pending'`, pid); n != 0 {
		t.Errorf("open request left after approve")
	}
}

func TestPromisePlayingTimeGradedWeekly(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "promise")
	ctx := context.Background()

	pid := pickPlayer(t, pool, tw.HumanClub)
	setRole(t, pool, pid, player.SquadRoleSquadPlayer)
	setCondition(t, pool, pid, 0.45, 0.0)

	// A kept promise: raise the share so the promise is fulfilled on the next
	// weekly pass.
	if err := svc.PromisePlayingTime(ctx, tw.WorldID, tw.HumanMgr, pid); err != nil {
		t.Fatalf("promise: %v", err)
	}
	setCondition(t, pool, pid, 0.45, 0.30) // 0.30 ≥ 0.20 entitlement
	if err := svc.WeeklyTick(ctx, tw.WorldID, 2); err != nil {
		t.Fatalf("weekly tick (kept): %v", err)
	}
	if got := count(t, pool,
		`SELECT COUNT(*) FROM social.promises WHERE player_id = $1 AND status = 'fulfilled'`, pid); got != 1 {
		t.Errorf("fulfilled promise = %d, want 1", got)
	}
	if got := count(t, pool,
		`SELECT COUNT(*) FROM social.relationship_events
		 WHERE player_id = $1 AND event_type = 'playing_time_promise_kept'`, pid); got != 1 {
		t.Errorf("promise-kept memory = %d, want 1", got)
	}

	// A broken promise: new promise, still benched, backdated past the grading
	// window — the weekly pass marks it broken and frees the player.
	if err := svc.PromisePlayingTime(ctx, tw.WorldID, tw.HumanMgr, pid); err != nil {
		t.Fatalf("promise 2: %v", err)
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -player.PromiseEvaluationWeeks*7-1)
	if _, err := pool.Exec(ctx,
		`UPDATE social.promises SET created_at = $2 WHERE player_id = $1 AND status = 'pending'`, pid, cutoff); err != nil {
		t.Fatalf("backdate promise: %v", err)
	}
	if err := svc.WeeklyTick(ctx, tw.WorldID, 3); err != nil {
		t.Fatalf("weekly tick (broken): %v", err)
	}
	if got := count(t, pool,
		`SELECT COUNT(*) FROM social.promises WHERE player_id = $1 AND status = 'broken'`, pid); got != 1 {
		t.Errorf("broken promise = %d, want 1", got)
	}
	if got := count(t, pool,
		`SELECT COUNT(*) FROM social.relationship_events
		 WHERE player_id = $1 AND event_type = 'playing_time_promise_broken' AND sentiment_delta = -30`, pid); got != 1 {
		t.Errorf("promise-broken memory = %d, want 1", got)
	}
}

func TestOnPlayerTransferredFreshStart(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "fresh-start")
	ctx := context.Background()

	target := pickPlayer(t, pool, tw.AIOneClub)
	setCondition(t, pool, target, 0.10, 0.42)
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_transfer_requests (player_id, club_id, manager_id, status, reason, created_at)
		VALUES ($1, $2, $3, 'pending', 'playing_time', now())`,
		target, tw.AIOneClub, tw.AIOneMgr); err != nil {
		t.Fatalf("insert request: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := svc.OnPlayerTransferred(ctx, tx, target, tw.AITwoClub); err != nil {
		t.Fatalf("on transferred: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var morale, share float64
	if err := pool.QueryRow(ctx,
		`SELECT morale, playing_time_pct FROM player.player_condition WHERE player_id = $1`, target).Scan(&morale, &share); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	if morale != player.FreshStartMorale {
		t.Errorf("fresh morale = %v, want %v", morale, player.FreshStartMorale)
	}
	if share != 0 {
		t.Errorf("fresh share = %v, want 0", share)
	}
	if n := count(t, pool,
		`SELECT COUNT(*) FROM player.player_transfer_requests WHERE player_id = $1 AND status = 'withdrawn'`, target); n != 1 {
		t.Errorf("withdrawn request = %d, want 1", n)
	}
}
