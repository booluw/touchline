//go:build integration

package social_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/social"
	"github.com/touchline/backend/internal/transfertest"
	"github.com/touchline/backend/pkg/realtime"
)

// newRivalryFixture provisions the standard world and, unless names says
// otherwise, pins both fixture clubs to known countries so the big-match
// predicate is deterministic.
func newRivalryFixture(t *testing.T, name string) (*social.Service, *pgxpool.Pool, transfertest.World) {
	t.Helper()
	svc, pool, w := newSocialFixture(t, name)
	return svc, pool, w
}

// seedRivalryFixture inserts a league competition for the world plus one
// fixture row between home and away. ht/at scores + completed_at are left to
// the caller so the recorder's strength math is what is under test.
func seedRivalryFixture(t *testing.T, pool *pgxpool.Pool, worldID, home, away uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var competitionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Rivalry Test League', 'league', 10, 0, 'active') RETURNING id`, worldID).Scan(&competitionID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	var fixtureID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $4, 1, now(), 'completed') RETURNING id`,
		worldID, competitionID, home, away).Scan(&fixtureID); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	return fixtureID
}

// setClubCountry pins a club's country so the big-match predicate is controlled.
func setClubCountry(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID, country string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE club.clubs SET country = $2 WHERE id = $1`, clubID, country); err != nil {
		t.Fatalf("set club country: %v", err)
	}
}

// makeHuman turns an AI-club manager into a human one (no user row, but the
// bot flag is what the recorder checks).
func makeHuman(t *testing.T, pool *pgxpool.Pool, managerID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE manager.managers SET is_policy_bot = FALSE WHERE id = $1`, managerID); err != nil {
		t.Fatalf("promote manager: %v", err)
	}
}

// recordMatch runs the tx-scoped recorder with the caller's completedAt.
func recordMatch(t *testing.T, svc *social.Service, pool *pgxpool.Pool, worldID, fixtureID, home, away uuid.UUID, hs, as int, at time.Time) *social.RelationshipPush {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	push, err := svc.RecordCompletedMatch(ctx, tx, worldID, fixtureID, home, away, hs, as, at)
	if err != nil {
		t.Fatalf("record completed match: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return push
}

func countRivalryEdges(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) (int, error) {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*)::int FROM social.relationships
		WHERE world_id = $1 AND relationship_type = 'rivalry'`, worldID).Scan(&n)
	return n, err
}

func TestRecordCompletedMatchClubEdgeAndHumanTrust(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-basic")
	ctx := context.Background()
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AIOneClub, "france") // not a big match
	fixtureID := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AIOneClub)
	push := recordMatch(t, svc, pool, w.WorldID, fixtureID, w.HumanClub, w.AIOneClub, 2, 1, time.Now().UTC())

	// Club edge always grows; AI opponent means no manager edge.
	if len(push.Edges) != 1 {
		t.Fatalf("edges = %+v, want 1 (club only)", push.Edges)
	}
	if push.Edges[0].EntityAType != "club" || push.Edges[0].EntityBType != "club" {
		t.Errorf("edge = %+v, want club↔club", push.Edges[0])
	}
	// delta = (10 + 2·min(1,5)) × 1 = 12.
	if push.Edges[0].Strength != 12 {
		t.Errorf("strength = %d, want 12", push.Edges[0].Strength)
	}

	var st int
	if err := pool.QueryRow(ctx, `
		SELECT strength FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'club' AND entity_b_type = 'club' AND relationship_type = 'rivalry'`,
		w.WorldID).Scan(&st); err != nil {
		t.Fatalf("persisted strength: %v", err)
	}
	if st != 12 {
		t.Errorf("persisted strength = %d, want 12", st)
	}

	// The winning human manager gets +5; the AI loser gets none.
	var winTrust int
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events WHERE manager_id = $1`, w.HumanMgr).Scan(&winTrust); err != nil {
		t.Fatalf("human trust: %v", err)
	}
	if winTrust != social.TrustWin {
		t.Errorf("human trust = %d, want %d", winTrust, social.TrustWin)
	}
	var aiTrust int
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events WHERE manager_id = $1`, w.AIOneMgr).Scan(&aiTrust); err != nil {
		t.Fatalf("ai trust: %v", err)
	}
	if aiTrust != 0 {
		t.Errorf("ai trust = %d, want 0", aiTrust)
	}

	// The outbox event row exists (nil bus = log-only write).
	var eventType string
	if err := pool.QueryRow(ctx, `
		SELECT event_type FROM world.events
		WHERE world_id = $1 AND event_type = 'RELATIONSHIP_CHANGED' ORDER BY occurred_at DESC LIMIT 1`,
		w.WorldID).Scan(&eventType); err != nil {
		t.Fatalf("RELATIONSHIP_CHANGED row: %v", err)
	}
}

func TestRecordCompletedMatchHumanPairCreatesManagerEdge(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-human")
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AITwoClub, "england") // big match
	makeHuman(t, pool, w.AITwoMgr)
	fixtureID := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AITwoClub)
	ctx := context.Background()

	// Human loses 0-2: manager edge appears, both humans get trust deltas.
	push := recordMatch(t, svc, pool, w.WorldID, fixtureID, w.HumanClub, w.AITwoClub, 0, 2, time.Now().UTC())
	if len(push.Edges) != 2 {
		t.Fatalf("edges = %d, want 2 (club + manager)", len(push.Edges))
	}

	var mgrStrength int
	if err := pool.QueryRow(ctx, `
		SELECT strength FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'manager' AND entity_b_type = 'manager' AND relationship_type = 'rivalry'`,
		w.WorldID).Scan(&mgrStrength); err != nil {
		t.Fatalf("manager edge strength: %v", err)
	}
	// big match: (8 + 2·2) × 2 = 24.
	if mgrStrength != 24 {
		t.Errorf("manager strength = %d, want 24", mgrStrength)
	}

	var humanTrust, aiTrust int
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events WHERE manager_id = $1`, w.HumanMgr).Scan(&humanTrust); err != nil {
		t.Fatalf("human trust: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events WHERE manager_id = $1`, w.AITwoMgr).Scan(&aiTrust); err != nil {
		t.Fatalf("ai trust: %v", err)
	}
	if humanTrust != social.TrustLoss || aiTrust != social.TrustWin {
		t.Errorf("trust = human %d / ai %d, want %d / %d", humanTrust, aiTrust, social.TrustLoss, social.TrustWin)
	}
}

func TestRivalryStrengthAccumulatesAndRepeatsEscalate(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-accum")
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AITwoClub, "england")
	makeHuman(t, pool, w.AITwoMgr)

	fixture1 := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AITwoClub)
	recordMatch(t, svc, pool, w.WorldID, fixture1, w.HumanClub, w.AITwoClub, 1, 0, time.Now().UTC())
	// big match, gd 1: (10 + 2) · 2 = 24.

	fixture2 := seedRivalryFixture(t, pool, w.WorldID, w.AITwoClub, w.HumanClub)
	recordMatch(t, svc, pool, w.WorldID, fixture2, w.AITwoClub, w.HumanClub, 4, 1, time.Now().UTC())

	var st int
	if err := pool.QueryRow(context.Background(), `
		SELECT strength FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'club' AND entity_b_type = 'club' AND relationship_type = 'rivalry'`,
		w.WorldID).Scan(&st); err != nil {
		t.Fatalf("club strength: %v", err)
	}
	// gd 3 → (10 + 6) · 2 = 32, + repeat 3 = 35 on top of 24.
	if st != 24+32+social.RivalRepeatBonus {
		t.Errorf("club strength = %d, want %d", st, 24+32+social.RivalRepeatBonus)
	}
}

func TestRivalryStrengthClamps(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-clamp")
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AIOneClub, "england")
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		fixtureID := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AIOneClub)
		recordMatch(t, svc, pool, w.WorldID, fixtureID, w.HumanClub, w.AIOneClub, 5, 0, time.Now().UTC())
	}
	var st int
	if err := pool.QueryRow(ctx, `
		SELECT strength FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'club' AND entity_b_type = 'club' AND relationship_type = 'rivalry'`,
		w.WorldID).Scan(&st); err != nil {
		t.Fatalf("club strength: %v", err)
	}
	if st != social.RivalMaxStrength {
		t.Errorf("clamped strength = %d, want %d", st, social.RivalMaxStrength)
	}
}

func TestRivalryStrengthDecaysWhenStale(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-decay")
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AIOneClub, "france") // no big match
	ctx := context.Background()

	t0 := time.Now().UTC().Add(-30 * 24 * time.Hour)
	f1 := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AIOneClub)
	recordMatch(t, svc, pool, w.WorldID, f1, w.HumanClub, w.AIOneClub, 1, 0, t0) // 12

	// Second meeting 60 days after the first (outside the 45-day staleness).
	f2 := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AIOneClub)
	recordMatch(t, svc, pool, w.WorldID, f2, w.HumanClub, w.AIOneClub, 2, 0, t0.Add(60*24*time.Hour))

	var st int
	if err := pool.QueryRow(ctx, `
		SELECT strength FROM social.relationships
		WHERE world_id = $1 AND entity_a_type = 'club' AND entity_b_type = 'club' AND relationship_type = 'rivalry'`,
		w.WorldID).Scan(&st); err != nil {
		t.Fatalf("club strength: %v", err)
	}
	// Decay 12 → 12 − 12/5 = 10, then + (10 + 4) + repeat 3 = 27.
	if st != 10+14+social.RivalRepeatBonus {
		t.Errorf("decayed strength = %d, want %d", st, 10+14+social.RivalRepeatBonus)
	}
}

func TestRivalEdgesResolveFromEitherOrientation(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-oriented")
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AITwoClub, "france")
	makeHuman(t, pool, w.AITwoMgr)
	attachPerson(t, pool, w.WorldID, w.AITwoMgr, "Grace", "Hopper", "Grace Hopper")
	fixtureID := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AITwoClub)
	ctx := context.Background()
	recordMatch(t, svc, pool, w.WorldID, fixtureID, w.HumanClub, w.AITwoClub, 3, 1, time.Now().UTC())

	// Both managers see the same manager edge with the OTHER side resolved,
	// regardless of who happens to be entity_a in canonical storage.
	for _, m := range []struct {
		id   uuid.UUID
		peer uuid.UUID
	}{{w.HumanMgr, w.AITwoMgr}, {w.AITwoMgr, w.HumanMgr}} {
		rels, err := svc.ListRelationships(ctx, w.WorldID, m.id)
		if err != nil {
			t.Fatalf("list relationships for %s: %v", m.id, err)
		}
		found := false
		for _, e := range rels {
			if e.EntityType == "manager" && e.EntityID == m.peer {
				found = true
				if e.EntityName != "Grace Hopper" && e.EntityName != "Ada Lovelace" {
					t.Errorf("peer name = %q, want resolved manager name", e.EntityName)
				}
			}
		}
		if !found {
			t.Errorf("manager %s missing peer edge %s: %+v", m.id, m.peer, rels)
		}
	}

	// The club edge shows up for the human manager too (their active club).
	rels, err := svc.ListRelationships(ctx, w.WorldID, w.HumanMgr)
	if err != nil {
		t.Fatalf("list relationships: %v", err)
	}
	clubSeen := false
	for _, e := range rels {
		if e.EntityType == "club" && (e.EntityID == w.AITwoClub || e.EntityID == w.HumanClub) {
			clubSeen = true
		}
	}
	if !clubSeen {
		t.Errorf("human manager missing club edge: %+v", rels)
	}

	// Cross-world boundary.
	if _, err := svc.ListRelationships(ctx, uuid.New(), w.HumanMgr); !errors.Is(err, social.ErrManagerNotInWorld) {
		t.Fatalf("cross-world = %v, want ErrManagerNotInWorld", err)
	}
}

func TestPublishRelationshipChangeRealtime(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-realtime")
	broker := realtime.NewLocalBroker()
	svc.WithRealtime(broker)
	fixtureID := seedRivalryFixture(t, pool, w.WorldID, w.HumanClub, w.AIOneClub)
	push := recordMatch(t, svc, pool, w.WorldID, fixtureID, w.HumanClub, w.AIOneClub, 1, 0, time.Now().UTC())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan realtime.Event, 4)
	go func() { _ = broker.Subscribe(ctx, func(ev realtime.Event) { got <- ev }) }()
	<-broker.Ready()

	svc.PublishRelationshipChange(ctx, push)

	select {
	case ev := <-got:
		if ev.Type != realtime.EventRelationshipChange {
			t.Fatalf("event type = %q, want %q", ev.Type, realtime.EventRelationshipChange)
		}
		if ev.WorldID == nil || *ev.WorldID != w.WorldID {
			t.Fatalf("event world = %v, want %s", ev.WorldID, w.WorldID)
		}
		var back social.RelationshipPush
		if err := json.Unmarshal(ev.Payload, &back); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if back.FixtureID != fixtureID || len(back.Edges) != 1 || back.Edges[0].Strength != 12 {
			t.Errorf("push = %+v", back)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no relationship_change event received")
	}

	// Nil broker never fails the publish path.
	svc.WithRealtime(nil)
	svc.PublishRelationshipChange(ctx, push)
}

func TestReconcileRivalriesBackfillsCompletedFixtures(t *testing.T) {
	svc, pool, w := newRivalryFixture(t, "rivalry-reconcile")
	ctx := context.Background()
	setClubCountry(t, pool, w.HumanClub, "england")
	setClubCountry(t, pool, w.AITwoClub, "france")
	makeHuman(t, pool, w.AITwoMgr)

	// A completed fixture whose rivalry was never recorded (e.g. played before
	// the S06-04c hook shipped). The league must exist before the fixture.
	var compID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'Rivalry Backfill League', 'league', 10, 0, 'active') RETURNING id`, w.WorldID).Scan(&compID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}
	var fixtureID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status, ht_score, at_score, completed_at)
		VALUES ($1, $2, $3, $4, 1, now() - interval '4 days', 'completed', 2, 1, now() - interval '4 days')
		RETURNING id`, w.WorldID, compID, w.HumanClub, w.AITwoClub).Scan(&fixtureID); err != nil {
		t.Fatalf("seed completed fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match.matches (fixture_id, world_id, seed, engine_version, home_score, away_score, status, started_at, ended_at)
		VALUES ($1, $2, 123, 'test', 2, 1, 'completed', now() - interval '5 days', now() - interval '4 days')`,
		fixtureID, w.WorldID); err != nil {
		t.Fatalf("seed match row: %v", err)
	}

	before, err := countRivalryEdges(t, pool, w.WorldID)
	if err != nil {
		t.Fatalf("count before: %v", err)
	}
	if before != 0 {
		t.Fatalf("edges before reconcile = %d, want 0", before)
	}

	n, err := svc.ReconcileRivalries(ctx, w.WorldID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled = %d, want 1", n)
	}

	after, err := countRivalryEdges(t, pool, w.WorldID)
	if err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 2 {
		t.Errorf("edges after = %d, want 2 (club + human manager pair)", after)
	}
	// Human winner of a non-big match got +5 via the backfill.
	var humanTrust int
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta), 0)::int FROM social.trust_events WHERE manager_id = $1`, w.HumanMgr).Scan(&humanTrust); err != nil {
		t.Fatalf("backfilled trust: %v", err)
	}
	if humanTrust != social.TrustWin {
		t.Errorf("backfilled trust = %d, want %d", humanTrust, social.TrustWin)
	}

	// Idempotent: a second pass replays nothing.
	if n, err := svc.ReconcileRivalries(ctx, w.WorldID); err != nil {
		t.Fatalf("reconcile again: %v", err)
	} else if n != 0 {
		t.Errorf("second reconcile = %d, want 0", n)
	}
	if after2, err := countRivalryEdges(t, pool, w.WorldID); err != nil {
		t.Fatalf("count final: %v", err)
	} else if after2 != 2 {
		t.Errorf("edges after idempotent pass = %d, want 2", after2)
	}
}
