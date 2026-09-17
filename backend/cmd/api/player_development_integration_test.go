//go:build integration

package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/touchline/backend/internal/transfertest"
)

// TestHTTPPlayerDevelopmentDetail exercises the S08-02 read surface
// (GET /api/clubs/:id/players/:playerID/development): the observable trajectory
// and drivers are returned while the hidden potential ceiling never is.
func TestHTTPPlayerDevelopmentDetail(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "dev-http-owner@example.com"
	tw := transfertest.Provision(t, pool, "dev-http", email)
	cookies := loginManager(t, ts, pool, email)

	var playerID, player2ID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		tw.HumanClub).Scan(&playerID); err != nil {
		t.Fatalf("pick player: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id DESC LIMIT 1`,
		tw.HumanClub).Scan(&player2ID); err != nil {
		t.Fatalf("pick second player: %v", err)
	}

	// Seed development state, weekly deltas and the latest DEVELOPMENT_WEEK
	// event (the weekly engine writes these; here we seed them directly).
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_development
			(player_id, last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks,
			 potential_expansions_remaining, potential_locked_week)
		VALUES ($1, 100, 9, 2, 3, NULL)`, playerID); err != nil {
		t.Fatalf("seed dev state: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_attribute_changes (player_id, applied_week, attribute_key, delta)
		VALUES ($1, 99, 'stamina', 2), ($1, 100, 'finishing', 1), ($1, 100, 'morale', -5)`,
		playerID); err != nil {
		t.Fatalf("seed deltas: %v", err)
	}
	payload := fmt.Sprintf(
		`{"club_id":%q,"archetype":"balanced","players":[{"player_id":%q,"explanation":{"subject":"player_development","score":0,"factors":[{"label":"Age 19 mental training risk","delta":3}]}}]}`,
		tw.HumanClub, playerID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO world.events (world_id, world_tick, event_type, actor_type, payload)
		VALUES ($1, 100, 'DEVELOPMENT_WEEK', 'system', $2::jsonb)`, tw.WorldID, payload); err != nil {
		t.Fatalf("seed dev event: %v", err)
	}

	// Owned player with state → 200 with trajectory + drivers, no ceiling.
	resp := get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/development", tw.HumanClub, playerID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("development = %d, want 200", code)
	}
	body := decodeTransferBody(t, resp)
	if got := body["last_eval_week"].(float64); got != 100 {
		t.Fatalf("last_eval_week = %v, want 100", got)
	}
	if got := body["cum_dev_weeks"].(float64); got != 9 {
		t.Fatalf("cum_dev_weeks = %v, want 9", got)
	}
	if got := body["consecutive_stagnant_weeks"].(float64); got != 2 {
		t.Fatalf("consecutive_stagnant_weeks = %v, want 2", got)
	}
	if got := body["stagnating"].(bool); got {
		t.Fatalf("stagnating = %v, want false with 2 consecutive weeks", got)
	}
	deltas, ok := body["recent_deltas"].([]any)
	if !ok || len(deltas) != 2 {
		t.Fatalf("recent_deltas = %v, want 2 (morale pseudo-key excluded)", body["recent_deltas"])
	}
	first := deltas[0].(map[string]any)
	if first["attribute_key"] != "finishing" || first["applied_week"].(float64) != 100 {
		t.Fatalf("recent_deltas[0] = %v, want financing week 100 newest-first", first)
	}
	drivers, ok := body["drivers"].(map[string]any)
	if !ok || drivers["subject"] != "player_development" {
		t.Fatalf("drivers = %v, want stored DEVELOPMENT_WEEK explanation", body["drivers"])
	}
	// The hidden ceiling must never surface.
	for _, key := range []string{"potential", "potential_expansions_remaining", "potential_locked", "potential_locked_week"} {
		if _, present := body[key]; present {
			t.Fatalf("hidden ceiling key %q leaked in response %v", key, body)
		}
	}

	// Never-evaluated player → 200 with zeros and no drivers.
	resp = get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/development", tw.HumanClub, player2ID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("unevaluated development = %d, want 200", code)
	}
	emptyBody := decodeTransferBody(t, resp)
	if got := emptyBody["last_eval_week"].(float64); got != 0 {
		t.Fatalf("unevaluated last_eval_week = %v, want 0", got)
	}
	if got := emptyBody["cum_dev_weeks"].(float64); got != 0 {
		t.Fatalf("unevaluated cum_dev_weeks = %v, want 0", got)
	}
	if deltas, ok := emptyBody["recent_deltas"].([]any); !ok || len(deltas) != 0 {
		t.Fatalf("unevaluated recent_deltas = %v, want empty array", emptyBody["recent_deltas"])
	}
	if _, present := emptyBody["drivers"]; present {
		t.Fatalf("unevaluated drivers should be absent/null, got %v", emptyBody["drivers"])
	}

	// An AI-club player cannot be read via the human club route.
	var aiPlayer string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		tw.AIOneClub).Scan(&aiPlayer); err != nil {
		t.Fatalf("pick ai player: %v", err)
	}
	resp = get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/development", tw.HumanClub, aiPlayer), cookies)
	if code := resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("AI-club development = %d, want 403", code)
	}

	// Invalid player id → 400; anonymous → 401.
	resp = get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/not-a-uuid/development", tw.HumanClub), cookies)
	if code := resp.StatusCode; code != http.StatusBadRequest {
		t.Fatalf("invalid player id = %d, want 400", code)
	}
	resp = get(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/development", tw.HumanClub, playerID), "")
	if code := resp.StatusCode; code != http.StatusUnauthorized {
		t.Fatalf("anonymous development = %d, want 401", code)
	}
}
