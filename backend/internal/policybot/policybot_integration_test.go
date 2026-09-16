//go:build integration

package policybot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/training"
	"github.com/touchline/backend/internal/transfer"
	internalworld "github.com/touchline/backend/internal/world"
)

// botWorld seeds an active world with one human-managed club and returns the
// pool/world/club/manager ids plus a fully-wired Service.
func botWorld(t *testing.T) (*pgxpool.Pool, *Service, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "policybot-world")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	worldID := w.ID
	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, worldID, "Harbour Bot FC", "")
	if err != nil {
		t.Fatalf("bootstrap club: %v", err)
	}
	clubID := res.ClubID
	if _, err := internalworld.NewService(pool, nil).SetStatus(ctx, worldID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	var managerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`, clubID).
		Scan(&managerID); err != nil {
		t.Fatalf("load manager: %v", err)
	}

	squadStore := squad.NewStore(pool)
	tacticsSvc := tactics.NewService(pool, nil, squadStore)
	trainingSvc := training.NewService(pool, nil)
	transferSvc := transfer.NewService(pool, nil)
	svc := NewService(pool, nil, squadStore, tacticsSvc, trainingSvc, transferSvc)
	return pool, svc, worldID, clubID, managerID
}

func TestPolicyUpsertGetDelete(t *testing.T) {
	_, svc, worldID, _, managerID := botWorld(t)
	ctx := context.Background()
	actor := Actor{ManagerID: managerID}

	params, _ := json.Marshal(SquadPolicy{Rule: RuleRotate, Tactics: "controlling"})
	p, err := svc.UpsertPolicy(ctx, actor, worldID, managerID, TypeSquad, params)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if p.Type != TypeSquad || !p.Enabled {
		t.Fatalf("upsert produced wrong row: %+v", p)
	}

	got, err := svc.GetPolicy(ctx, managerID, TypeSquad)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var sp SquadPolicy
	if err := json.Unmarshal(got.Params, &sp); err != nil {
		t.Fatalf("params: %v", err)
	}
	if sp.Rule != RuleRotate {
		t.Fatalf("expected rotate rule, got %q", sp.Rule)
	}

	policies, err := svc.ListPolicies(ctx, managerID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}

	found, err := svc.DeletePolicy(ctx, actor, worldID, managerID, TypeSquad)
	if err != nil || !found {
		t.Fatalf("delete: found=%v err=%v", found, err)
	}
	if _, err := svc.GetPolicy(ctx, managerID, TypeSquad); err != ErrNoPolicy {
		t.Fatalf("expected ErrNoPolicy after delete, got %v", err)
	}
}

func TestSetAwayOracle(t *testing.T) {
	pool, svc, worldID, clubID, managerID := botWorld(t)
	ctx := context.Background()

	if err := svc.SetAway(ctx, worldID, managerID, true); err != nil {
		t.Fatalf("set away: %v", err)
	}
	view, err := svc.GetAbsence(ctx, worldID, managerID)
	if err != nil {
		t.Fatalf("get absence: %v", err)
	}
	if !view.AbsenceState.IsAway() || view.AbsenceState.AwayAuto {
		t.Fatalf("expected explicit away, got %+v", view.AbsenceState)
	}
	if len(view.ClubIDs) != 1 || view.ClubIDs[0] != clubID {
		t.Fatalf("expected club %s in view, got %v", clubID, view.ClubIDs)
	}

	if err := svc.SetAway(ctx, worldID, managerID, false); err != nil {
		t.Fatalf("clear away: %v", err)
	}
	view, _ = svc.GetAbsence(ctx, worldID, managerID)
	if view.AbsenceState.IsAway() {
		t.Fatalf("expected away cleared, got %+v", view.AbsenceState)
	}

	// Activity heartbeat pings the store and stays a no-op through the API.
	if err := svc.TouchActivity(ctx, managerID); err != nil {
		t.Fatalf("touch: %v", err)
	}
	var lastAct *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT last_activity_at FROM manager.managers WHERE id = $1`, managerID).Scan(&lastAct); err != nil {
		t.Fatalf("load activity: %v", err)
	}
	if lastAct == nil {
		t.Fatalf("expected activity heartbeat written")
	}
}

func TestEnsureMatchInputsLineupWritten(t *testing.T) {
	pool, svc, worldID, clubID, managerID := botWorld(t)
	ctx := context.Background()

	// Create a scheduled fixture for the club.
	var fixtureID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures
			(world_id, matchday, home_club_id, away_club_id, scheduled_at, status)
		VALUES ($1, 1, $2, $3, now(), 'scheduled')
		RETURNING id`, worldID, clubID, uuid.New()).Scan(&fixtureID); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	if err := svc.SetAway(ctx, worldID, managerID, true); err != nil {
		t.Fatalf("set away: %v", err)
	}
	if err := svc.EnsureMatchInputs(ctx, fixtureID); err != nil {
		t.Fatalf("ensure match inputs: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM club.club_lineups WHERE club_id = $1`, clubID).Scan(&count); err != nil {
		t.Fatalf("count lineup: %v", err)
	}
	if count != 11 {
		t.Fatalf("expected 11 lineup rows written by bot, got %d", count)
	}
}

func TestEnsureTrainingNoPlanGetsArchetype(t *testing.T) {
	pool, svc, worldID, clubID, managerID := botWorld(t)
	ctx := context.Background()

	if err := svc.SetAway(ctx, worldID, managerID, true); err != nil {
		t.Fatalf("set away: %v", err)
	}
	if err := svc.EnsureTraining(ctx, worldID); err != nil {
		t.Fatalf("ensure training: %v", err)
	}

	var archetype string
	err := pool.QueryRow(ctx,
		`SELECT archetype FROM club.club_training_plans WHERE club_id = $1`, clubID).Scan(&archetype)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if archetype == "" {
		t.Fatalf("expected an archetype to be submitted for the away club")
	}
}

func TestAttendOrMissAutoAway(t *testing.T) {
	pool, svc, worldID, _, managerID := botWorld(t)
	ctx := context.Background()

	// No activity ever; run through four unattended fixtures with a
	// prevKickoff in the past => streak climbs to auto-away on the third.
	bot, err := (&Store{pool: pool}).GetOrCreateAbsenceBot(ctx, worldID)
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}
	if bot == uuid.Nil {
		t.Fatalf("expected a bot manager")
	}

	prev := time.Now().Add(-2 * time.Hour)
	store := NewStore(pool)
	var activated bool
	for i := 0; i < 4; i++ {
		activated, err = store.AttendOrMiss(ctx, managerID, &prev)
		if err != nil {
			t.Fatalf("attend/miss %d: %v", i, err)
		}
	}
	if !activated {
		t.Fatalf("expected auto-away to activate at streak threshold")
	}

	state, err := svc.GetAbsence(ctx, worldID, managerID)
	if err != nil {
		t.Fatalf("get absence: %v", err)
	}
	if !state.AbsenceState.IsAway() || !state.AbsenceState.AwayAuto {
		t.Fatalf("expected auto-away active, got %+v", state.AbsenceState)
	}
	if state.AbsenceState.ConsecutiveMissed < int(MissedFixtureThreshold) {
		t.Fatalf("streak %d below threshold", state.AbsenceState.ConsecutiveMissed)
	}
}

func TestRespondToBidsForAbsent(t *testing.T) {
	pool, svc, worldID, clubID, managerID := botWorld(t)
	ctx := context.Background()

	if err := svc.SetAway(ctx, worldID, managerID, true); err != nil {
		t.Fatalf("set away: %v", err)
	}

	// Seed a player on the club and a pending incoming bid.
	var playerID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM player.players WHERE club_id = $1 LIMIT 1`, clubID).Scan(&playerID); err != nil {
		t.Fatalf("load player: %v", err)
	}
	var buyerClub uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM club.clubs WHERE world_id = $1 AND id <> $2 LIMIT 1`, worldID, clubID).Scan(&buyerClub); err != nil {
		t.Fatalf("load buyer club: %v", err)
	}

	// Default transfer policy: any bid below valuation is rejected. Place the
	// bid by the buyer side against our player; the bot rejects it.
	var bidID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO transfer.bids
			(world_id, player_id, bidding_club_id, selling_club_id, fee, weekly_wage,
			 contract_length_months, signing_bonus, proposed_by, status, created_at)
		VALUES ($1, $2, $3, $4, 1, 500, 24, 0, 'buying_club', 'pending', now())
		RETURNING id`, worldID, playerID, buyerClub, clubID).Scan(&bidID); err != nil {
		t.Fatalf("insert bid: %v", err)
	}

	if err := svc.RespondToBidsForAbsent(ctx, worldID); err != nil {
		t.Fatalf("respond bids: %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM transfer.bids WHERE id = $1`, bidID).Scan(&status); err != nil {
		t.Fatalf("load bid status: %v", err)
	}
	if status == "pending" {
		t.Fatalf("expected the bot to respond to the pending bid (status=%q)", status)
	}
}
