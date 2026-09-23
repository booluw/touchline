//go:build integration

package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/testdb"
)

// TestHTTPSeasonCalendarAndClubFixtures covers the IM03 read surface: the
// paced season calendar grouped by game-week at GET /api/competitions/:id/
// calendar, and a single club's fixture list at GET /api/clubs/:id/fixtures —
// both world-scoped to the caller's manager.
func TestHTTPSeasonCalendarAndClubFixtures(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	client := ts.Client()

	// World + admin + a manager who joins it.
	var worldID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO world.worlds (id, name, status) VALUES ($1, $2, 'provisioning') RETURNING id`,
		uuid.New(), "Calendar Town").Scan(&worldID); err != nil {
		t.Fatalf("insert world: %v", err)
	}
	admin := testdb.CreateUser(t, pool, "caladmin@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, admin)
	manager := testdb.CreateUser(t, pool, "calmanager@example.com", "s3cret", []testdb.Join{{WorldID: mustParseUUID(t, worldID)}})
	adminCookies := login(t, ts, client, "caladmin@example.com", "s3cret")
	login(t, ts, client, "calmanager@example.com", "s3cret")

	// Admin declares one league and seeds the world.
	resp := post(t, ts, client, "/api/admin/countries",
		`{"world_id":"`+worldID+`","code":"eng","name":"England"}`, adminCookies)
	countryID := decodeID(t, resp, "id")
	resp = post(t, ts, client, "/api/admin/leagues",
		`{"country_id":"`+countryID+`","name":"Premier","tier":1,"team_count":4}`, adminCookies)
	leagueID := decodeID(t, resp, "id")
	waitForSeed(t, ts, client, adminCookies, worldID)

	// The caller is a plain manager in the world (admin-only console keeps
	// manager reads independent), so mint a manager session for the reads.
	var plainManager uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = $1`, manager).Scan(&plainManager); err != nil {
		t.Fatalf("manager id: %v", err)
	}
	plainCookies := managerCookies(t, ts, pool, plainManager, manager)

	// Unauthenticated calendar read is rejected before any logic.
	if resp := get(t, ts, client, "/api/competitions/"+leagueID+"/calendar", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous calendar = %d, want 401", resp.StatusCode)
	}

	// No season yet: the calendar is a 404 (no active season).
	// Start the season, paced at the default 3 matchdays per 7 game-days.
	if resp := get(t, ts, client, "/api/competitions/"+leagueID+"/calendar", plainCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("calendar before season = %d, want 404", resp.StatusCode)
	}
	resp = post(t, ts, client, "/api/admin/worlds/"+worldID+"/leagues/"+leagueID+"/season", "", adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start season = %d, want 201", resp.StatusCode)
	}

	// Calendar: 6 matchdays of a 4-team round robin in 2 weeks (default pacing).
	resp = get(t, ts, client, "/api/competitions/"+leagueID+"/calendar", plainCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("calendar = %d, want 200", resp.StatusCode)
	}
	body := decodeMap(t, resp)
	weeks := body["weeks"].([]any)
	if len(weeks) != 2 {
		t.Fatalf("calendar weeks = %d, want 2", len(weeks))
	}
	wantDays := []int{1, 3, 5, 8, 10, 12}
	var gotMD int
	for _, w := range weeks {
		for _, md := range w.(map[string]any)["matchdays"].([]any) {
			gotMD++
			mdm := md.(map[string]any)
			scheduled := mustParseTime(t, mdm["scheduled_at"].(string))
			var worldLaunch time.Time
			if err := pool.QueryRow(context.Background(),
				`SELECT date_trunc('day', COALESCE(launched_at, created_at)) FROM world.worlds WHERE id = $1`, worldID).
				Scan(&worldLaunch); err != nil {
				t.Fatalf("world launch: %v", err)
			}
			if day := int(scheduled.Sub(worldLaunch).Hours() / 24); day != wantDays[gotMD-1] {
				t.Fatalf("matchday %d at day %d, want %d", gotMD, day, wantDays[gotMD-1])
			}
			fx := mdm["fixtures"].([]any)
			if len(fx) != 2 {
				t.Fatalf("matchday %d fixtures = %d, want 2", gotMD, len(fx))
			}
		}
	}
	if gotMD != 6 {
		t.Fatalf("calendar matchdays = %d, want 6", gotMD)
	}

	// Explicit season filter: ?season=1 matches the default read, ?season=99 is
	// a 404, and a non-numeric value is rejected with a 400.
	season1 := get(t, ts, client, "/api/competitions/"+leagueID+"/calendar?season=1", plainCookies)
	if season1.StatusCode != http.StatusOK {
		t.Fatalf("calendar season=1 = %d, want 200", season1.StatusCode)
	}
	if got := countMatchdays(decodeMap(t, season1)); got != 6 {
		t.Fatalf("calendar season=1 matchdays = %d, want 6", got)
	}
	if resp := get(t, ts, client, "/api/competitions/"+leagueID+"/calendar?season=99", plainCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("calendar season=99 = %d, want 404", resp.StatusCode)
	}
	if resp := get(t, ts, client, "/api/competitions/"+leagueID+"/calendar?season=abc", plainCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("calendar season=abc = %d, want 400", resp.StatusCode)
	}

	// Club fixtures: pick a club, expect its whole season, ordered by day.
	var clubID string
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY name LIMIT 1`, worldID).Scan(&clubID); err != nil {
		t.Fatalf("pick club: %v", err)
	}
	resp = get(t, ts, client, "/api/clubs/"+clubID+"/fixtures", plainCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("club fixtures = %d, want 200", resp.StatusCode)
	}
	fixtures := decodeMap(t, resp)["fixtures"].([]any)
	if len(fixtures) != 6 {
		t.Fatalf("club fixtures = %d, want 6", len(fixtures))
	}
	prev := time.Time{}
	for _, f := range fixtures {
		scheduled := mustParseTime(t, f.(map[string]any)["scheduled_at"].(string))
		if scheduled.Before(prev) {
			t.Fatalf("club fixtures out of order: %v before %v", scheduled, prev)
		}
		prev = scheduled
	}

	// Cross-world reads 404: another world's fake league/club are invisible.
	otherLeague := uuid.New().String()
	if resp := get(t, ts, client, "/api/competitions/"+otherLeague+"/calendar", plainCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other-world calendar = %d, want 404", resp.StatusCode)
	}
	if resp := get(t, ts, client, "/api/clubs/"+uuid.New().String()+"/fixtures", plainCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown club fixtures = %d, want 404", resp.StatusCode)
	}
}

// countMatchdays counts the matchdays across a decoded calendar payload.
func countMatchdays(body map[string]any) int {
	total := 0
	for _, w := range body["weeks"].([]any) {
		total += len(w.(map[string]any)["matchdays"].([]any))
	}
	return total
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm
}
