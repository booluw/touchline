//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
)

// insertClub inserts a named club row and returns its id.
func insertClub(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO club.clubs (world_id, name, short_name, country) VALUES ($1, $2, $2, 'testland') RETURNING id`,
		worldID, name).Scan(&id); err != nil {
		t.Fatalf("insert club %s: %v", name, err)
	}
	return id
}

// insertCompletedFixture materializes one completed fixture with its finished
// match row and a 9-event feed (goals, cards, sub, injury, half/full time) so
// the match-screen endpoints have something to serve without the engine. The
// goal events and the stamped scoreline agree (2-1).
func insertCompletedFixture(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, homeClub, awayClub uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	var compID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO competition.competitions (world_id, name, competition_type) VALUES ($1, 'Feed Cup', 'domestic_cup') RETURNING id`,
		worldID).Scan(&compID); err != nil {
		t.Fatalf("insert competition: %v", err)
	}

	var fixtureID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.fixtures (world_id, competition_id, home_club_id, away_club_id, matchday, scheduled_at, status)
		VALUES ($1, $2, $3, $4, 1, now(), 'completed') RETURNING id`,
		worldID, compID, homeClub, awayClub).Scan(&fixtureID); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}

	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO match.matches (fixture_id, world_id, seed, engine_version, home_score, away_score, status, current_minute, ended_at)
		VALUES ($1, $2, 42, 'v1.4', 2, 1, 'completed', 90, now()) RETURNING id`,
		fixtureID, worldID).Scan(&matchID); err != nil {
		t.Fatalf("insert match: %v", err)
	}

	type feedEvent struct {
		seq, minute int
		typ         string
		clubID      *uuid.UUID
		detail      string
	}
	home, away := &homeClub, &awayClub
	feed := []feedEvent{
		{1, 1, "kickoff", nil, `{"commentary":"We are underway!"}`},
		{2, 12, "goal", home, `{"commentary":"GOAL! a great strike from the home side.","detail":"long range"}`},
		{3, 20, "yellow_card", home, `{"commentary":"Booked for a late challenge."}`},
		{4, 40, "goal", away, `{"commentary":"GOAL! the visitors level it."}`},
		{5, 45, "half_time", nil, `{"commentary":"Half-time."}`},
		{6, 61, "goal", home, `{"commentary":"GOAL! the hosts retake the lead."}`},
		{7, 70, "substitution", home, `{"commentary":"Fresh legs for the home side."}`},
		{8, 78, "injury", home, `{"commentary":"A player is down needing treatment."}`},
		{9, 90, "full_time", nil, `{"commentary":"That is full time."}`},
	}
	for _, e := range feed {
		if _, err := pool.Exec(ctx, `
			INSERT INTO match.match_events (match_id, sequence, minute, event_type, club_id, detail)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
			matchID, e.seq, e.minute, e.typ, e.clubID, e.detail); err != nil {
			t.Fatalf("insert match event %d: %v", e.seq, err)
		}
	}
	return fixtureID, matchID
}

func TestMatchFeedFixtureHeader(t *testing.T) {
	ts, pool := testHTTPServer(t)
	world := testdb.CreateWorld(t, pool, "wf-header")
	testdb.CreateUser(t, pool, "wf@example.com", "s3cret", []testdb.Join{{WorldID: world}})

	home := insertClub(t, pool, world, "Rovers FC")
	away := insertClub(t, pool, world, "United AFC")
	fixtureID, _ := insertCompletedFixture(t, pool, world, home, away)

	cookies := loginManager(t, ts, pool, "wf@example.com")
	client := ts.Client()
	resp := get(t, ts, client, "/api/fixtures/"+fixtureID.String(), cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fixture header = %d, want 200", resp.StatusCode)
	}
	var view struct {
		Fixture struct {
			ID           string `json:"id"`
			WorldID      string `json:"world_id"`
			HomeClubName string `json:"home_club_name"`
			AwayClubName string `json:"away_club_name"`
			Status       string `json:"status"`
		} `json:"fixture"`
		Match *struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			Minute    int    `json:"minute"`
			HomeScore int    `json:"home_score"`
			AwayScore int    `json:"away_score"`
		} `json:"match"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode fixture header: %v", err)
	}
	if view.Fixture.ID != fixtureID.String() || view.Fixture.WorldID != world.String() {
		t.Fatalf("fixture id/world = %s/%s, want %s/%s", view.Fixture.ID, view.Fixture.WorldID, fixtureID, world)
	}
	if view.Fixture.HomeClubName != "Rovers FC" || view.Fixture.AwayClubName != "United AFC" {
		t.Fatalf("club names = %q / %q, want Rovers FC / United AFC", view.Fixture.HomeClubName, view.Fixture.AwayClubName)
	}
	if view.Match == nil {
		t.Fatal("match must be present for a kicked-off fixture")
	}
	if view.Match.Status != "completed" || view.Match.Minute != 90 || view.Match.HomeScore != 2 || view.Match.AwayScore != 1 {
		t.Fatalf("match view = %+v, want completed 90' 2-1", *view.Match)
	}
}

func TestMatchFeedEventsEndpoint(t *testing.T) {
	ts, pool := testHTTPServer(t)
	world := testdb.CreateWorld(t, pool, "wf-events")
	testdb.CreateUser(t, pool, "wf@example.com", "s3cret", []testdb.Join{{WorldID: world}})

	home := insertClub(t, pool, world, "Rovers FC")
	away := insertClub(t, pool, world, "United AFC")
	_, matchID := insertCompletedFixture(t, pool, world, home, away)

	cookies := loginManager(t, ts, pool, "wf@example.com")
	client := ts.Client()
	resp := get(t, ts, client, "/api/matches/"+matchID.String()+"/events", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("match events = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Events []struct {
			Sequence int             `json:"sequence"`
			Minute   int             `json:"minute"`
			Type     string          `json:"type"`
			ClubID   *string         `json:"club_id"`
			Detail   json.RawMessage `json:"detail"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(body.Events) != 9 {
		t.Fatalf("events = %d, want 9", len(body.Events))
	}
	for i, e := range body.Events {
		if want := i + 1; e.Sequence != want {
			t.Fatalf("events[%d].sequence = %d, want %d (ordered feed)", i, e.Sequence, want)
		}
	}
	if body.Events[0].Type != "kickoff" || body.Events[8].Type != "full_time" {
		t.Fatalf("first/last = %s / %s, want kickoff / full_time", body.Events[0].Type, body.Events[8].Type)
	}
	var detail struct {
		Commentary string `json:"commentary"`
	}
	if err := json.Unmarshal(body.Events[1].Detail, &detail); err != nil || detail.Commentary == "" {
		t.Fatalf("detail commentary must be a JSON object with a commentary line (got %s, err %v)", body.Events[1].Detail, err)
	}
	if body.Events[1].ClubID == nil || *body.Events[1].ClubID != home.String() {
		t.Fatalf("goal club_id = %v, want %s", body.Events[1].ClubID, home)
	}

	// Unauthenticated callers are rejected by requireAuth.
	if resp := get(t, ts, client, "/api/matches/"+matchID.String()+"/events", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated events = %d, want 401", resp.StatusCode)
	}
}

func TestMatchFeedScopedToCallerWorld(t *testing.T) {
	ts, pool := testHTTPServer(t)
	world := testdb.CreateWorld(t, pool, "wf-a")
	other := testdb.CreateWorld(t, pool, "wf-b")
	testdb.CreateUser(t, pool, "wf@example.com", "s3cret", []testdb.Join{{WorldID: world}})
	testdb.CreateUser(t, pool, "other@example.com", "s3cret", []testdb.Join{{WorldID: other}})

	home := insertClub(t, pool, world, "Rovers FC")
	away := insertClub(t, pool, world, "United AFC")
	fixtureID, matchID := insertCompletedFixture(t, pool, world, home, away)

	cookies := loginManager(t, ts, pool, "other@example.com")
	client := ts.Client()

	if resp := get(t, ts, client, "/api/fixtures/"+fixtureID.String(), cookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign fixture header = %d, want 404", resp.StatusCode)
	}
	if resp := get(t, ts, client, "/api/matches/"+matchID.String()+"/events", cookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign match events = %d, want 404", resp.StatusCode)
	}
}
