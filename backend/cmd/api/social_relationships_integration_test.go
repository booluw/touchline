//go:build integration

package main

import (
	"net/http"
	"testing"

	"github.com/touchline/backend/internal/testdb"
)

func TestHTTPRelationships(t *testing.T) {
	ts, pool, viewerMgr, targetMgr := newSocialHTTPServer(t)
	client := ts.Client()
	viewerCookies := loginManager(t, ts, pool, "viewer@example.com")

	// 401 without a session.
	if resp := get(t, ts, client, "/api/relationships", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", resp.StatusCode)
	}

	// 200 with the caller's edge surface. newSocialHTTPServer seeds one
	// manager↔manager rivalry stored as entity_a = target (viewer is entity_b),
	// so this exercises the canonical two-sided read over HTTP too.
	resp := get(t, ts, client, "/api/relationships", viewerCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relationships = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := decodeJSON(t, resp, &body); err != nil {
		t.Fatalf("decode relationships: %v", err)
	}
	if body["world_id"] != mustWorldOf(t, pool, viewerMgr).String() {
		t.Errorf("world_id = %v, want the caller's world", body["world_id"])
	}
	edges, _ := body["edges"].([]any)
	if len(edges) == 0 {
		t.Fatalf("edges empty, want the seeded manager edge")
	}
	found := false
	for _, raw := range edges {
		e, _ := raw.(map[string]any)
		if e["entity_type"] == "manager" && e["entity_id"] == targetMgr.String() {
			found = true
			if e["strength"] != float64(30) {
				t.Errorf("edge strength = %v, want 30", e["strength"])
			}
		}
	}
	if !found {
		t.Errorf("viewer missing the seeded target edge (two-sided read failed): %+v", edges)
	}
}

func TestHTTPRelationshipsWorldIsolation(t *testing.T) {
	ts, pool, _, _ := newSocialHTTPServer(t)
	client := ts.Client()

	// The seeded world's caller is unauthenticated there — good. A session from
	// a sibling world resolves against its own world and gets its own (empty)
	// edge list: the endpoint can never leak another world's edges because the
	// caller's manager row IS the world boundary.
	sibling := testdb.CreateWorld(t, pool, "W-REL-OTHER")
	testdb.CreateUser(t, pool, "rel-sibling@example.com", "s3cret",
		[]testdb.Join{{WorldID: sibling, Employed: true}})
	siblingCookies := loginManager(t, ts, pool, "rel-sibling@example.com")

	resp := get(t, ts, client, "/api/relationships", siblingCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sibling relationships = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := decodeJSON(t, resp, &body); err != nil {
		t.Fatalf("decode sibling relationships: %v", err)
	}
	edges, _ := body["edges"].([]any)
	if len(edges) != 0 {
		t.Errorf("sibling edges = %+v, want empty (no fixtures in that world)", edges)
	}
}
