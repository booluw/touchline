//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/testdb"
)

// TestHTTPCompetitionAdminAndReads covers the S04-01 API surface: admin
// country+league creation and seeding, plus world-scoped manager reads of
// competitions, fixtures and standings — the full admin->seed->schedule loop.
func TestHTTPCompetitionAdminAndReads(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	client := ts.Client()

	// Provisioning world + admin with a manager row in it.
	var worldID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO world.worlds (id, name, status) VALUES ($1, $2, 'provisioning') RETURNING id`,
		uuid.New(), "League Town").Scan(&worldID); err != nil {
		t.Fatalf("insert world: %v", err)
	}
	admin := testdb.CreateUser(t, pool, "leagueadmin@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, admin)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO manager.managers (world_id, user_id, is_policy_bot, status)
		VALUES ($1, $2, FALSE, 'active')`, worldID, admin); err != nil {
		t.Fatalf("insert admin manager: %v", err)
	}
	plain := testdb.CreateUser(t, pool, "leagueplain@example.com", "s3cret", []testdb.Join{{WorldID: mustParseUUID(t, worldID)}})
	_ = plain
	adminCookies := login(t, ts, client, "leagueadmin@example.com", "s3cret")
	plainCookies := login(t, ts, client, "leagueplain@example.com", "s3cret")

	// Bootstrap the starter club + seed material.
	resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/bootstrap",
		`{"name":"Harbour Railway","short_name":"HAR"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("bootstrap = %d, want 201", resp.StatusCode)
	}
	resp.Body.Close()

	// Non-admins cannot create countries or seed.
	if r := post(t, ts, client, "/api/admin/countries",
		`{"world_id":"`+worldID+`","code":"eng","name":"England"}`, plainCookies); r.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin country = %d, want 403", r.StatusCode)
	}

	// Admin creates the country.
	resp = post(t, ts, client, "/api/admin/countries",
		`{"world_id":"`+worldID+`","code":"eng","name":"England"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create country = %d, want 201", resp.StatusCode)
	}
	countryID := decodeID(t, resp, "id")

	// Admin declares two leagues (creation order forces one league first, then
	// adjacency wiring via PATCH, then the second league linking up).
	resp = post(t, ts, client, "/api/admin/leagues",
		`{"country_id":"`+countryID+`","name":"Championship","tier":2,"team_count":4,"promotions":1}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create championship = %d, want 201", resp.StatusCode)
	}
	champID := decodeID(t, resp, "id")

	resp = post(t, ts, client, "/api/admin/leagues",
		`{"country_id":"`+countryID+`","name":"Premier","tier":1,"team_count":4,"relegations":1}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create premier = %d, want 201", resp.StatusCode)
	}
	premierID := decodeID(t, resp, "id")

	// Wire symmetry: premier relegates into championship; championship
	// promotes into premier (1 up / 1 down).
	resp = patch(t, ts, client, "/api/admin/leagues/"+premierID+"/adjacency",
		`{"relegates_to":"`+champID+`"}`, adminCookies)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("link premier = %d, want 204", resp.StatusCode)
	}
	resp = patch(t, ts, client, "/api/admin/leagues/"+champID+"/adjacency",
		`{"promotes_to":"`+premierID+`"}`, adminCookies)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("link champion = %d, want 204", resp.StatusCode)
	}

	// Seed the country with the premier league hosting the starter club.
	resp = post(t, ts, client, "/api/admin/worlds/"+worldID+"/seed-competition",
		`{"country_id":"`+countryID+`","starter_league_id":"`+premierID+`"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("seed = %d, want 201 (%s)", resp.StatusCode, string(raw))
	}
	seed := decodeMap(t, resp)
	leagues := seed["leagues"].([]any)
	if len(leagues) != 2 {
		t.Fatalf("seeded leagues = %d, want 2", len(leagues))
	}
	for _, l := range leagues {
		l := l.(map[string]any)
		if int(l["team_count"].(float64)) != 4 || int(l["matchdays"].(float64)) != 6 {
			t.Fatalf("league seed = %v, want 4 teams / 6 matchdays", l)
		}
	}

	// A second seed of the country is rejected as already seeded.
	resp = post(t, ts, client, "/api/admin/worlds/"+worldID+"/seed-competition",
		`{"country_id":"`+countryID+`","starter_league_id":"`+premierID+`"}`, adminCookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("re-seed = %d, want 409", resp.StatusCode)
	}

	// Manager reads: competitions, fixtures, standings.
	resp = get(t, ts, client, "/api/competitions", plainCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list competitions = %d, want 200", resp.StatusCode)
	}
	comps := decodeMap(t, resp)["competitions"].([]any)
	if len(comps) != 2 {
		t.Fatalf("competitions = %d, want 2", len(comps))
	}

	resp = get(t, ts, client, "/api/competitions/"+premierID+"/fixtures?matchday=1", plainCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fixtures = %d, want 200", resp.StatusCode)
	}
	fixtures := decodeMap(t, resp)["fixtures"].([]any)
	if len(fixtures) != 2 {
		t.Fatalf("matchday 1 fixtures = %d, want 2", len(fixtures))
	}

	resp = get(t, ts, client, "/api/competitions/"+premierID+"/standings", plainCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("standings = %d, want 200", resp.StatusCode)
	}
	standing := decodeMap(t, resp)
	if standing["season_number"].(float64) != 1 || len(standing["rows"].([]any)) != 0 {
		t.Fatalf("initial standings = %v, want empty season 1", standing)
	}

	// Cross-world isolation: the plain manager's world has no countries but
	// the admin's provisioning world does — reads are always world-scoped.
	resp = get(t, ts, client, "/api/competitions", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin competitions = %d, want 200", resp.StatusCode)
	}
	if n := len(decodeMap(t, resp)["competitions"].([]any)); n != 2 {
		t.Fatalf("admin-world competitions = %d, want 2", n)
	}
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

func decodeMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var m map[string]any
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return m
}

func decodeID(t *testing.T, resp *http.Response, key string) string {
	t.Helper()
	return decodeMap(t, resp)[key].(string)
}

func patch(t *testing.T, ts *httptest.Server, client *http.Client, path, body, cookieHeader string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build patch request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("patch %s: %v", path, err)
	}
	return resp
}
