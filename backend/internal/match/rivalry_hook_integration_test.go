//go:build integration

package match

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	internalsocial "github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/realtime"
)

// TestPlayFixtureWiresSocialHook proves the S06-04c seam: finishing a fixture
// through the match service grows the rivalry edge, writes the trust deltas for
// a human manager, and publishes the best-effort relationship_change envelope
// after its commit — all in the same completion transaction.
func TestPlayFixtureWiresSocialHook(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()

	worldID, homeID, awayID := worldFor(t, pool, ctx, "match-rivalry-world", "Harbour FC", "Riverside Albion")
	fixtureID := insertFixture(t, pool, ctx, uuid.Nil, worldID, homeID, awayID, time.Date(2030, 6, 1, 15, 0, 0, 0, time.UTC))

	// Promote the home club's current manager to a human so the trust + manager
	// edge branches run, and pin the same country so the league is a big match.
	if _, err := pool.Exec(ctx, `
		UPDATE manager.managers SET is_policy_bot = FALSE
		WHERE id = (SELECT current_manager_id FROM club.clubs WHERE id = $1)`, homeID); err != nil {
		t.Fatalf("promote home manager: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE club.clubs SET country = 'england' WHERE id = ANY($1)`, []uuid.UUID{homeID, awayID}); err != nil {
		t.Fatalf("pin countries: %v", err)
	}

	broker := realtime.NewLocalBroker()
	socialSvc := internalsocial.NewService(pool, nil)
	socialSvc.WithRealtime(broker)
	svc := newMatchService(pool)
	svc.WithSocial(socialSvc)

	pubCtx, cancelPub := context.WithCancel(ctx)
	defer cancelPub()
	got := make(chan realtime.Event, 4)
	go func() { _ = broker.Subscribe(pubCtx, func(ev realtime.Event) { got <- ev }) }()
	<-broker.Ready()

	mr, err := svc.PlayFixture(ctx, fixtureID)
	if err != nil {
		t.Fatalf("play fixture: %v", err)
	}
	_ = mr

	// Club↔club rivalry edge exists with the canonical shape.
	var edges int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM social.relationships
		WHERE world_id = $1 AND relationship_type = 'rivalry'`, worldID).Scan(&edges); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if edges < 1 {
		t.Errorf("edges = %d, want ≥ 1 after play fixture", edges)
	}

	// The home manager (human, winner or loser — deterministic by seed) has a
	// +5/−5 trust row, never both.
	var homeTrust int
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events
		WHERE manager_id = (SELECT current_manager_id FROM club.clubs WHERE id = $1)`, homeID).Scan(&homeTrust); err != nil {
		t.Fatalf("home trust: %v", err)
	}
	if homeTrust != internalsocial.TrustWin && homeTrust != internalsocial.TrustLoss {
		t.Errorf("home trust = %d, want ±%d", homeTrust, internalsocial.TrustWin)
	}

	// The RELATIONSHIP_CHANGED outbox row landed with the fixture.
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM world.events
		WHERE world_id = $1 AND event_type = 'RELATIONSHIP_CHANGED'`, worldID).Scan(&n); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if n != 1 {
		t.Errorf("outbox rows = %d, want 1", n)
	}

	// The post-commit realtime push reached the sink.
	select {
	case ev := <-got:
		if ev.Type != realtime.EventRelationshipChange {
			t.Fatalf("event type = %q, want %q", ev.Type, realtime.EventRelationshipChange)
		}
		if ev.WorldID == nil || *ev.WorldID != worldID {
			t.Errorf("event world = %v, want %s", ev.WorldID, worldID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no relationship_change event after finalize")
	}
}
