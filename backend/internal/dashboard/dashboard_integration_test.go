//go:build integration

package dashboard

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/realtime"
)

// dashboardWorld bootstraps a world with one managed club and a Service without
// realtime wiring.
func dashboardWorld(t *testing.T) (*pgxpool.Pool, *Service, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "dashboard-world")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Harbour Dash FC", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := internalworld.NewService(pool, nil).SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}
	managerID, err := NewStore(pool).ManagerForClub(ctx, w.ID, res.ClubID)
	if err != nil {
		t.Fatalf("manager for club: %v", err)
	}
	return pool, NewService(pool, nil), w.ID, res.ClubID, managerID
}

func TestDashboardEmptyForClublessManager(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	worldID := testdb.CreateWorld(t, pool, "dashboard-empty")
	userID := testdb.CreateUser(t, pool, "dashless@example.com", "secret", []testdb.Join{{WorldID: worldID}})
	ctx := context.Background()

	var managerID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM manager.managers WHERE user_id = $1`, userID).Scan(&managerID); err != nil {
		t.Fatalf("load manager: %v", err)
	}

	snap, err := NewService(pool, nil).GetDashboard(ctx, worldID, managerID)
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	if len(snap.Urgent) != 0 || len(snap.Important) != 0 || len(snap.Interesting) != 0 {
		t.Fatalf("expected empty sections, got %+v", snap)
	}
}

func TestDashboardSurfacesBoardAndExpiringContract(t *testing.T) {
	pool, svc, worldID, clubID, managerID := dashboardWorld(t)
	ctx := context.Background()

	// A critically low confidence snapshot -> urgent board item.
	if _, err := pool.Exec(ctx, `
		INSERT INTO manager.job_security_snapshots
			(manager_id, club_id, world_tick, performance_score, expectations_score,
			 financial_score, board_relationship_score, club_dna_alignment_score,
			 supporter_sentiment_score, alternatives_score, total_score, explanation)
		VALUES ($1, $2, 1, 10, 10, 10, 10, 10, 10, 10, 12, '{}'::jsonb)`,
		managerID, clubID); err != nil {
		t.Fatalf("insert board snapshot: %v", err)
	}

	// An expiring contract -> urgent contract item.
	if _, err := pool.Exec(ctx, `
		UPDATE player.contracts SET end_date = CURRENT_DATE + 5
		WHERE club_id = $1 AND status = 'active' AND id = (
			SELECT id FROM player.contracts WHERE club_id = $1 AND status = 'active'
			ORDER BY id LIMIT 1)`, clubID); err != nil {
		t.Fatalf("age contract: %v", err)
	}

	snap, err := svc.GetDashboard(ctx, worldID, managerID)
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}
	if !hasCategory(snap.Urgent, CatBoard) {
		t.Fatalf("expected an urgent board item, got %+v", snap.Urgent)
	}
	if !hasCategory(snap.Urgent, CatContracts) {
		t.Fatalf("expected an urgent contract item, got %+v", snap.Urgent)
	}
	for _, it := range snap.Urgent {
		if it.ID == "" || it.Title == "" || it.Priority != PriorityUrgent {
			t.Fatalf("malformed item: %+v", it)
		}
	}
}

// TestPushWorldDeltaEmitsDashboardUpdate exercises the worker sweep path with a
// real database and an in-process broker.
func TestPushWorldDeltaEmitsDashboardUpdate(t *testing.T) {
	pool, svc, worldID, clubID, managerID := dashboardWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := pool.Exec(ctx, `
		INSERT INTO manager.job_security_snapshots
			(manager_id, club_id, world_tick, performance_score, expectations_score,
			 financial_score, board_relationship_score, club_dna_alignment_score,
			 supporter_sentiment_score, alternatives_score, total_score, explanation)
		VALUES ($1, $2, 2, 5, 5, 5, 5, 5, 5, 5, 8, '{}'::jsonb)`,
		managerID, clubID); err != nil {
		t.Fatalf("insert board snapshot: %v", err)
	}

	broker := realtime.NewLocalBroker()
	svc.WithRealtime(broker)
	got := make(chan realtime.Event, 8)
	go func() { _ = broker.Subscribe(ctx, func(ev realtime.Event) { got <- ev }) }()
	select {
	case <-broker.Ready():
	case <-time.After(time.Second):
		t.Fatal("broker not ready")
	}

	if err := svc.PushWorldDelta(ctx, worldID); err != nil {
		t.Fatalf("push world delta: %v", err)
	}

	select {
	case ev := <-got:
		if ev.Type != realtime.EventDashboardUpdate {
			t.Fatalf("event type = %q", ev.Type)
		}
		var payload DashboardUpdatePayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		if len(payload.Items) == 0 {
			t.Fatal("expected pushed items")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a dashboard_update event")
	}
}

func hasCategory(items []Item, category string) bool {
	for _, it := range items {
		if it.Category == category {
			return true
		}
	}
	return false
}
