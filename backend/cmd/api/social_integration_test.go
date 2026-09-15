//go:build integration

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
)

// newSocialHTTPServer returns a test HTTP server plus the viewer manager (the
// logged-in persona) and target manager (the profile subject): two human
// accounts with their own clubs in the same world, one completed 2-1 win over
// the viewer, career history, a trust event and one manager rivalry edge.
func newSocialHTTPServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, uuid.UUID, uuid.UUID) {
	t.Helper()
	ts, pool := testHTTPServer(t)
	ctx := context.Background()

	w := testdb.CreateWorld(t, pool, "W-SOCIAL-HTTP")
	viewerUser := testdb.CreateUser(t, pool, "viewer@example.com", "s3cret",
		[]testdb.Join{{WorldID: w, Employed: true}})
	targetUser := testdb.CreateUser(t, pool, "target@example.com", "s3cret",
		[]testdb.Join{{WorldID: w, Employed: true}})

	var viewerMgr, targetMgr, targetClub uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE user_id = $1`, viewerUser).Scan(&viewerMgr); err != nil {
		t.Fatalf("viewer manager: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id, current_club_id FROM manager.managers WHERE user_id = $1`, targetUser).Scan(&targetMgr, &targetClub); err != nil {
		t.Fatalf("target manager: %v", err)
	}

	var compID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type, reputation, prize_pool, status)
		VALUES ($1, 'HTTP Social League', 'league', 10, 0, 'active') RETURNING id`, w).Scan(&compID); err != nil {
		t.Fatalf("competition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status, ht_score, at_score, completed_at)
		VALUES ($1, $2, $3, $4, 1, now() - interval '2 days', 'completed', 2, 1, now() - interval '2 days')`,
		w, compID, targetClub, *viewerClubFor(t, pool, viewerMgr)); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO manager.manager_history (manager_id, world_id, club_id, role, start_date, end_date)
		VALUES ($1, $2, $3, 'manager', '2020-01-01', NULL)`, targetMgr, w, targetClub); err != nil {
		t.Fatalf("history: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO social.trust_events (manager_id, delta, reason)
		VALUES ($1, +10, 'http: transfer approved')`, targetMgr); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO social.relationships
			(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type, relationship_type, strength, trust, sentiment, last_interaction_at)
		VALUES ($1, $2, 'manager', $3, 'manager', 'rivalry', 30, 5, -10, now())`,
		w, targetMgr, viewerMgr); err != nil {
		t.Fatalf("relationship: %v", err)
	}

	return ts, pool, viewerMgr, targetMgr
}

func viewerClubFor(t *testing.T, pool *pgxpool.Pool, managerID uuid.UUID) *uuid.UUID {
	t.Helper()
	var clubID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT current_club_id FROM manager.managers WHERE id = $1`, managerID).Scan(&clubID); err != nil {
		t.Fatalf("viewer club: %v", err)
	}
	return &clubID
}

func TestHTTPManagerProfile(t *testing.T) {
	ts, pool, viewerMgr, targetMgr := newSocialHTTPServer(t)
	client := ts.Client()
	viewerCookies := loginManager(t, ts, pool, "viewer@example.com")

	// 401 without a session.
	if resp := get(t, ts, client, "/api/managers/"+targetMgr.String()+"/profile", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous profile = %d, want 401", resp.StatusCode)
	}

	// 200 with the assembled profile.
	resp := get(t, ts, client, "/api/managers/"+targetMgr.String()+"/profile", viewerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := decodeJSON(t, resp, &body); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if body["id"] != targetMgr.String() {
		t.Errorf("profile id = %v, want %s", body["id"], targetMgr)
	}
	active, ok := body["active_club"].(map[string]any)
	if !ok || active["id"] == "" {
		t.Errorf("active_club missing: %v", body["active_club"])
	}
	if career := body["career"].(map[string]any); career["matches"].(float64) != 1 {
		t.Errorf("career matches = %v, want 1", career["matches"])
	}
	if body["trust_score"].(float64) != 10 {
		t.Errorf("trust_score = %v, want 10", body["trust_score"])
	}
	h2h, _ := body["h2h_vs_viewer"].(map[string]any)
	if h2h == nil {
		t.Errorf("h2h missing for a pair that played")
	} else if h2h["wins"].(float64) != 1 {
		t.Errorf("h2h wins = %v, want 1", h2h["wins"])
	}
	if rivalries := body["rivalries"].([]any); len(rivalries) == 0 {
		t.Errorf("rivalries empty, want the seeded manager edge")
	}

	// Self profile also resolves (200, h2h absent).
	resp = get(t, ts, client, "/api/managers/"+viewerMgr.String()+"/profile", viewerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("self profile = %d, want 200", resp.StatusCode)
	}

	// Unknown ids 404; malformed id 400.
	if resp := get(t, ts, client, "/api/managers/"+uuid.New().String()+"/profile", viewerCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown profile = %d, want 404", resp.StatusCode)
	}
	if resp := get(t, ts, client, "/api/managers/not-a-uuid/profile", viewerCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed profile = %d, want 400", resp.StatusCode)
	}
}

func TestHTTPManagerProfileWorldIsolation(t *testing.T) {
	ts, pool, _, targetMgr := newSocialHTTPServer(t)
	client := ts.Client()

	// A session from a sibling world cannot read another world's manager:
	// the world boundary is enforced in the service and reads as 404.
	sibling := testdb.CreateWorld(t, pool, "W-SOCIAL-OTHER")
	testdb.CreateUser(t, pool, "sibling@example.com", "s3cret",
		[]testdb.Join{{WorldID: sibling, Employed: true}})
	siblingCookies := loginManager(t, ts, pool, "sibling@example.com")

	if resp := get(t, ts, client, "/api/managers/"+targetMgr.String()+"/profile", siblingCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-world profile = %d, want 404", resp.StatusCode)
	}

	// ...but the sibling can view their own profile in their own world.
	siblingSelf := get(t, ts, client, "/api/managers/me/offers", siblingCookies) // sanity: sibling session works
	if siblingSelf.StatusCode != http.StatusOK {
		t.Fatalf("sibling session sanity = %d, want 200", siblingSelf.StatusCode)
	}
	var selfID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = (SELECT id FROM auth.users WHERE email = 'sibling@example.com')`,
	).Scan(&selfID); err != nil {
		t.Fatalf("sibling manager id: %v", err)
	}
	if resp := get(t, ts, client, "/api/managers/"+selfID.String()+"/profile", siblingCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("sibling self profile = %d, want 200", resp.StatusCode)
	}
}
