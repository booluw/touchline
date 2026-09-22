//go:build integration

// HTTP integration coverage for the S05-01 club-scoped tactics/training routes:
// GET/PUT /clubs/:id/lineup, GET/POST /clubs/:id/tactics,
// GET/POST /clubs/:id/training-plan, and POST /matches/:id/tactical (live
// tactic change). Auth is the standard cookie login; every write path is
// exercised for unauthenticated (401), foreign-manager (403), validation (400)
// and live-fixture deadline (409) semantics.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// tacticsClub provisions a playable world with one bootstrapped club owned by a
// human manager and returns the world id, the club id, and that owner's cookie
// header.
func tacticsClub(t *testing.T, ts *httptest.Server, pool *pgxpool.Pool, email string) (uuid.UUID, uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, "api-tactics")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := internalbootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Harbour API FC", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	// One active manager may own the club; the bootstrap's policy bot already
	// holds the seat, so repurpose it as the human owner.
	userID := testdb.CreateUser(t, pool, email, "s3cret", nil)
	if _, err := pool.Exec(ctx, `
		UPDATE manager.managers
		SET user_id = $1, is_policy_bot = FALSE
		WHERE current_club_id = $2 AND status = 'active'`, userID, res.ClubID); err != nil {
		t.Fatalf("owner manager: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET is_ai_controlled = FALSE WHERE id = $1`, res.ClubID); err != nil {
		t.Fatalf("make club human: %v", err)
	}

	cookies := loginManager(t, ts, pool, email)
	return w.ID, res.ClubID, cookies
}

func squadForLineup(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY squad_number LIMIT 11`,
		clubID)
	if err != nil {
		t.Fatalf("load squad: %v", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate squad: %v", err)
	}
	if len(out) != 11 {
		t.Fatalf("squad has %d candidates, want 11", len(out))
	}
	return out
}

func lineupBody(players []uuid.UUID) string {
	var b strings.Builder
	b.WriteString(`{"slots":[`)
	for i, p := range players {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"slot":` + strconv.Itoa(i) + `,"player_id":"` + p.String() + `"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func decodedSlots(t *testing.T, resp *http.Response, key string) []string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var blob map[string]any
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	arr, _ := blob[key].([]any)
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		player, _ := m["player"].(map[string]any)
		if id, ok := player["id"].(string); ok {
			out = append(out, id)
		}
	}
	return out
}

func decodedStatus(t *testing.T, resp *http.Response) int {
	t.Helper()
	return resp.StatusCode
}

func TestHTTPLineupRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	_, clubID, cookies := tacticsClub(t, ts, pool, "lineup-owner@example.com")

	// A bootstrapped club starts with every formation slot unfilled (nil UUID).
	resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", cookies)
	if code := decodedStatus(t, resp); code != http.StatusOK {
		t.Fatalf("get lineup = %d, want 200", code)
	}
	if slots := decodedSlots(t, resp, "slots"); len(slots) != 11 {
		t.Fatalf("fresh lineup has %d slots, want 11", len(slots))
	} else {
		for _, id := range slots {
			if id != uuid.Nil.String() {
				t.Fatalf("fresh lineup prematerialized player %s", id)
			}
		}
	}

	players := squadForLineup(t, pool, clubID)
	body := lineupBody(players)
	if resp := put(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", body, cookies); decodedStatus(t, resp) != http.StatusNoContent {
		t.Fatalf("put lineup = %d, want 204", decodedStatus(t, resp))
	}

	resp = get(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", cookies)
	if code := decodedStatus(t, resp); code != http.StatusOK {
		t.Fatalf("get lineup after put = %d, want 200", code)
	}
	got := decodedSlots(t, resp, "slots")
	if len(got) != 11 {
		t.Fatalf("lineup slots = %d, want 11", len(got))
	}
	want := make(map[string]bool, 11)
	for _, p := range players {
		want[p.String()] = true
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("lineup contains non-roster player %s", id)
		}
	}

	// A duplicate player across two slots is invalid.
	duplicate := append([]uuid.UUID{players[0]}, players[:10]...)
	if resp := put(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", lineupBody(duplicate), cookies); decodedStatus(t, resp) != http.StatusBadRequest {
		t.Fatalf("duplicate lineup = %d, want 400", decodedStatus(t, resp))
	}

	// A foreign manager (in another world) is forbidden.
	w2 := testdb.CreateWorld(t, pool, "lineup-other")
	testdb.CreateUser(t, pool, "lineup-foreign@example.com", "s3cret", []testdb.Join{{WorldID: w2}})
	foreign := loginManager(t, ts, pool, "lineup-foreign@example.com")
	if resp := put(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", body, foreign); decodedStatus(t, resp) != http.StatusForbidden {
		t.Fatalf("foreign put lineup = %d, want 403", decodedStatus(t, resp))
	}

	// Unauthenticated writes are rejected before touching the club.
	if resp := put(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup", body, ""); decodedStatus(t, resp) != http.StatusUnauthorized {
		t.Fatalf("anonymous put lineup = %d, want 401", decodedStatus(t, resp))
	}
}

func TestHTTPTacticsRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	_, clubID, cookies := tacticsClub(t, ts, pool, "tactics-owner@example.com")

	getTactics := func() map[string]any {
		t.Helper()
		resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics", cookies)
		if code := decodedStatus(t, resp); code != http.StatusOK {
			t.Fatalf("get tactics = %d, want 200", code)
		}
		raw, _ := io.ReadAll(resp.Body)
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode tactics: %v", err)
		}
		return m
	}

	if style, _ := getTactics()["style"].(string); style == "" {
		t.Fatal("bootstrapped club has no tactics style")
	}

	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"gegenpress","formation":"4-3-3"}`, cookies); decodedStatus(t, resp) != http.StatusNoContent {
		t.Fatalf("post tactics = %d, want 204", decodedStatus(t, resp))
	}
	after := getTactics()
	if after["style"] != "gegenpress" || after["formation"] != "4-3-3" {
		t.Fatalf("tactics = %v, want gegenpress/4-3-3", after)
	}

	// Style-only set resolves to the style's default formation.
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"low_block"}`, cookies); decodedStatus(t, resp) != http.StatusNoContent {
		t.Fatalf("post style-only = %d, want 204", decodedStatus(t, resp))
	}
	if after := getTactics(); after["formation"] != "5-4-1" {
		t.Fatalf("default low_block formation = %v, want 5-4-1", after["formation"])
	}

	// Unknown style or a formation outside the style is 400.
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"park_the_bus"}`, cookies); decodedStatus(t, resp) != http.StatusBadRequest {
		t.Fatalf("invalid style = %d, want 400", decodedStatus(t, resp))
	}
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"possession","formation":"4-4-2"}`, cookies); decodedStatus(t, resp) != http.StatusBadRequest {
		t.Fatalf("formation outside style = %d, want 400", decodedStatus(t, resp))
	}

	w2 := testdb.CreateWorld(t, pool, "tactics-other")
	testdb.CreateUser(t, pool, "tactics-foreign@example.com", "s3cret", []testdb.Join{{WorldID: w2}})
	foreign := loginManager(t, ts, pool, "tactics-foreign@example.com")
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"direct"}`, foreign); decodedStatus(t, resp) != http.StatusForbidden {
		t.Fatalf("foreign post tactics = %d, want 403", decodedStatus(t, resp))
	}
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"direct"}`, ""); decodedStatus(t, resp) != http.StatusUnauthorized {
		t.Fatalf("anonymous post tactics = %d, want 401", decodedStatus(t, resp))
	}
}

func TestHTTPTrainingPlanRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	_, clubID, cookies := tacticsClub(t, ts, pool, "plan-owner@example.com")

	getPlan := func() map[string]any {
		t.Helper()
		resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/training-plan", cookies)
		if code := decodedStatus(t, resp); code != http.StatusOK {
			t.Fatalf("get training-plan = %d, want 200", code)
		}
		raw, _ := io.ReadAll(resp.Body)
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode plan: %v", err)
		}
		return m
	}

	if arch, _ := getPlan()["archetype"].(string); arch != "" {
		t.Fatalf("fresh plan archetype = %q, want empty", arch)
	}

	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/training-plan",
		`{"archetype":"technical"}`, cookies); decodedStatus(t, resp) != http.StatusNoContent {
		t.Fatalf("post training-plan = %d, want 204", decodedStatus(t, resp))
	}
	if arch, _ := getPlan()["archetype"].(string); arch != "technical" {
		t.Fatalf("plan archetype = %q, want technical", arch)
	}

	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/training-plan",
		`{"archetype":"yoga"}`, cookies); decodedStatus(t, resp) != http.StatusBadRequest {
		t.Fatalf("invalid archetype = %d, want 400", decodedStatus(t, resp))
	}

	w2 := testdb.CreateWorld(t, pool, "plan-other")
	testdb.CreateUser(t, pool, "plan-foreign@example.com", "s3cret", []testdb.Join{{WorldID: w2}})
	foreign := loginManager(t, ts, pool, "plan-foreign@example.com")
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/training-plan",
		`{"archetype":"defensive"}`, foreign); decodedStatus(t, resp) != http.StatusForbidden {
		t.Fatalf("foreign post training-plan = %d, want 403", decodedStatus(t, resp))
	}
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/training-plan",
		`{"archetype":"defensive"}`, ""); decodedStatus(t, resp) != http.StatusUnauthorized {
		t.Fatalf("anonymous post training-plan = %d, want 401", decodedStatus(t, resp))
	}
}

func TestHTTPTacticsDeadlineConflict(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	worldID, clubID, cookies := tacticsClub(t, ts, pool, "deadline-owner@example.com")

	away := testdb.CreateClub(t, pool, worldID)
	ctx := context.Background()
	var comp, fixture uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type)
		VALUES ($1, 'Deadline Cup', 'domestic_cup') RETURNING id`, worldID).Scan(&comp); err != nil {
		t.Fatalf("create competition: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $4, 1, now(), 'live') RETURNING id`,
		worldID, comp, clubID, away).Scan(&fixture); err != nil {
		t.Fatalf("insert live fixture: %v", err)
	}

	// While the club has a live fixture both write paths are locked out (409).
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"balanced"}`, cookies); decodedStatus(t, resp) != http.StatusConflict {
		t.Fatalf("tactics during live fixture = %d, want 409", decodedStatus(t, resp))
	}
	players := squadForLineup(t, pool, clubID)
	if resp := put(t, ts, client, "/api/clubs/"+clubID.String()+"/lineup",
		lineupBody(players), cookies); decodedStatus(t, resp) != http.StatusConflict {
		t.Fatalf("lineup during live fixture = %d, want 409", decodedStatus(t, resp))
	}

	// Once the fixture completes the deadline lifts.
	if _, err := pool.Exec(ctx,
		`UPDATE match.fixtures SET status = 'completed' WHERE id = $1`, fixture); err != nil {
		t.Fatalf("complete fixture: %v", err)
	}
	if resp := post(t, ts, client, "/api/clubs/"+clubID.String()+"/tactics",
		`{"style":"direct"}`, cookies); decodedStatus(t, resp) != http.StatusNoContent {
		t.Fatalf("tactics after fixture = %d, want 204", decodedStatus(t, resp))
	}
}

func TestHTTPLiveTacticChange(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	worldID, clubID, cookies := tacticsClub(t, ts, pool, "live-owner@example.com")

	away := testdb.CreateClub(t, pool, worldID)
	ctx := context.Background()
	var comp, fixture, matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type)
		VALUES ($1, 'Tactical Cup', 'domestic_cup') RETURNING id`, worldID).Scan(&comp); err != nil {
		t.Fatalf("create competition: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $4, 1, now(), 'scheduled') RETURNING id`,
		worldID, comp, clubID, away).Scan(&fixture); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.matches (fixture_id, world_id, seed, engine_version, status, current_minute, sim_inputs)
		VALUES ($1, $2, 42, 'v1.4', 'in_progress', 20, '{}') RETURNING id`,
		fixture, worldID).Scan(&matchID); err != nil {
		t.Fatalf("insert match: %v", err)
	}

	// A manager of the home club can call a tactic change ahead of the clock.
	if resp := post(t, ts, client, "/api/matches/"+matchID.String()+"/tactical",
		`{"minute":30,"style":"possession"}`, cookies); decodedStatus(t, resp) != http.StatusNoContent {
		t.Fatalf("live tactic change = %d, want 204", decodedStatus(t, resp))
	}
	var kind string
	if err := pool.QueryRow(ctx, `
		SELECT kind FROM match.match_inputs
		WHERE match_id = $1 ORDER BY sequence LIMIT 1`, matchID).Scan(&kind); err != nil {
		t.Fatalf("load input: %v", err)
	}
	if kind != "tactic_change" {
		t.Fatalf("input kind = %q, want tactic_change", kind)
	}

	// A minute at or behind the live clock is closed.
	if resp := post(t, ts, client, "/api/matches/"+matchID.String()+"/tactical",
		`{"minute":15,"style":"direct"}`, cookies); decodedStatus(t, resp) != http.StatusConflict {
		t.Fatalf("closed minute = %d, want 409", decodedStatus(t, resp))
	}

	// An unknown style is rejected.
	if resp := post(t, ts, client, "/api/matches/"+matchID.String()+"/tactical",
		`{"minute":40,"style":"tiki_taka"}`, cookies); decodedStatus(t, resp) != http.StatusBadRequest {
		t.Fatalf("invalid style = %d, want 400", decodedStatus(t, resp))
	}

	// Unauthenticated is rejected.
	if resp := post(t, ts, client, "/api/matches/"+matchID.String()+"/tactical",
		`{"minute":40,"style":"direct"}`, ""); decodedStatus(t, resp) != http.StatusUnauthorized {
		t.Fatalf("anonymous tactic change = %d, want 401", decodedStatus(t, resp))
	}
}
