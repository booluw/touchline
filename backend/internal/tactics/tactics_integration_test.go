//go:build integration

package tactics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// tacticsWorld builds an active world with one user-managed club (full squad,
// owning active manager) and returns the pool, world id, club id, and manager id.
func tacticsWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "tactics-world")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	worldID := w.ID

	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, worldID, "Harbour Tactics FC", "")
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
	return pool, worldID, clubID, managerID
}

func squadPlayerIDs(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY squad_number LIMIT $2`,
		clubID, n)
	if err != nil {
		t.Fatalf("load players: %v", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate players: %v", err)
	}
	return out
}

func TestSetTacticsRoundTripsAndEvents(t *testing.T) {
	pool, worldID, clubID, managerID := tacticsWorld(t)
	svc := NewService(pool, nil, squad.NewStore(pool))
	ctx := context.Background()
	actor := Actor{ManagerID: managerID}

	if err := svc.SetTactics(ctx, actor, clubID, "possession", "3-2-4-1"); err != nil {
		t.Fatalf("set tactics: %v", err)
	}
	view, err := svc.GetTactics(ctx, clubID)
	if err != nil {
		t.Fatalf("get tactics: %v", err)
	}
	if view.Style != "possession" || view.Formation != "3-2-4-1" {
		t.Fatalf("view = %s/%s, want possession/3-2-4-1", view.Style, view.Formation)
	}
	if len(view.Allowed) != 2 || view.Allowed[0] != "4-3-3" {
		t.Fatalf("allowed formations = %v, want [4-3-3 3-2-4-1]", view.Allowed)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'TACTIC_SET'`, worldID).Scan(&count); err != nil {
		t.Fatalf("events: %v", err)
	}
	if count != 1 {
		t.Fatalf("TACTIC_SET events = %d, want 1", count)
	}
	var actorType string
	if err := pool.QueryRow(ctx, `
		SELECT actor_type FROM world.events WHERE world_id = $1 AND event_type = 'TACTIC_SET'`, worldID).Scan(&actorType); err != nil {
		t.Fatalf("event actor: %v", err)
	}
	if actorType != "manager" {
		t.Fatalf("event actor_type = %q, want manager", actorType)
	}
}

func TestSetTacticsValidation(t *testing.T) {
	pool, _, clubID, managerID := tacticsWorld(t)
	svc := NewService(pool, nil, squad.NewStore(pool))
	ctx := context.Background()
	actor := Actor{ManagerID: managerID}

	if err := svc.SetTactics(ctx, actor, clubID, "park_the_bus", ""); err != ErrInvalidStyle {
		t.Fatalf("invalid style err = %v, want ErrInvalidStyle", err)
	}
	if err := svc.SetTactics(ctx, actor, clubID, "possession", "4-4-2"); err != ErrInvalidFormation {
		t.Fatalf("formation outside style err = %v, want ErrInvalidFormation", err)
	}
	// style-only set resolves to the style's default formation.
	if err := svc.SetTactics(ctx, actor, clubID, "low_block", ""); err != nil {
		t.Fatalf("style-only set: %v", err)
	}
	view, err := svc.GetTactics(ctx, clubID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.Formation != "5-4-1" {
		t.Fatalf("default low_block formation = %s, want 5-4-1", view.Formation)
	}
}

func TestSetTacticsOwnershipAndWorld(t *testing.T) {
	pool, _, clubID, _ := tacticsWorld(t)
	svc := NewService(pool, nil, squad.NewStore(pool))
	ctx := context.Background()

	// A manager who owns a different club cannot set tactics.
	if err := svc.SetTactics(ctx, Actor{ManagerID: uuid.New()}, clubID, "balanced", ""); err != ErrNotOwned {
		t.Fatalf("ownership err = %v, want ErrNotOwned", err)
	}

	// A paused world rejects writes.
	if _, err := pool.Exec(ctx,
		`UPDATE world.worlds SET status = 'paused' WHERE id = (SELECT world_id FROM club.clubs WHERE id = $1)`, clubID); err != nil {
		t.Fatalf("pause world: %v", err)
	}
	view, err := testdbAuthManagerID(t, pool, clubID)
	if err != nil {
		t.Fatalf("load manager: %v", err)
	}
	if err := svc.SetTactics(ctx, Actor{ManagerID: view}, clubID, "balanced", ""); err != ErrWorldNotActive {
		t.Fatalf("paused world err = %v, want ErrWorldNotActive", err)
	}
}

func TestSetTacticsDeadlineWhenFixtureLive(t *testing.T) {
	pool, worldID, clubID, managerID := tacticsWorld(t)
	svc := NewService(pool, nil, squad.NewStore(pool))
	ctx := context.Background()

	insertFixture(t, pool, ctx, worldID, clubID, "live")
	if err := svc.SetTactics(ctx, Actor{ManagerID: managerID}, clubID, "gegenpress", ""); err != ErrFixtureLive {
		t.Fatalf("deadline err = %v, want ErrFixtureLive", err)
	}
}

func TestSetLineupRoundTripsAndValidates(t *testing.T) {
	pool, worldID, clubID, managerID := tacticsWorld(t)
	svc := NewService(pool, nil, squad.NewStore(pool))
	ctx := context.Background()
	actor := Actor{ManagerID: managerID}

	players := squadPlayerIDs(t, pool, clubID, 11)
	slots := make([]LineupInput, 0, 11)
	for i, pid := range players {
		slots = append(slots, LineupInput{Slot: i, PlayerID: pid})
	}
	if err := svc.SetLineup(ctx, actor, clubID, slots); err != nil {
		t.Fatalf("set lineup: %v", err)
	}

	view, err := svc.GetLineup(ctx, clubID)
	if err != nil {
		t.Fatalf("get lineup: %v", err)
	}
	if len(view.Slots) != 11 {
		t.Fatalf("lineup slots = %d, want 11", len(view.Slots))
	}
	for i, sv0 := range view.Slots {
		if sv0.Slot != i || sv0.Position == "" || sv0.PlayerID != players[i] {
			t.Fatalf("slot %d = %+v, want position and player %s", i, sv0, players[i])
		}
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'LINEUP_SAVED'`, worldID).Scan(&count); err != nil {
		t.Fatalf("events: %v", err)
	}
	if count != 1 {
		t.Fatalf("LINEUP_SAVED events = %d, want 1", count)
	}

	// Validation: fewer than 11 slots, duplicate player, out-of-range slot.
	if err := svc.SetLineup(ctx, actor, clubID, slots[:10]); err != ErrInvalidLineup {
		t.Fatalf("short lineup err = %v, want ErrInvalidLineup", err)
	}
	dup := []LineupInput{{Slot: 0, PlayerID: players[0]}, {Slot: 1, PlayerID: players[0]}}
	if err := svc.SetLineup(ctx, actor, clubID, dup); err != ErrInvalidLineup {
		t.Fatalf("duplicate err = %v, want ErrInvalidLineup", err)
	}
}

func TestSetLineupDeadlineWhenFixtureLive(t *testing.T) {
	pool, worldID, clubID, managerID := tacticsWorld(t)
	svc := NewService(pool, nil, squad.NewStore(pool))
	ctx := context.Background()

	insertFixture(t, pool, ctx, worldID, clubID, "live")
	players := squadPlayerIDs(t, pool, clubID, 11)
	slots := make([]LineupInput, 0, 11)
	for i, pid := range players {
		slots = append(slots, LineupInput{Slot: i, PlayerID: pid})
	}
	if err := svc.SetLineup(ctx, Actor{ManagerID: managerID}, clubID, slots); err != ErrFixtureLive {
		t.Fatalf("deadline err = %v, want ErrFixtureLive", err)
	}
}

// insertFixture writes a competition + one fixture with the given status so
// deadline tests can exercise the "already live" rejection.
func insertFixture(t *testing.T, pool *pgxpool.Pool, ctx context.Context, worldID, clubID uuid.UUID, status string) {
	t.Helper()
	var compID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Deadline Test League', 'league', 10, 0, 'active') RETURNING id`, worldID).Scan(&compID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	// The away side is a minimal unmanaged club (home ≠ away is enforced).
	var awayID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO club.clubs (world_id, name, short_name, country)
		VALUES ($1, 'Deadline Away FC', 'Deadline Away FC', 'testland') RETURNING id`, worldID).Scan(&awayID); err != nil {
		t.Fatalf("insert away club: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $4, 1, $5, $6)`,
		worldID, compID, clubID, awayID, time.Now(), status); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
}

// testdbAuthManagerID resolves the owning manager for a club (mirrors the
// match harness's load-manager query).
func testdbAuthManagerID(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`, clubID).Scan(&id)
	return id, err
}
