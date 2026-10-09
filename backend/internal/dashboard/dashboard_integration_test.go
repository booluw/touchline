//go:build integration

package dashboard

import (
	"context"
	"encoding/json"
	"strings"
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

// TestDashboardSummaryCountsAndBoardFactors covers the IM38 additions: board
// items carry the stored explanation, the summary rail reports confidence
// change, morale, money and league, counts match the sections, and an unhappy
// player now surfaces (the morale query used a non-existent column before).
func TestDashboardSummaryCountsAndBoardFactors(t *testing.T) {
	pool, svc, worldID, clubID, managerID := dashboardWorld(t)
	ctx := context.Background()

	for _, s := range []struct {
		tick, total int
		exp         string
	}{
		{1, 40, `{}`},
		{2, 12, `{"subject":"board_confidence","score":12,"factors":[{"label":"league performance","delta":12}]}`},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO manager.job_security_snapshots
				(manager_id, club_id, world_tick, performance_score, expectations_score,
				 financial_score, board_relationship_score, club_dna_alignment_score,
				 supporter_sentiment_score, alternatives_score, total_score, explanation)
			VALUES ($1, $2, $3, 10, 10, 10, 10, 10, 10, 10, $4, $5::jsonb)`,
			managerID, clubID, s.tick, s.total, s.exp); err != nil {
			t.Fatalf("insert board snapshot tick %d: %v", s.tick, err)
		}
	}
	var unhappyID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		clubID).Scan(&unhappyID); err != nil {
		t.Fatalf("pick player: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, morale) VALUES ($1, 0.1)
		ON CONFLICT (player_id) DO UPDATE SET morale = 0.1`, unhappyID); err != nil {
		t.Fatalf("set morale: %v", err)
	}

	// An active league season with the club on the table (bootstrap creates
	// no league).
	if _, err := pool.Exec(ctx, `
		WITH c AS (
			INSERT INTO competition.competitions (world_id, name, competition_type)
			VALUES ($1, 'Second Division North', 'league') RETURNING id
		), s AS (
			INSERT INTO competition.seasons (world_id, competition_id, season_label, season_number, start_date, status)
			SELECT $1, id, '2026/27', 1, CURRENT_DATE, 'in_progress' FROM c RETURNING id
		), e AS (
			INSERT INTO competition.competition_entries (season_id, club_id) SELECT id, $2 FROM s
		)
		INSERT INTO competition.standings (season_id, club_id, played, won, points, goals_for)
		SELECT id, $2, 1, 1, 3, 2 FROM s`, worldID, clubID); err != nil {
		t.Fatalf("seed league: %v", err)
	}

	snap, err := svc.GetDashboard(ctx, worldID, managerID)
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
	}

	var board *Item
	for i := range snap.Urgent {
		if snap.Urgent[i].Category == CatBoard {
			board = &snap.Urgent[i]
		}
	}
	if board == nil || board.Explanation["subject"] != "board_confidence" {
		t.Fatalf("urgent board item explanation = %+v", board)
	}
	if !hasCategory(snap.Important, CatMorale) {
		t.Fatalf("expected an unhappy-player item, got %+v", snap.Important)
	}

	if snap.Counts.Urgent != len(snap.Urgent) || snap.Counts.Important != len(snap.Important) ||
		snap.Counts.Interesting != len(snap.Interesting) {
		t.Fatalf("counts %+v do not match sections", snap.Counts)
	}
	total := 0
	for _, n := range snap.Counts.ByCategory {
		total += n
	}
	if total != len(snap.Urgent)+len(snap.Important)+len(snap.Interesting) || snap.Counts.ByCategory[CatMorale] < 1 {
		t.Fatalf("by_category = %+v", snap.Counts.ByCategory)
	}

	if len(snap.Summary) != 1 || snap.Summary[0].ClubID != clubID {
		t.Fatalf("summary = %+v, want one entry for the club", snap.Summary)
	}
	sum := snap.Summary[0]
	if sum.Board == nil || sum.Board.Confidence != 12 || sum.Board.Change == nil || *sum.Board.Change != -28 {
		t.Fatalf("board summary = %+v, want 12 with change -28", sum.Board)
	}
	if sum.Morale == nil || sum.Morale.Unhappy < 1 || sum.Morale.Players == 0 ||
		sum.Morale.Average <= 0 || sum.Morale.Average >= 1 {
		t.Fatalf("morale summary = %+v", sum.Morale)
	}
	if sum.Finance == nil || sum.Finance.Cash <= 0 || sum.Finance.WeeklyWageBill <= 0 || sum.Finance.SeasonWageBudget == nil {
		t.Fatalf("finance summary = %+v", sum.Finance)
	}

	// The standings item names the league, not the season ("sit 3rd in the
	// Premier Division", never "in the 2026/27").
	if sum.League == nil || sum.League.CompetitionName != "Second Division North" {
		t.Fatalf("league summary = %+v, want a competition name", sum.League)
	}
	var standings *Item
	for i := range snap.Interesting {
		if snap.Interesting[i].Category == CatStandings {
			standings = &snap.Interesting[i]
		}
	}
	if standings == nil || !strings.HasSuffix(standings.Title, "in the "+sum.League.CompetitionName) {
		t.Fatalf("standings item = %+v, want title ending with the league name %q", standings, sum.League.CompetitionName)
	}
}
