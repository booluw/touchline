//go:build integration

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/transfertest"
)

// TestHTTPPlayerDetailRoundTrip covers the player detail page's read (IM20) at
// the HTTP edge: the wire shape, the world scoping (a foreign or unknown id is a
// plain 404), and the one private field — the weekly wage, which is present for
// the caller's own player and withheld for a rival's.
func TestHTTPPlayerDetailRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	const email = "player-detail-owner@example.com"
	tw := transfertest.Provision(t, pool, "player-detail-http", email)
	cookies := loginManager(t, ts, pool, email)

	// The caller's own player: full card, wage included.
	resp := get(t, ts, client, fmt.Sprintf("/api/players/%s", firstRosterPlayerID(t, ts, client, tw, cookies)), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("player detail = %d, want 200 (body: %v)", code, resp.Status)
	}
	card := decodeTransferBody(t, resp)
	if _, ok := card["display_name"].(string); !ok {
		t.Fatalf("display_name missing: %v", card)
	}
	if _, ok := card["club"].(map[string]any); !ok {
		t.Fatalf("club missing: %v", card)
	}
	attrs, ok := card["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes missing: %v", card)
	}
	for _, cat := range []string{"technical", "physical", "mental", "tactical", "goalkeeping", "positional"} {
		if _, ok := attrs[cat].(float64); !ok {
			t.Fatalf("attribute %q missing in %v", cat, attrs)
		}
	}
	overall, ok := card["overall"].(float64)
	if !ok || overall < 1 || overall > 99 {
		t.Fatalf("overall = %v, want 1..99", card["overall"])
	}
	career, ok := card["career"].(map[string]any)
	if !ok {
		t.Fatalf("career missing: %v", card)
	}
	for _, key := range []string{"appearances", "goals", "assists", "average_rating"} {
		if _, ok := career[key].(float64); !ok {
			t.Fatalf("career.%s missing in %v", key, career)
		}
	}
	if _, ok := card["weekly_wage"].(float64); !ok {
		t.Fatalf("own player's weekly_wage missing: %v", card)
	}

	// A rival's player in the same world is readable, but without the wage.
	rivalID := pickPlayerOf(t, pool, tw.AIOneClub)
	resp = get(t, ts, client, fmt.Sprintf("/api/players/%s", rivalID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("rival player detail = %d, want 200 (a world player is readable)", code)
	}
	rival := decodeTransferBody(t, resp)
	if _, present := rival["weekly_wage"]; present {
		t.Fatalf("rival's weekly_wage leaked: %v", rival)
	}

	// Unknown id and malformed id are both 404 / 400, never a 500.
	resp = get(t, ts, client, "/api/players/00000000-0000-4000-8000-0000000000ff", cookies)
	if code := resp.StatusCode; code != http.StatusNotFound {
		t.Fatalf("unknown player = %d, want 404", code)
	}
	resp = get(t, ts, client, "/api/players/not-a-uuid", cookies)
	if code := resp.StatusCode; code != http.StatusBadRequest {
		t.Fatalf("malformed player id = %d, want 400", code)
	}

	// Unauthenticated callers are rejected (the read is behind requireAuth).
	if code := get(t, ts, client, fmt.Sprintf("/api/players/%s", rivalID), "").StatusCode; code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated player detail = %d, want 401", code)
	}
}

// firstRosterPlayerID reads the caller's own roster through the API and returns
// the first player's id, so the detail assertions run against a player the
// fixture really owns.
func firstRosterPlayerID(t *testing.T, ts *httptest.Server, client *http.Client, tw transfertest.World, cookies string) uuid.UUID {
	t.Helper()
	resp := get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players", tw.HumanClub), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("list players = %d, want 200", code)
	}
	players, ok := decodeTransferBody(t, resp)["players"].([]any)
	if !ok || len(players) == 0 {
		t.Fatalf("roster is empty")
	}
	ref, _ := players[0].(map[string]any)["player"].(map[string]any)
	id, _ := ref["id"].(string)
	if id == "" {
		t.Fatalf("roster row has no player ref")
	}
	return uuid.MustParse(id)
}

// pickPlayerOf reads one active player of a club straight from the database.
func pickPlayerOf(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		clubID).Scan(&id); err != nil {
		t.Fatalf("pick player of %s: %v", clubID, err)
	}
	return id
}
