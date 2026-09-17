//go:build integration

// HTTP integration coverage for the S08-03 injury endpoints
// (GET /clubs/:id/players/:playerID/injury, POST .../rush-return), the
// medical-facility routes (GET/PUT /clubs/:id/medical-facility) and the
// injured-player lineup gate (PUT /clubs/:id/lineup -> 422).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/transfertest"
)

// TestHTTPInjuryReadAndRushReturn drives the S08-03 player injury read model
// and the rushed-return command: a sidelined player reports the stored injury
// with read-time derived progress, the rush closes it and floors recurrence,
// a fit player reads null, and another manager's player is forbidden.
func TestHTTPInjuryReadAndRushReturn(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "injury-owner@example.com"
	tw := transfertest.Provision(t, pool, "injury-http", email)
	cookies := loginManager(t, ts, pool, email)

	var playerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		tw.HumanClub).Scan(&playerID); err != nil {
		t.Fatalf("pick human player: %v", err)
	}

	// A fit player reads null.
	resp := get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players/%s/injury", tw.HumanClub, playerID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("injury (fit) = %d, want 200", code)
	}
	if m := decodeTransferBody(t, resp); len(m) != 0 {
		t.Fatalf("injury (fit) body = %v, want null", m)
	}

	// Side-select the player (open injury started 5 days ago, 10-day plan).
	now := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, 'muscle', 4, $2, 0.2, $3)`,
		playerID, now.AddDate(0, 0, 10), now.AddDate(0, 0, -5)); err != nil {
		t.Fatalf("insert open injury: %v", err)
	}

	// The open injury reads with derived progress.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players/%s/injury", tw.HumanClub, playerID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("injury (open) = %d, want 200", code)
	}
	body := decodeTransferBody(t, resp)
	if stringField(t, body, "injury_type") != "muscle" {
		t.Errorf("injury_type = %v, want muscle", body["injury_type"])
	}
	if _, ok := body["recovery_progress"].(float64); !ok {
		t.Errorf("recovery_progress missing: %v", body)
	}
	if _, ok := body["days_remaining"].(float64); !ok {
		t.Errorf("days_remaining missing: %v", body)
	}

	// Rush the player back: closed, recurrence floored, event recorded.
	resp = post(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/rush-return", tw.HumanClub, playerID), ``, cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("rush-return = %d, want 200", code)
	}
	closed := decodeTransferBody(t, resp)
	if risk, _ := closed["recurrence_risk"].(float64); risk < 0.65 {
		t.Errorf("recurrence_risk after rush = %v, want ≥ 0.65", closed["recurrence_risk"])
	}
	if stringField(t, closed, "actual_recovery_date") == "" {
		t.Errorf("actual_recovery_date missing after rush: %v", closed)
	}
	var ev struct{ Payload string }
	if err := pool.QueryRow(ctx,
		`SELECT payload FROM world.events
		 WHERE world_id = $1 AND event_type = $2 AND payload::text LIKE $3`,
		tw.WorldID, "PLAYER_RUSHED_RETURN", "%"+playerID.String()+"%").Scan(&ev.Payload); err != nil {
		t.Fatalf("load PLAYER_RUSHED_RETURN event: %v", err)
	}

	// Now fit: reads null, and a second rush is a 404.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players/%s/injury", tw.HumanClub, playerID), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("injury (closed) = %d, want 200", code)
	}
	if m := decodeTransferBody(t, resp); len(m) != 0 {
		t.Fatalf("injury (closed) body = %v, want null", m)
	}
	resp = post(t, ts, client,
		fmt.Sprintf("/api/clubs/%s/players/%s/rush-return", tw.HumanClub, playerID), ``, cookies)
	if code := resp.StatusCode; code != http.StatusNotFound {
		t.Fatalf("double rush-return = %d, want 404", code)
	}

	// An AI club's player is forbidden through the human manager route.
	var aiPlayer uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		tw.AIOneClub).Scan(&aiPlayer); err != nil {
		t.Fatalf("pick ai player: %v", err)
	}
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/players/%s/injury", tw.HumanClub, aiPlayer), cookies)
	if code := resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("foreign player injury = %d, want 403", code)
	}
}

// TestHTTPMedicalFacilityUpgrade covers GET/PUT /clubs/:id/medical-facility:
// the neutral read (level 5, no row), the billed upgrade (finance ledger under
// a dedup key), the MEDICAL_FACILITY_UPGRADED event, and ownership gating.
func TestHTTPMedicalFacilityUpgrade(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "med-owner@example.com"
	tw := transfertest.Provision(t, pool, "medical-http", email)
	cookies := loginManager(t, ts, pool, email)

	// Neutral default 5 before any row exists.
	resp := get(t, ts, client, fmt.Sprintf("/api/clubs/%s/medical-facility", tw.HumanClub), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("get medical facility = %d, want 200", code)
	}
	if lvl := bodyInt(t, resp, "level"); lvl != 5 {
		t.Fatalf("initial medical level = %d, want 5", lvl)
	}
	var rows int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM club.facilities WHERE club_id = $1 AND facility_type = 'medical'`,
		tw.HumanClub).Scan(&rows); err != nil {
		t.Fatalf("count medical rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("read materialised %d medical rows, want 0", rows)
	}

	// Upgrade 5 -> 6: billed £1.0M (MedicalCostBase x 5) with a dedup key.
	resp = put(t, ts, client, fmt.Sprintf("/api/clubs/%s/medical-facility", tw.HumanClub), `{}`, cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("upgrade medical facility = %d, want 200", code)
	}
	if lvl := bodyInt(t, resp, "level"); lvl != 6 {
		t.Fatalf("upgraded medical level = %d, want 6", lvl)
	}
	var amount int64
	if err := pool.QueryRow(ctx, `
		SELECT l.amount FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1 AND l.dedup_key = $2`,
		tw.HumanClub, fmt.Sprintf("medical:upgrade:%s:6", tw.HumanClub)).Scan(&amount); err != nil {
		t.Fatalf("load ledger debit: %v", err)
	}
	if amount != 1_000_000 {
		t.Errorf("ledger debit = %d, want 1000000", amount)
	}
	var ev struct{ Payload string }
	if err := pool.QueryRow(ctx,
		`SELECT payload FROM world.events
		 WHERE world_id = $1 AND event_type = $2`, tw.WorldID, "MEDICAL_FACILITY_UPGRADED").Scan(&ev.Payload); err != nil {
		t.Fatalf("load MEDICAL_FACILITY_UPGRADED event: %v", err)
	}

	// Read reflects the upgrade; a second upgrade reads 7.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/medical-facility", tw.HumanClub), cookies)
	if lvl := bodyInt(t, resp, "level"); lvl != 6 {
		t.Fatalf("read medical level after upgrade = %d, want 6", lvl)
	}
	resp = put(t, ts, client, fmt.Sprintf("/api/clubs/%s/medical-facility", tw.HumanClub), `{}`, cookies)
	if lvl := bodyInt(t, resp, "level"); lvl != 7 {
		t.Fatalf("second upgrade = %d, want 7", lvl)
	}

	// A foreign (AI) club is forbidden.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/medical-facility", tw.AIOneClub), cookies)
	if code := resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("foreign medical facility = %d, want 403", code)
	}
	resp = put(t, ts, client, fmt.Sprintf("/api/clubs/%s/medical-facility", tw.AIOneClub), `{}`, cookies)
	if code := resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("foreign upgrade = %d, want 403", code)
	}
}

// TestHTTPLineupRejectsInjuredPlayer verifies the S08-03 gate: an open injury
// hard-rejects the whole lineup with 422 and writes nothing (slots stay empty).
func TestHTTPLineupRejectsInjuredPlayer(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "lineup-injury@example.com"
	_, clubID, cookies := tacticsClub(t, ts, pool, email)

	players := squadForLineup(t, pool, clubID)
	// Side-select the first selected outfield player.
	injured := players[0]
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, 'bone', 5, now()::date + 20, 0.2, now()::date - 2)`, injured); err != nil {
		t.Fatalf("insert open injury: %v", err)
	}

	resp := put(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", lineupBody(players), cookies)
	if code := resp.StatusCode; code != http.StatusUnprocessableEntity {
		t.Fatalf("lineup with injured player = %d, want 422", code)
	}
	raw, _ := io.ReadAll(resp.Body)
	var errBody map[string]any
	if err := json.Unmarshal(raw, &errBody); err != nil {
		t.Fatalf("decode 422 body: %v", err)
	}
	if errMsg := stringField(t, errBody, "error"); errMsg == "" {
		t.Fatalf("422 body must explain the unavailable player: %v", errBody)
	}

	// The rejected lineup is not persisted: slots are still empty.
	resp = get(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("get lineup after rejection = %d, want 200", code)
	}
	raw, _ = io.ReadAll(resp.Body)
	var lineup map[string]any
	if err := json.Unmarshal(raw, &lineup); err != nil {
		t.Fatalf("decode lineup: %v", err)
	}
	slots, _ := lineup["slots"].([]any)
	for i, s := range slots {
		m, _ := s.(map[string]any)
		if id, _ := m["player_id"].(string); id != uuid.Nil.String() {
			t.Fatalf("rejected lineup persisted player in slot %d: %s", i, id)
		}
	}
}

func bodyInt(t *testing.T, resp *http.Response, key string) int {
	t.Helper()
	m := decodeTransferBody(t, resp)
	v, _ := m[key].(float64)
	return int(v)
}
