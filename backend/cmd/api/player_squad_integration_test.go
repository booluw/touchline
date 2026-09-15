//go:build integration

package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/touchline/backend/internal/transfertest"
)

func TestHTTPPlayerSquadRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "player-http-owner@example.com"
	tw := transfertest.Provision(t, pool, "player-http", email)
	cookies := loginManager(t, ts, pool, email)

	// Human club roster returns a non-empty players array.
	resp := get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players", tw.HumanClub), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("list players = %d, want 200 (body: %v)", code, resp.Status)
	}
	listBody := decodeTransferBody(t, resp)
	players, ok := listBody["players"].([]any)
	if !ok || len(players) < 1 {
		t.Fatalf("players array = %v, want ≥ 1 element", listBody["players"])
	}
	first := players[0].(map[string]any)
	playerID := first["player_id"].(string)
	if _, ok := first["morale"].(float64); !ok {
		t.Fatalf("player morale missing in %v", first)
	}
	if _, ok := first["squad_role"].(string); !ok {
		t.Fatalf("player squad_role missing in %v", first)
	}

	// Individual morale detail exposes the explanation.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players/%s", tw.HumanClub, playerID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("player detail = %d, want 200", code)
	}
	detail := decodeTransferBody(t, resp)
	if _, ok := detail["explanation"].(map[string]any); !ok {
		t.Fatalf("detail explanation missing: %v", detail)
	}
	if _, ok := detail["expectations"].([]any); !ok {
		t.Fatalf("detail expectations missing: %v", detail)
	}

	// Promise-playing-time succeeds for an owned player.
	resp = post(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/promise-playing-time", tw.HumanClub, playerID),
		`{}`, cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("promise = %d, want 200", code)
	}

	// An AI-club player cannot be accessed via the human club route.
	var aiPlayer string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		tw.AIOneClub).Scan(&aiPlayer); err != nil {
		t.Fatalf("pick ai player: %v", err)
	}
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players/%s", tw.HumanClub, aiPlayer), cookies)
	if code := resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("other club player = %d, want 403", code)
	}

	// Unknown player → 404.
	resp = get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/00000000-0000-4000-8000-000000000999", tw.HumanClub),
		cookies)
	if code := resp.StatusCode; code != http.StatusNotFound {
		t.Fatalf("unknown player = %d, want 404", code)
	}

	// Anonymous → 401.
	resp = get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players", tw.HumanClub), "")
	if code := resp.StatusCode; code != http.StatusUnauthorized {
		t.Fatalf("anonymous list players = %d, want 401", code)
	}

	// Transfer-request approve/deny require a pending request. We insert one
	// manually to exercise the HTTP path without pulling the weekly engine in.
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_transfer_requests
			(player_id, club_id, manager_id, status, reason, created_at)
		VALUES ($1, $2, $3, 'pending', 'playing_time', now())`,
		playerID, tw.HumanClub, tw.HumanMgr); err != nil {
		t.Fatalf("seed request: %v", err)
	}

	resp = post(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/transfer-request/deny", tw.HumanClub, playerID),
		`{}`, cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("deny = %d, want 200", code)
	}
	denyBody := decodeTransferBody(t, resp)
	denied, _ := denyBody["request"].(map[string]any)
	if denied == nil || denied["status"] != "denied" {
		t.Fatalf("deny body = %v, want status denied", denyBody)
	}
}
