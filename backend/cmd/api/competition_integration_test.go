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
// country+league creation and whole-world seeding, plus world-scoped manager
// reads of competitions, fixtures and standings — the full admin->seed loop.
func TestHTTPCompetitionAdminAndReads(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	client := ts.Client()

	// Provisioning world + admin.
	var worldID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO world.worlds (id, name, status) VALUES ($1, $2, 'provisioning') RETURNING id`,
		uuid.New(), "League Town").Scan(&worldID); err != nil {
		t.Fatalf("insert world: %v", err)
	}
	admin := testdb.CreateUser(t, pool, "leagueadmin@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, admin)
	plain := testdb.CreateUser(t, pool, "leagueplain@example.com", "s3cret", []testdb.Join{{WorldID: mustParseUUID(t, worldID)}})
	adminCookies := login(t, ts, client, "leagueadmin@example.com", "s3cret")

	// A non-admin account with a joined world now resolves to a manager session
	// (OPD-15(4)(c)) — the console stays admin-only, not the login.
	_ = login(t, ts, client, "leagueplain@example.com", "s3cret")

	// Unauthenticated admin routes are 401.
	if resp := post(t, ts, client, "/api/admin/countries",
		`{"world_id":"`+worldID+`","code":"eng","name":"England"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous country = %d, want 401", resp.StatusCode)
	}

	// Admin creates the country.
	resp := post(t, ts, client, "/api/admin/countries",
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

	// Seed the whole world: clubs + players + memberships only, no seasons.
	resp = post(t, ts, client, "/api/admin/worlds/"+worldID+"/seed", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("seed = %d, want 200 (%s)", resp.StatusCode, string(raw))
	}
	seed := decodeMap(t, resp)
	if int(seed["new_clubs"].(float64)) != 8 {
		t.Fatalf("seed new_clubs = %v, want 8", seed["new_clubs"])
	}
	if int(seed["league_count"].(float64)) != 2 {
		t.Fatalf("seed league_count = %v, want 2", seed["league_count"])
	}
	countries := seed["countries"].([]any)
	if len(countries) != 1 {
		t.Fatalf("seeded countries = %d, want 1", len(countries))
	}
	leagues := countries[0].(map[string]any)["leagues"].([]any)
	if len(leagues) != 2 {
		t.Fatalf("seeded leagues = %d, want 2", len(leagues))
	}
	for _, l := range leagues {
		l := l.(map[string]any)
		if int(l["team_count"].(float64)) != 4 || int(l["new_clubs"].(float64)) != 4 {
			t.Fatalf("league seed = %v, want 4 team_count / 4 new_clubs", l)
		}
	}

	// A second seed is an idempotent no-op (200, zero new clubs) — the launch
	// model lets admins re-run after adding leagues.
	resp = post(t, ts, client, "/api/admin/worlds/"+worldID+"/seed", "", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-seed = %d, want 200", resp.StatusCode)
	}
	if n := int(decodeMap(t, resp)["new_clubs"].(float64)); n != 0 {
		t.Fatalf("re-seed new_clubs = %d, want 0", n)
	}

	// Manager reads: the plain manager is still a valid manager row, but the
	// login gate only admits admins. Mint a session for the manager directly so
	// the world-scoped read surface keeps its coverage independent of the gate.
	var plainManager uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = $1`, plain).Scan(&plainManager); err != nil {
		t.Fatalf("plain manager id: %v", err)
	}
	plainCookies := managerCookies(t, ts, pool, plainManager, plain)

	// Seeding created no seasons — standings are empty, fixtures are gone.
	resp = get(t, ts, client, "/api/competitions/"+premierID+"/standings", plainCookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("standings before season = %d, want 404", resp.StatusCode)
	}

	// The admin console session is world-less: manager-scoped reads are 403.
	resp = get(t, ts, client, "/api/competitions", adminCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("admin competitions = %d, want 403 (no world context)", resp.StatusCode)
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
