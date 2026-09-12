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

// TestHTTPBootstrapAndClubReads covers the S03-01 API surface: the admin
// bootstrap endpoint (201/403/409), world-scoped club listing, and club detail
// with manager + squad.
func TestHTTPBootstrapAndClubReads(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedRefData(t, pool)
	client := ts.Client()

	w := testdb.CreateWorld(t, pool, "W-BOOT")
	admin := testdb.CreateUser(t, pool, "bootadmin@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, admin)

	// A provisioning world is created directly (the admin world-create route is
	// covered by TestHTTPAdminWorldLifecycle) so the admin's single manager row
	// resolves to the world this test bootstraps.
	var worldID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO world.worlds (id, name, status) VALUES ($1, $2, 'provisioning') RETURNING id`,
		uuid.New(), "Bootstrap Town").Scan(&worldID); err != nil {
		t.Fatalf("insert provisioning world: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO manager.managers (world_id, user_id, is_policy_bot, status)
		VALUES ($1, $2, FALSE, 'active')`, worldID, admin); err != nil {
		t.Fatalf("insert admin manager row: %v", err)
	}

	adminCookies := login(t, ts, client, "bootadmin@example.com", "s3cret")
	testdb.CreateUser(t, pool, "bootplain@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	plainCookies := login(t, ts, client, "bootplain@example.com", "s3cret")

	// Non-admins may not bootstrap a world.
	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/bootstrap",
		`{"name":"Sneaky FC"}`, plainCookies); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin bootstrap = %d, want 403", resp.StatusCode)
	}

	// The admin bootstraps the world into a club + squad.
	resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/bootstrap",
		`{"name":"Harbour United","short_name":"HAR"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("bootstrap = %d, want 201", resp.StatusCode)
	}
	var boot map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &boot); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	clubID, _ := boot["club_id"].(string)
	if clubID == "" || boot["club_name"] != "Harbour United" || boot["manager_id"] == "" {
		t.Fatalf("bootstrap result = %v", boot)
	}
	if squad, ok := boot["squad_size"].(float64); !ok || int(squad) != 24 {
		t.Fatalf("squad_size = %v, want 24", boot["squad_size"])
	}
	if ps, ok := boot["players"].([]any); !ok || len(ps) != 24 {
		t.Fatalf("players = %v entries, want 24", boot["players"])
	}
	firstPlayer := boot["players"].([]any)[0].(map[string]any)
	if firstPlayer["primary_position"] == "" || firstPlayer["first_name"] == "" || firstPlayer["squad_number"].(float64) != 1 {
		t.Fatalf("first player malformed: %v", firstPlayer)
	}

	// A second bootstrap is a conflict (world already material).
	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/bootstrap",
		`{"name":"Twice FC"}`, adminCookies); resp.StatusCode != http.StatusConflict {
		t.Fatalf("double bootstrap = %d, want 409", resp.StatusCode)
	}

	// The caller's world lists exactly the bootstrapped club.
	resp = get(t, ts, client, "/api/clubs", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list clubs = %d, want 200", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	var list map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode clubs: %v", err)
	}
	if clubs, ok := list["clubs"].([]any); !ok || len(clubs) != 1 {
		t.Fatalf("clubs = %v, want exactly 1", list["clubs"])
	}

	// Club detail exposes manager + 24-player squad for the same world.
	resp = get(t, ts, client, "/api/clubs/"+clubID, adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get club = %d, want 200", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	var detail map[string]any
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("decode club detail: %v", err)
	}
	if detail["name"] != "Harbour United" || detail["short_name"] != "HAR" {
		t.Fatalf("club detail identity = %v", detail)
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

	// Worlds are sealed: a user in another world can never read this club.
	otherWorld := testdb.CreateWorld(t, pool, "W-BOOT-OTHER")
	other := testdb.CreateUser(t, pool, "bootother@example.com", "s3cret", []testdb.Join{{WorldID: otherWorld}})
	testdb.MakeAdmin(t, pool, other)
	otherCookies := login(t, ts, client, "bootother@example.com", "s3cret")
	if resp := get(t, ts, client, "/api/clubs/"+clubID, otherCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-world club read = %d, want 404", resp.StatusCode)
	}
}
