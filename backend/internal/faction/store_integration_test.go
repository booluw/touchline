//go:build integration

package faction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// factionFixture boots one active world with a human-owned club and returns the
// pool, world id, club id, owner manager id and the club's active players.
func factionFixture(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID, []uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, "faction-dynamics")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := internalbootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Harbour Dynamics FC", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	userID := testdb.CreateUser(t, pool, "faction-owner@example.com", "s3cret", nil)
	if _, err := pool.Exec(ctx, `
		UPDATE manager.managers
		SET user_id = $1, is_policy_bot = FALSE
		WHERE current_club_id = $2 AND status = 'active'`, userID, res.ClubID); err != nil {
		t.Fatalf("owner manager: %v", err)
	}
	var managerID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`,
		res.ClubID).Scan(&managerID); err != nil {
		t.Fatalf("load owner: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id`, res.ClubID)
	if err != nil {
		t.Fatalf("load players: %v", err)
	}
	defer rows.Close()
	var players []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		players = append(players, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate players: %v", err)
	}
	if len(players) < 3 {
		t.Fatalf("club has %d players, want ≥ 3", len(players))
	}
	return pool, w.ID, res.ClubID, managerID, players
}

// seedClique inserts strong friendship edges among every player pair so the
// graph is guaranteed connected (test-deterministic unrest).
func seedClique(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, players []uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < len(players); i++ {
		for j := i + 1; j < len(players); j++ {
			a, b := players[i].String(), players[j].String()
			if a > b {
				a, b = b, a
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO social.relationships
					(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type,
					 relationship_type, strength, trust, sentiment, last_interaction_at)
				VALUES ($1, $2, 'player', $3, 'player', 'friendship', 60, 40, 60, now())
				ON CONFLICT (entity_a_id, entity_b_id, relationship_type) DO NOTHING`,
				worldID, a, b); err != nil {
				t.Fatalf("seed clique edge: %v", err)
			}
		}
	}
}

// TestGetDynamicsGeneratesIdempotentCanonicalGraph: reading generates the graph
// once, is idempotent, keeps edges canonical (a<b) and returns a well-formed
// read model bounded to the documented ranges.
func TestGetDynamicsGeneratesIdempotentCanonicalGraph(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, managerID, players := factionFixture(t)
	svc := NewService(pool, nil)

	first, err := svc.GetDynamics(ctx, worldID, managerID, clubID)
	if err != nil {
		t.Fatalf("get dynamics: %v", err)
	}
	if len(first.Tiers) == 0 || len(first.Tiers) > len(players) {
		t.Fatalf("tiers = %d, want between 1 and %d (influencers only)", len(first.Tiers), len(players))
	}
	for i, tv := range first.Tiers {
		if tv.Tier == TierOther {
			t.Fatalf("tier %d must not be 'other' (influencers only), got %s", i, tv.Tier)
		}
		if tv.Profile == nil {
			t.Fatalf("tier %d (%s) must carry a full player profile", i, tv.PlayerID)
		}
		if tv.Profile.Overall < 1 || tv.Profile.Overall > 99 {
			t.Fatalf("tier %d (%s) overall = %d out of [1,99]", i, tv.PlayerID, tv.Profile.Overall)
		}
		if tv.Profile.Name == "" || tv.Profile.Position == "" {
			t.Fatalf("tier %d (%s) profile missing identity: %+v", i, tv.PlayerID, tv.Profile)
		}
	}
	if first.Tiers[0].Tier != TierTeamLeader {
		t.Fatalf("highest-ranked tier must be team leader, got %s", first.Tiers[0].Tier)
	}
	if first.Cohesion < CohesionMin || first.Cohesion > CohesionMax {
		t.Fatalf("cohesion %d out of [%d,%d]", first.Cohesion, CohesionMin, CohesionMax)
	}
	if first.ManagerSupport < 0 || first.ManagerSupport > 100 {
		t.Fatalf("manager support %d out of [0,100]", first.ManagerSupport)
	}
	if first.DressingRoomMood < 0 || first.DressingRoomMood > 100 {
		t.Fatalf("dressing-room mood %d out of [0,100]", first.DressingRoomMood)
	}

	var before int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'player' AND entity_b_type = 'player'`,
		worldID).Scan(&before); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if before == 0 {
		t.Fatalf("reading must generate player↔player edges")
	}
	if _, err := svc.GetDynamics(ctx, worldID, managerID, clubID); err != nil {
		t.Fatalf("second dynamics read: %v", err)
	}
	var after int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'player' AND entity_b_type = 'player'`,
		worldID).Scan(&after); err != nil {
		t.Fatalf("recount edges: %v", err)
	}
	if before != after {
		t.Fatalf("generation must be idempotent: %d → %d", before, after)
	}

	var nonCanonical int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'player' AND entity_b_type = 'player'
		  AND entity_a_id >= entity_b_id`, worldID).Scan(&nonCanonical); err != nil {
		t.Fatalf("canonical check: %v", err)
	}
	if nonCanonical != 0 {
		t.Fatalf("%d player edges are not canonically oriented", nonCanonical)
	}
}

// TestGetDynamicsSecondReadIsNoOp: once the persisted graph matches the squad
// fingerprint, a steady-state read skips the rewrite entirely — the fingerprint
// rows stay put and no edge's last_interaction_at is touched, so the ~1100-edge
// batch write that used to dominate GET /dynamics latency is paid once.
func TestGetDynamicsSecondReadIsNoOp(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, managerID, _ := factionFixture(t)
	svc := NewService(pool, nil)

	if _, err := svc.GetDynamics(ctx, worldID, managerID, clubID); err != nil {
		t.Fatalf("first dynamics read: %v", err)
	}
	var stamp time.Time
	if err := pool.QueryRow(ctx, `
		SELECT MAX(last_interaction_at) FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'player' AND entity_b_type = 'player'`,
		worldID).Scan(&stamp); err != nil {
		t.Fatalf("max last_interaction_at: %v", err)
	}
	var before, afterHash int64
	if err := pool.QueryRow(ctx, `
		SELECT member_hash FROM social.squad_graph_state WHERE world_id = $1 AND club_id = $2`,
		worldID, clubID).Scan(&before); err != nil {
		t.Fatalf("squad graph state after first read: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)

	if _, err := svc.GetDynamics(ctx, worldID, managerID, clubID); err != nil {
		t.Fatalf("second dynamics read: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT member_hash FROM social.squad_graph_state WHERE world_id = $1 AND club_id = $2`,
		worldID, clubID).Scan(&afterHash); err != nil {
		t.Fatalf("squad graph state after second read: %v", err)
	}
	if afterHash != before {
		t.Fatalf("fingerprint changed %d → %d; second read must be a no-op", before, afterHash)
	}
	var afterStamp time.Time
	if err := pool.QueryRow(ctx, `
		SELECT MAX(last_interaction_at) FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'player' AND entity_b_type = 'player'`,
		worldID).Scan(&afterStamp); err != nil {
		t.Fatalf("max last_interaction_at after: %v", err)
	}
	if afterStamp.After(stamp) {
		t.Fatalf("second read rewrote edges (%v → %v); fingerprint must suppress the rewrite", stamp, afterStamp)
	}
}

// TestGetDynamicsOwnershipGate: a manager who does not hold the club is denied.
func TestGetDynamicsOwnershipGate(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, _, _ := factionFixture(t)
	svc := NewService(pool, nil)
	if _, err := svc.GetDynamics(ctx, worldID, uuid.New(), clubID); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("foreign dynamics err = %v, want ErrNotOwned", err)
	}
}

// TestOnPlayerSoldEmitsUnrestAndFormerTeammates: a sale from a connected room
// records former-teammate bonds and, because contagion saturates, emits a
// well-formed SQUAD_UNREST_TRIGGERED that the read model then reflects.
func TestOnPlayerSoldEmitsUnrestAndFormerTeammates(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, managerID, players := factionFixture(t)
	svc := NewService(pool, nil)
	seedClique(t, pool, worldID, players)

	sold := players[0]
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := svc.OnPlayerSold(ctx, tx, worldID, 500, clubID, sold); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("on player sold: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var former int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM social.relationships
		WHERE world_id = $1 AND relationship_type = 'former_teammate'
		  AND (entity_a_id = $2 OR entity_b_id = $2)`, worldID, sold).Scan(&former); err != nil {
		t.Fatalf("count former teammates: %v", err)
	}
	if former == 0 {
		t.Fatalf("sale must record former-teammate bonds")
	}

	var eventType string
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT event_type, payload FROM world.events
		WHERE world_id = $1 AND event_type = $2 ORDER BY occurred_at DESC LIMIT 1`,
		worldID, EventSquadUnrestTriggered).Scan(&eventType, &payload); err != nil {
		t.Fatalf("expected a %s event: %v", EventSquadUnrestTriggered, err)
	}

	dyn, err := svc.GetDynamics(ctx, worldID, managerID, clubID)
	if err != nil {
		t.Fatalf("get dynamics after sale: %v", err)
	}
	if dyn.Unrest == nil {
		t.Fatalf("read model must surface the recent unrest")
	}
	if dyn.Unrest.Demand != DemandEnMasseTransferReqs {
		t.Fatalf("saturated contagion must demand en-masse transfer requests, got %q", dyn.Unrest.Demand)
	}
	if dyn.Unrest.Severity < EnMasseThreshold {
		t.Fatalf("severity %d below en-masse threshold %d", dyn.Unrest.Severity, EnMasseThreshold)
	}
}
