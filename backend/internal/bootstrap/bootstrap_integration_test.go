//go:build integration

package bootstrap

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/playerpool"
	"github.com/touchline/backend/internal/scheduler"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

func TestBootstrapWorldCreatesMaterial(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "boot-material")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}

	svc := NewService(pool, nil)
	res, err := svc.BootstrapWorld(ctx, w.ID, "Harbour City FC", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if res.SquadSize != SquadSizeDefault {
		t.Fatalf("squad size = %d, want %d", res.SquadSize, SquadSizeDefault)
	}
	if res.ManagerID == uuid.Nil || res.ClubID == uuid.Nil || res.RandomSeed == 0 {
		t.Fatalf("bootstrap result incomplete: %+v", res)
	}

	// Club row: AI-controlled, wired to its policy-bot manager.
	var (
		clubName string
		isAI     bool
		botID    uuid.UUID
		botBot   bool
	)
	if err := pool.QueryRow(ctx, `
		SELECT c.name, c.is_ai_controlled, m.id, m.is_policy_bot
		FROM club.clubs c JOIN manager.managers m ON m.id = c.current_manager_id
		WHERE c.id = $1`, res.ClubID).Scan(&clubName, &isAI, &botID, &botBot); err != nil {
		t.Fatalf("load club+manager: %v", err)
	}
	if clubName != "Harbour City FC" || !isAI || !botBot || botID != res.ManagerID {
		t.Fatalf("club/manager mismatch: name=%q isAI=%v bot=%v manager=%v", clubName, isAI, botBot, botID)
	}

	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM player.players WHERE club_id = $1`, res.ClubID,
	).Scan(&playersCount); err != nil {
		t.Fatalf("count players: %v", err)
	}
	if playersCount != SquadSizeDefault {
		t.Fatalf("players = %d, want %d", playersCount, SquadSizeDefault)
	}

	// Every squad member carries a full football profile: attribute EAV,
	// hidden traits, personality, and the initial emotional state — all
	// persisted inside the same club-creation transaction.
	var attrRows, traitsRows, personalityRows, emotionRows int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM player.player_attributes pa JOIN player.players p ON p.id = pa.player_id WHERE p.club_id = $1),
			(SELECT COUNT(*) FROM player.player_hidden_traits ht JOIN player.players p ON p.id = ht.player_id WHERE p.club_id = $1),
			(SELECT COUNT(*) FROM player.player_personality pp JOIN player.players p ON p.id = pp.player_id WHERE p.club_id = $1),
			(SELECT COUNT(*) FROM player.player_emotional_states es JOIN player.players p ON p.id = es.player_id WHERE p.club_id = $1)`,
		res.ClubID).Scan(&attrRows, &traitsRows, &personalityRows, &emotionRows); err != nil {
		t.Fatalf("count profile rows: %v", err)
	}
	if traitsRows != SquadSizeDefault || personalityRows != SquadSizeDefault || emotionRows != SquadSizeDefault {
		t.Fatalf("profile rows: traits %d personality %d emotions %d, want %d each",
			traitsRows, personalityRows, emotionRows, SquadSizeDefault)
	}
	// Outfielders carry 5 categories (~37 keys), GKs carry 5 with the
	// goalkeeping set instead of technical: 24 players is easily 100+ rows.
	if attrRows < SquadSizeDefault*4 {
		t.Fatalf("attribute rows %d, want >= %d", attrRows, SquadSizeDefault*4)
	}

	var people int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM person.people WHERE world_id = $1`, w.ID,
	).Scan(&people); err != nil {
		t.Fatalf("count people: %v", err)
	}
	// Bootstrap seeds a world-level free-agent pool (PoolTargetSize) plus the
	// 24 drafted squad members are re-seeded by the replenish step: total =
	// PoolTargetSize + SquadSizeDefault.
	if people != playerpool.PoolTargetSize+SquadSizeDefault {
		t.Fatalf("people = %d, want %d", people, playerpool.PoolTargetSize+SquadSizeDefault)
	}

	// The world-level free-agent pool stays at its target after the starter
	// drafted from it.
	var freeAgents int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM player.players
		WHERE world_id = $1 AND club_id IS NULL AND status = 'free_agent' AND country_id IS NULL`,
		w.ID).Scan(&freeAgents); err != nil {
		t.Fatalf("count free agents: %v", err)
	}
	if freeAgents != playerpool.PoolTargetSize {
		t.Fatalf("free agents = %d, want %d", freeAgents, playerpool.PoolTargetSize)
	}

	var badPositions int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM player.players WHERE club_id = $1
		AND primary_position NOT IN ('GK','CB','LB','RB','DM','CM','AM','LM','RM','LW','RW','ST')`,
		res.ClubID).Scan(&badPositions); err != nil {
		t.Fatalf("count bad positions: %v", err)
	}
	if badPositions != 0 {
		t.Fatalf("players with invalid positions = %d", badPositions)
	}

	gkCount := 0
	for _, p := range res.Players {
		if p.PrimaryPosition == "GK" {
			gkCount++
		}
	}
	if gkCount < 2 {
		t.Fatalf("GK count = %d, want >= 2", gkCount)
	}

	var worldRef time.Time
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(launched_at, created_at) FROM world.worlds WHERE id = $1`, w.ID).Scan(&worldRef); err != nil {
		t.Fatalf("world ref: %v", err)
	}
	for _, p := range res.Players {
		want := daysTruncate(worldRef).AddDate(-p.Age, 0, 0)
		if !p.DateOfBirth.Equal(want) {
			t.Fatalf("player %s dob = %v, want %v", p.DisplayName, p.DateOfBirth, want)
		}
	}

	// Auditable events: CLUB_CREATED + WORLD_BOOTSTRAPPED carry the seed.
	var bootEvents int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type IN ('CLUB_CREATED','WORLD_BOOTSTRAPPED') AND actor_type = 'system'`,
		w.ID).Scan(&bootEvents); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if bootEvents != 2 {
		t.Fatalf("bootstrap events = %d, want 2", bootEvents)
	}

	var eventSeed int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(random_seed, 0) FROM world.events
		WHERE world_id = $1 AND event_type = 'WORLD_BOOTSTRAPPED'`, w.ID).Scan(&eventSeed); err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if eventSeed != res.RandomSeed {
		t.Fatalf("event seed = %d, want %d", eventSeed, res.RandomSeed)
	}
}

var playersCount int

func TestBootstrapWorldGuards(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()
	worldSvc := internalworld.NewService(pool, nil)
	svc := NewService(pool, nil)

	// Already-launched worlds reject bootstrap.
	active := testdb.CreateWorld(t, pool, "boot-active") // status active
	if _, err := svc.BootstrapWorld(ctx, active, "X FC", ""); err != ErrWorldNotProvisioning {
		t.Fatalf("bootstrap active = %v, want ErrWorldNotProvisioning", err)
	}

	// A provisioning world bootstraps once; a second run is a conflict.
	w, err := worldSvc.CreateWorld(ctx, "boot-single")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	if _, err := svc.BootstrapWorld(ctx, w.ID, "Once FC", ""); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if _, err := svc.BootstrapWorld(ctx, w.ID, "Twice FC", ""); err != ErrAlreadyBootstrapped {
		t.Fatalf("second bootstrap = %v, want ErrAlreadyBootstrapped", err)
	}

	// Missing world -> not found.
	if _, err := svc.BootstrapWorld(ctx, uuid.New(), "Ghost FC", ""); err != ErrWorldNotFound {
		t.Fatalf("bootstrap missing world = %v, want ErrWorldNotFound", err)
	}
}

func TestBootstrapWorldDeterministic(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()
	worldSvc := internalworld.NewService(pool, nil)

	wA, err := worldSvc.CreateWorld(ctx, "boot-det-a")
	if err != nil {
		t.Fatalf("create world a: %v", err)
	}
	wB, err := worldSvc.CreateWorld(ctx, "boot-det-b")
	if err != nil {
		t.Fatalf("create world b: %v", err)
	}

	rA, err := newServiceWith(pool, nil, rand.New(rand.NewSource(4321))).BootstrapWorld(ctx, wA.ID, "A FC", "")
	if err != nil {
		t.Fatalf("bootstrap a: %v", err)
	}
	rB, err := newServiceWith(pool, nil, rand.New(rand.NewSource(4321))).BootstrapWorld(ctx, wB.ID, "B FC", "")
	if err != nil {
		t.Fatalf("bootstrap b: %v", err)
	}

	if rA.RandomSeed != rB.RandomSeed {
		t.Fatalf("seeds differ: %d vs %d", rA.RandomSeed, rB.RandomSeed)
	}
	if len(rA.Players) != len(rB.Players) {
		t.Fatalf("squad sizes differ")
	}
	for i := range rA.Players {
		a, b := rA.Players[i], rB.Players[i]
		if a.FirstName != b.FirstName || a.LastName != b.LastName ||
			a.PrimaryPosition != b.PrimaryPosition || a.Age != b.Age {
			t.Fatalf("player %d differs under the same seed: %+v vs %+v", i, a, b)
		}
	}
}

// TestBootstrappedWorldReceivesDailyTick proves AC5: a bootstrapped world can
// be launched and immediately driven by the S02-03 world clock.
func TestBootstrappedWorldReceivesDailyTick(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()
	worldSvc := internalworld.NewService(pool, nil)

	w, err := worldSvc.CreateWorld(ctx, "boot-ticking")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Ticking Town", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if err := scheduler.NewService(pool, nil).FireTick(ctx, w.ID, "daily"); err != nil {
		t.Fatalf("fire daily tick: %v", err)
	}

	var tick int64
	if err := pool.QueryRow(ctx, `SELECT current_tick FROM world.worlds WHERE id = $1`, w.ID).Scan(&tick); err != nil {
		t.Fatalf("read tick: %v", err)
	}
	if tick != 1 {
		t.Fatalf("current_tick = %d, want 1", tick)
	}

	var events int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = 'WORLD_TICK' AND world_tick = 1`, w.ID).Scan(&events); err != nil {
		t.Fatalf("count tick events: %v", err)
	}
	if events != 1 {
		t.Fatalf("WORLD_TICK events = %d, want 1", events)
	}

	// The tick must reference the bootstrapped club's squad domain: at least
	// one generated player is present for the world.
	var players int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM player.players WHERE club_id = $1`, res.ClubID).Scan(&players); err != nil {
		t.Fatalf("count players: %v", err)
	}
	if players != SquadSizeDefault {
		t.Fatalf("players = %d, want %d", players, SquadSizeDefault)
	}
}
