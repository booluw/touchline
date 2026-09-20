//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/scheduler"
	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/pkg/realtime"
)

// TestPhase0VerticalSlice proves the Phase-0 exit criterion end to end (plan
// §16): create a world, generate a club with a squad, and see a daily tick
// fire — delivered to an authenticated browser socket straight from the event
// log, with no client-side simulation.
//
// The candidate WebSocket is the browser stand-in. The tick's envelope is
// built with realtime.BuildWorldTick — the exact helper cmd/worker uses after
// consuming WORLD_TICK off the event bus — so the test exercises the same
// wire shape as production.
func TestPhase0VerticalSlice(t *testing.T) {
	ts, pool, hub := newRealtimeTestServer(t)
	waitHubReady(t, hub)
	testdb.SeedRefData(t, pool)
	ctx := context.Background()
	client := ts.Client()

	// Admin (in any world — admin routes need no world context) and, later, a
	// job-outside candidate who is also an admin under the launch gate (only
	// admins may log in during phase 1; manager sessions are minted below).
	adminWorld := testdb.CreateWorld(t, pool, "SLICE-ADMIN-WORLD")
	admin := testdb.CreateUser(t, pool, "slice-admin@example.com", "s3cret",
		[]testdb.Join{{WorldID: adminWorld}})
	testdb.MakeAdmin(t, pool, admin)
	adminCookies := login(t, ts, client, "slice-admin@example.com", "s3cret")

	// 1. Create a world (comes up in 'provisioning').
	resp := post(t, ts, client, "/api/admin/worlds", `{"name":"Phase Zero"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create world = %d, want 201 (%s)", resp.StatusCode, body)
	}
	var created struct {
		ID     uuid.UUID `json:"id"`
		Status string    `json:"status"`
	}
	if err := decodeJSON(t, resp, &created); err != nil {
		t.Fatalf("decode created world: %v", err)
	}
	if created.Status != "provisioning" {
		t.Fatalf("created world status = %q, want provisioning", created.Status)
	}
	world := created.ID

	candidate := testdb.CreateUser(t, pool, "slice-candidate@example.com", "s3cret",
		[]testdb.Join{{WorldID: world}})
	testdb.MakeAdmin(t, pool, candidate)
	var candidateManager uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = $1`, candidate).Scan(&candidateManager); err != nil {
		t.Fatalf("candidate manager id: %v", err)
	}
	// Manager-scoped reads need a real manager session, which the phase-1 login
	// gate does not mint (admins get world-less console sessions) — mint one.
	candidateCookies := managerCookies(t, ts, pool, candidateManager, candidate)

	// 2. Declare the world's structure (country + one league), then generate
	// the clubs with squads via the whole-world seed (launch model).
	resp = post(t, ts, client, "/api/admin/countries",
		`{"world_id":"`+world.String()+`","code":"eng","name":"England"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create country = %d, want 201 (%s)", resp.StatusCode, body)
	}
	countryID := decodeID(t, resp, "id")
	resp = post(t, ts, client, "/api/admin/leagues",
		`{"country_id":"`+countryID+`","name":"Slice League","tier":1,"team_count":4}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create league = %d, want 201 (%s)", resp.StatusCode, body)
	}
	resp = post(t, ts, client, "/api/admin/worlds/"+world.String()+"/seed", "", adminCookies)
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("seed = %d, want 202 (%s)", resp.StatusCode, string(raw))
	}
	status := waitForSeed(t, ts, client, adminCookies, world.String())
	if status["clubs"].(float64) != 4 {
		t.Fatalf("seeded clubs = %v, want 4", status["clubs"])
	}

	// The candidate can read the generated clubs and their squads in their world.
	resp = get(t, ts, client, "/api/clubs", candidateCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list clubs = %d, want 200", resp.StatusCode)
	}
	var clubs struct {
		Clubs []struct {
			ID uuid.UUID `json:"id"`
		} `json:"clubs"`
	}
	if err := decodeJSON(t, resp, &clubs); err != nil {
		t.Fatalf("decode clubs: %v", err)
	}
	if len(clubs.Clubs) != 4 {
		t.Fatalf("world clubs = %d, want 4", len(clubs.Clubs))
	}
	clubID := clubs.Clubs[0].ID

	// 3. Launch the world, then set the daily cadence the scheduler syncs to.
	resp = post(t, ts, client, "/api/admin/worlds/"+world.String()+"/status",
		`{"status":"active"}`, adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("launch = %d, want 200", resp.StatusCode)
	}
	resp = post(t, ts, client, "/api/admin/worlds/"+world.String()+"/config",
		`{"key":"tick.daily_cadence","value":"* * * * *"}`, adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set daily cadence = %d, want 200", resp.StatusCode)
	}

	// 4. A daily tick fires (deterministically, as the S02-03 scheduler does).
	if err := scheduler.NewService(pool, nil).FireTick(ctx, world, "daily"); err != nil {
		t.Fatalf("fire daily tick: %v", err)
	}

	// 4a. The tick is in the event log with its world context — the server is
	// the source of truth; nothing is simulated client-side.
	var (
		evID        uuid.UUID
		evWorldID   uuid.UUID
		evTick      int64
		evGran      string
		currentTick int64
	)
	if err := pool.QueryRow(ctx, `
		SELECT id, world_id, world_tick, payload::jsonb->>'granularity'
		FROM world.events
		WHERE world_id = $1 AND event_type = 'WORLD_TICK'
		ORDER BY occurred_at DESC LIMIT 1`, world,
	).Scan(&evID, &evWorldID, &evTick, &evGran); err != nil {
		t.Fatalf("read WORLD_TICK event: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT current_tick FROM world.worlds WHERE id = $1`, world).Scan(&currentTick); err != nil {
		t.Fatalf("read current_tick: %v", err)
	}
	if evWorldID != world || evTick != 1 || evGran != "daily" {
		t.Fatalf("tick event context mismatch: world=%s tick=%d granularity=%s", evWorldID, evTick, evGran)
	}
	if currentTick != 1 {
		t.Fatalf("world.current_tick = %d, want 1", currentTick)
	}

	// 4b. The candidate's browser socket sees the tick live. The envelope is
	// built with the same helper the worker's realtime bridge uses, so this is
	// the production wire shape.
	conn, _ := wsConnectFor(t, ts, pool, "slice-candidate@example.com", world)
	tickEvent, err := realtime.BuildWorldTick(world, evID.String(), "daily", evTick)
	if err != nil {
		t.Fatalf("build world_tick envelope: %v", err)
	}
	if err := hub.Publish(ctx, tickEvent); err != nil {
		t.Fatalf("publish world_tick: %v", err)
	}
	got, err := readWSEvent(t, conn, 3*time.Second)
	if err != nil {
		t.Fatalf("read ws world_tick: %v", err)
	}
	if got.Type != realtime.EventWorldTick {
		t.Fatalf("ws event type = %q, want %q", got.Type, realtime.EventWorldTick)
	}
	if got.WorldID == nil || *got.WorldID != world {
		t.Fatalf("ws event world = %v, want %s", got.WorldID, world)
	}
	var payload realtime.WorldTickPayload
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("decode ws payload: %v", err)
	}
	if payload.EventID != evID.String() || payload.Granularity != "daily" || payload.Tick != 1 {
		t.Fatalf("ws payload = %+v, want event %s daily tick 1", payload, evID)
	}

	// 5. Failures surface as understandable user feedback (AC3, API side).
	// Wrong password -> 401; authenticated but no world context -> 403.
	if resp := post(t, ts, client, "/api/auth/login",
		`{"email":"slice-candidate@example.com","password":"wrong"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", resp.StatusCode)
	}
	ghost := testdb.CreateUser(t, pool, "slice-ghost@example.com", "s3cret",
		[]testdb.Join{{WorldID: world}})
	testdb.MakeAdmin(t, pool, ghost)
	if _, err := pool.Exec(ctx, `DELETE FROM manager.managers WHERE user_id = $1`, ghost); err != nil {
		t.Fatalf("remove ghost manager row: %v", err)
	}
	ghostCookies := login(t, ts, client, "slice-ghost@example.com", "s3cret")
	resp = get(t, ts, client, "/api/clubs", ghostCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no-world-context clubs = %d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "no world context") {
		t.Fatalf("no-world-context error = %s, want readable message", body)
	}

	var squad struct {
		Squad []json.RawMessage `json:"squad"`
	}
	resp = get(t, ts, client, "/api/clubs/"+clubID.String(), candidateCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("club detail = %d, want 200", resp.StatusCode)
	}
	if err := decodeJSON(t, resp, &squad); err != nil {
		t.Fatalf("decode club detail: %v", err)
	}
	if len(squad.Squad) != 24 {
		t.Fatalf("squad size = %d, want 24", len(squad.Squad))
	}
}

func decodeJSON(t *testing.T, resp *http.Response, out any) error {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	return json.Unmarshal(raw, out)
}
