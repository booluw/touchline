//go:build integration

package httpapi_test

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
	playerEnt, _ := first["player"].(map[string]any)
	playerID, _ := playerEnt["id"].(string)
	if playerID == "" {
		t.Fatalf("player ref missing in %v", first)
	}
	if _, ok := first["morale"].(float64); !ok {
		t.Fatalf("player morale missing in %v", first)
	}
	if _, ok := first["squad_role"].(string); !ok {
		t.Fatalf("player squad_role missing in %v", first)
	}
	// IM17: every roster row carries the six attribute-category means and the
	// position-weighted overall (1..99, capped).
	attrs, ok := first["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("player attributes missing in %v", first)
	}
	for _, cat := range []string{"technical", "physical", "mental", "tactical", "goalkeeping", "positional"} {
		if _, ok := attrs[cat].(float64); !ok {
			t.Fatalf("attribute %q missing in %v", cat, attrs)
		}
	}
	overall, ok := first["overall"].(float64)
	if !ok {
		t.Fatalf("player overall missing in %v", first)
	}
	if overall < 1 || overall > 99 {
		t.Fatalf("player overall = %v, want 1..99", overall)
	}
	// IM40: nationality, age and the active contract on every bootstrapped row.
	for _, p := range players {
		row := p.(map[string]any)
		nat, natOK := row["nationality"].(map[string]any)
		if !natOK || nat["code"] == "" || nat["name"] == "" {
			t.Fatalf("roster nationality = %v", row["nationality"])
		}
		if age, ageOK := row["age"].(float64); !ageOK || age < 15 || age > 45 {
			t.Fatalf("roster age = %v", row["age"])
		}
		if dob, dobOK := row["date_of_birth"].(string); !dobOK || len(dob) != 10 {
			t.Fatalf("roster date_of_birth = %v", row["date_of_birth"])
		}
		contract, cOK := row["contract"].(map[string]any)
		if !cOK {
			t.Fatalf("roster contract = %v, want object", row["contract"])
		}
		if wage, wOK := contract["weekly_wage"].(float64); !wOK || wage <= 0 {
			t.Fatalf("roster contract weekly_wage = %v", contract["weekly_wage"])
		}
		if end, eOK := contract["end_date"].(string); !eOK || len(end) != 10 {
			t.Fatalf("roster contract end_date = %v", contract["end_date"])
		}
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

// TestHTTPTransferRequestPreviewReassureApprove covers IM65: the preview,
// reassure by player, and approve with an asking-price preset.
func TestHTTPTransferRequestPreviewReassureApprove(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "request-preview-owner@example.com"
	tw := transfertest.Provision(t, pool, "request-preview", email)
	cookies := loginManager(t, ts, pool, email)

	var ids []string
	rows, err := pool.Query(ctx, `SELECT id::text FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 2`, tw.HumanClub)
	if err != nil {
		t.Fatalf("pick players: %v", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) < 2 {
		t.Fatalf("need 2 players, got %d", len(ids))
	}
	for _, id := range ids {
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.player_transfer_requests (player_id, club_id, manager_id, status, reason, created_at)
			VALUES ($1, $2, $3, 'pending', 'playing_time', now())`, id, tw.HumanClub, tw.HumanMgr); err != nil {
			t.Fatalf("seed request: %v", err)
		}
	}
	url := func(id, action string) string {
		return fmt.Sprintf("/api/clubs/%s/players/%s/transfer-request/%s", tw.HumanClub, id, action)
	}

	resp := get(t, ts, client, url(ids[0], "preview"), cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview = %d, want 200", resp.StatusCode)
	}
	preview := decodeTransferBody(t, resp)
	if opts, _ := preview["options"].([]any); len(opts) != 3 {
		t.Fatalf("preview options = %v, want 3", preview["options"])
	}

	resp = post(t, ts, client, url(ids[0], "reassure"), `{}`, cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reassure = %d, want 200", resp.StatusCode)
	}
	if req, _ := decodeTransferBody(t, resp)["request"].(map[string]any); req == nil || req["status"] != "reassured" {
		t.Fatalf("reassure body status = %v, want reassured", req)
	}
	if resp = get(t, ts, client, url(ids[0], "preview"), cookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("preview after reassure = %d, want 404", resp.StatusCode)
	}

	if resp = post(t, ts, client, url(ids[1], "approve"), `{"price_preset":"bargain"}`, cookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("approve bad preset = %d, want 400", resp.StatusCode)
	}
	var value int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(market_value,0)::bigint FROM player.players WHERE id = $1`, ids[1]).Scan(&value); err != nil {
		t.Fatalf("market value: %v", err)
	}
	resp = post(t, ts, client, url(ids[1], "approve"), `{"price_preset":"hold_out"}`, cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve hold_out = %d, want 200", resp.StatusCode)
	}
	listing, _ := decodeTransferBody(t, resp)["listing"].(map[string]any)
	want := float64(int64(float64(value)*1.25 + 0.5))
	if listing == nil || listing["asking_price"] != want {
		t.Fatalf("listing = %v, want asking_price %v", listing, want)
	}
}
