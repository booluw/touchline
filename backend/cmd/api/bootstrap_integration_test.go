//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/testdb"
)

// TestHTTPClubReads covers world-scoped club reads: the seeded world's clubs
// list and club detail (manager + squad) are visible to a manager of that
// world and sealed against a manager of another world.
func TestHTTPClubReads(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	client := ts.Client()

	admin := testdb.CreateUser(t, pool, "clubadmin@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, admin)

	// A provisioning world is created directly (the admin world-create route is
	// covered by TestHTTPAdminWorldLifecycle); the admin's manager row scopes
	// the club reads below.
	var worldID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO world.worlds (id, name, status) VALUES ($1, $2, 'provisioning') RETURNING id`,
		uuid.New(), "Club Town").Scan(&worldID); err != nil {
		t.Fatalf("insert provisioning world: %v", err)
	}
	var adminManager uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO manager.managers (world_id, user_id, is_policy_bot, status)
		VALUES ($1, $2, FALSE, 'active') RETURNING id`, worldID, admin).Scan(&adminManager); err != nil {
		t.Fatalf("insert admin manager row: %v", err)
	}

	adminCookies := login(t, ts, client, "clubadmin@example.com", "s3cret")
	// Manager-scoped reads need a real manager session (admins get world-less
	// console sessions under the login gate) — mint one for the admin's row.
	mgrCookies := managerCookies(t, ts, pool, adminManager, admin)

	// Declare a country + one league and seed the whole world (launch model).
	resp := post(t, ts, client, "/api/admin/countries",
		`{"world_id":"`+worldID+`","code":"eng","name":"England"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create country = %d, want 201", resp.StatusCode)
	}
	countryID := decodeID(t, resp, "id")
	resp = post(t, ts, client, "/api/admin/leagues",
		`{"country_id":"`+countryID+`","name":"League One","tier":1,"team_count":4}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create league = %d, want 201", resp.StatusCode)
	}
	resp = post(t, ts, client, "/api/admin/worlds/"+worldID+"/seed", "", adminCookies)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("seed = %d, want 202", resp.StatusCode)
	}
	status := waitForSeed(t, ts, client, adminCookies, worldID)
	if status["clubs"].(float64) != 4 {
		t.Fatalf("seed clubs = %v, want 4", status["clubs"])
	}
	if s := status["leagues"].(map[string]any)["seeded"].(float64); s != 1 {
		t.Fatalf("seed leagues seeded = %v, want 1", s)
	}

	// The manager's world lists exactly the seeded clubs.
	resp = get(t, ts, client, "/api/clubs", mgrCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list clubs = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var list map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode clubs: %v", err)
	}
	clubs, ok := list["clubs"].([]any)
	if !ok || len(clubs) != 4 {
		t.Fatalf("clubs = %v, want exactly 4", list["clubs"])
	}
	clubID := clubs[0].(map[string]any)["id"].(string)

	// Club detail exposes manager + 24-player squad for the same world.
	resp = get(t, ts, client, "/api/clubs/"+clubID, mgrCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get club = %d, want 200", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	var detail map[string]any
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("decode club detail: %v", err)
	}
	if detail["short_name"] == "" {
		t.Fatalf("club detail short_name missing: %v", detail)
	}
	if detail["is_ai_controlled"] != true {
		t.Fatalf("club detail is_ai_controlled = %v, want true", detail["is_ai_controlled"])
	}
	if mgr, ok := detail["manager"].(map[string]any); !ok || mgr["is_policy_bot"] != true {
		t.Fatalf("club detail manager = %v", detail["manager"])
	}
	if squad, ok := detail["squad"].([]any); !ok || len(squad) != 24 {
		t.Fatalf("club detail squad = %v entries, want 24", detail["squad"])
	}
	for _, p := range detail["squad"].([]any) {
		pm := p.(map[string]any)
		if pm["display_name"] == "" || pm["primary_position"] == "" || pm["age"] == nil {
			t.Fatalf("squad player malformed: %v", pm)
		}
	}

	// Worlds are sealed: a manager in another world can never read this club.
	otherWorld := testdb.CreateWorld(t, pool, "W-CLUB-OTHER")
	other := testdb.CreateUser(t, pool, "clubother@example.com", "s3cret", []testdb.Join{{WorldID: otherWorld}})
	testdb.MakeAdmin(t, pool, other)
	var otherManager uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = $1`, other).Scan(&otherManager); err != nil {
		t.Fatalf("other manager id: %v", err)
	}
	otherCookies := managerCookies(t, ts, pool, otherManager, other)
	if resp := get(t, ts, client, "/api/clubs/"+clubID, otherCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-world club read = %d, want 404", resp.StatusCode)
	}
}
