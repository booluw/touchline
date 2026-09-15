//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalauth "github.com/touchline/backend/internal/auth"
	internalboard "github.com/touchline/backend/internal/board"
	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
	internalfinance "github.com/touchline/backend/internal/finance"
	internalform "github.com/touchline/backend/internal/form"
	"github.com/touchline/backend/internal/httpapi"
	internalmanager "github.com/touchline/backend/internal/manager"
	internalmatch "github.com/touchline/backend/internal/match"
	internalplayer "github.com/touchline/backend/internal/player"
	internalsocial "github.com/touchline/backend/internal/social"
	internalsquad "github.com/touchline/backend/internal/squad"
	internaltactics "github.com/touchline/backend/internal/tactics"
	"github.com/touchline/backend/internal/testdb"
	internaltraining "github.com/touchline/backend/internal/training"
	internaltransfer "github.com/touchline/backend/internal/transfer"
	internalworld "github.com/touchline/backend/internal/world"
	pkgauth "github.com/touchline/backend/pkg/auth"
	"github.com/touchline/backend/pkg/realtime"
)

// newTestServer builds a server wired like main(): real services, a realtime
// hub with an in-process broker, and its pooled Postgres test DB. WS tests use
// it directly to reach the hub; REST tests wrap it below.
func newTestServer(t *testing.T) (*httpapi.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)

	cfg := pkgauth.JWTConfig{Secret: "api-integration-secret", AccessTTL: time.Hour, RefreshTTL: 30 * 24 * time.Hour}

	broker := realtime.NewLocalBroker()
	hub := realtime.NewHub(broker,
		realtime.WithOriginPatterns(httpapi.OriginHostPattern("http://localhost:3000")))
	go func() { _ = hub.Run(context.Background()) }()
	t.Cleanup(func() { _ = hub.Close() })

	transfersSvc := internaltransfer.NewService(pool, nil)
	playersSvc := internalplayer.NewService(pool, nil, transfersSvc)
	transfersSvc.WithPlayerLifecycle(playersSvc)
	matchSvc := internalmatch.NewService(pool, nil, internalsquad.NewStore(pool), internalform.NewStore(pool))
	matchSvc.WithPlayers(playersSvc)
	socialSvc := internalsocial.NewService(pool, nil)
	socialSvc.WithRealtime(broker)
	matchSvc.WithSocial(socialSvc)

	s := httpapi.New(httpapi.Options{
		Auth:          internalauth.NewService(pool, cfg),
		World:         internalworld.NewService(pool, nil),
		Manager:       internalmanager.NewService(pool, nil),
		Club:          internalclub.NewService(pool),
		Bootstrap:     internalbootstrap.NewService(pool, nil),
		Competition:   internalcompetition.NewService(pool, nil),
		Match:         matchSvc,
		Tactics:       internaltactics.NewService(pool, nil, internalsquad.NewStore(pool)),
		Training:      internaltraining.NewService(pool, nil),
		Transfers:     transfersSvc,
		Board:         internalboard.NewService(pool, nil, internalmanager.NewService(pool, nil)),
		Finance:       internalfinance.NewService(pool, nil),
		Player:        playersSvc,
		Social:        socialSvc,
		JWT:           cfg,
		Pool:          pool,
		CookiesSecure: false,
		AppOrigin:     "http://localhost:3000",
		Hub:           hub,
	})
	return s, pool
}

// testHTTPServer boots the real Gin router against a migrated, truncated DB
// and returns the test server plus its pool (for fixtures).
func testHTTPServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	s, pool := newTestServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, pool
}

func TestHTTPLoginSetsCookiesAndProtectsDashboard(t *testing.T) {
	ts, pool := testHTTPServer(t)
	w := testdb.CreateWorld(t, pool, "W-HTTP-A")
	userID := testdb.CreateUser(t, pool, "http@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	testdb.MakeAdmin(t, pool, userID)
	client := ts.Client()

	// 401 without any session.
	if resp := get(t, ts, client, "/api/dashboard", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard = %d, want 401", resp.StatusCode)
	}

	// Login sets an httpOnly cookie pair.
	resp := post(t, ts, client, "/api/auth/login", `{"email":"http@example.com","password":"s3cret"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d, want 200", resp.StatusCode)
	}
	cookies := cookieMap(resp)
	if cookies["access_token"] == "" || cookies["refresh_token"] == "" {
		t.Fatalf("login must set access+refresh cookies, got %v", cookieNames(resp))
	}
	for _, c := range resp.Cookies() {
		if !c.HttpOnly {
			t.Errorf("cookie %s must be httpOnly", c.Name)
		}
	}

	// Protected dashboard with ONLY the access cookie (no Authorization header).
	resp = get(t, ts, client, "/api/dashboard", cookieHeader(cookies))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated dashboard = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	for _, k := range []string{"urgent", "important", "interesting"} {
		list, ok := body[k].([]any)
		if !ok {
			t.Errorf("dashboard.%s missing or not a list", k)
			continue
		}
		if len(list) != 0 {
			t.Errorf("dashboard.%s = %v, want empty stub", k, list)
		}
	}

	// Refresh rotates the pair; the old refresh cookie is then rejected.
	resp = post(t, ts, client, "/api/auth/refresh", "", cookieHeader(cookies))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d, want 200", resp.StatusCode)
	}
	refreshed := cookieMap(resp)
	if refreshed["access_token"] == "" || refreshed["refresh_token"] == "" {
		t.Fatal("refresh must issue a new cookie pair")
	}

	resp = post(t, ts, client, "/api/auth/refresh", "", cookieHeader(cookies))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused old refresh cookie = %d, want 401 (rotation)", resp.StatusCode)
	}

	// And the fresh refresh token still works.
	resp = post(t, ts, client, "/api/auth/refresh", "", cookieHeader(refreshed))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new refresh cookie = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPLogin_RejectsBadCredentials(t *testing.T) {
	ts, pool := testHTTPServer(t)
	w := testdb.CreateWorld(t, pool, "W-HTTP-B")
	testdb.CreateUser(t, pool, "http2@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	client := ts.Client()

	if resp := post(t, ts, client, "/api/auth/login", `{"email":"http2@example.com","password":"nope"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad password = %d, want 401", resp.StatusCode)
	}
	if resp := post(t, ts, client, "/api/auth/login", `{"email":"ghost@example.com","password":"s3cret"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown email = %d, want 401", resp.StatusCode)
	}
}

func TestHTTPLogin_Validation(t *testing.T) {
	ts, _ := testHTTPServer(t)
	client := ts.Client()

	if resp := post(t, ts, client, "/api/auth/login", `{}`, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty payload = %d, want 400", resp.StatusCode)
	}
	if resp := post(t, ts, client, "/api/auth/login", `not-json`, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json = %d, want 400", resp.StatusCode)
	}
}

// login returns the cookie header for an account that resolves to a single
// session context (no world picker).
func login(t *testing.T, ts *httptest.Server, client *http.Client, email, password string) string {
	t.Helper()
	resp := post(t, ts, client, "/api/auth/login",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, password), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d, want 200", email, resp.StatusCode)
	}
	return cookieHeader(cookieMap(resp))
}

// loginManager returns a manager-scoped session cookie header for the named
// user, bypassing the login endpoint. This keeps world-scoped manager reads
// isolated from the account-resolution rules: fixtures mint a manager row and
// token directly instead of going through OPD-15(4) login.
func loginManager(t *testing.T, ts *httptest.Server, pool *pgxpool.Pool, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID, managerID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM auth.users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("resolve user %s: %v", email, err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM manager.managers WHERE user_id = $1`, userID).Scan(&managerID); err != nil {
		t.Fatalf("resolve manager for %s: %v", email, err)
	}
	return managerCookies(t, ts, pool, managerID, userID)
}

// managerCookies mints a manager session cookie header from a manager + user id.
func managerCookies(t *testing.T, ts *httptest.Server, pool *pgxpool.Pool, managerID, userID uuid.UUID) string {
	t.Helper()
	var worldID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT world_id FROM manager.managers WHERE id = $1`, managerID).Scan(&worldID); err != nil {
		t.Fatalf("manager world: %v", err)
	}
	cfg := pkgauth.JWTConfig{Secret: "api-integration-secret", AccessTTL: time.Hour, RefreshTTL: 30 * 24 * time.Hour}
	pair, err := pkgauth.GenerateTokenPair(cfg, pkgauth.ManagerIdentity{ManagerID: managerID, WorldID: worldID, UserID: userID})
	if err != nil {
		t.Fatalf("mint manager tokens: %v", err)
	}
	return cookieHeader(map[string]string{"access_token": pair.AccessToken, "refresh_token": pair.RefreshToken})
}

func TestHTTPAdminWorldLifecycle(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()

	w := testdb.CreateWorld(t, pool, "W-ADMIN-A")
	admin := testdb.CreateUser(t, pool, "admin@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	testdb.MakeAdmin(t, pool, admin)
	adminCookies := login(t, ts, client, "admin@example.com", "s3cret")

	// A non-admin account with a joined world now logs in as a manager
	// (OPD-15(4)); an account with NO joined world still cannot obtain a
	// session (ErrNoManager) — that is the only login 403 an outsider reaches.
	testdb.CreateUser(t, pool, "plain@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	if resp := post(t, ts, client, "/api/auth/login",
		`{"email":"plain@example.com","password":"s3cret"}`, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("plain manager login = %d, want 200", resp.StatusCode)
	}
	testdb.CreateUser(t, pool, "noworld@example.com", "s3cret", nil)
	if resp := post(t, ts, client, "/api/auth/login",
		`{"email":"noworld@example.com","password":"s3cret"}`, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no-world login = %d, want 403", resp.StatusCode)
	}

	// Unauthenticated admin routes are 401.
	if resp := post(t, ts, client, "/api/admin/worlds", `{"name":"anon"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous create world = %d, want 401", resp.StatusCode)
	}

	// Admin creates a world, launches it, and can list/pause/archive.
	resp := post(t, ts, client, "/api/admin/worlds", `{"name":"Created World"}`, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create world = %d, want 201", resp.StatusCode)
	}
	var created map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created world: %v", err)
	}
	worldID, _ := created["id"].(string)
	if worldID == "" || created["status"] != "provisioning" {
		t.Fatalf("created world = %v", created)
	}

	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/status", `{"status":"active"}`, adminCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("launch world = %d, want 200", resp.StatusCode)
	}

	// Admin can tune a runtime cadence; it lands in world_config for the
	// scheduler to pick up on its next sync (S02-03).
	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/config",
		`{"key":"tick.daily_cadence","value":"30 0 * * *"}`, adminCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("set config = %d, want 200", resp.StatusCode)
	}
	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/config",
		`{"key":"tick.daily_cadence","value":"30 0 * * *"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous set config = %d, want 401", resp.StatusCode)
	}
	var daily string
	if err := pool.QueryRow(context.Background(),
		`SELECT config_value::text FROM world.world_config WHERE world_id = $1 AND config_key = 'tick.daily_cadence'`, worldID,
	).Scan(&daily); err != nil {
		t.Fatalf("read daily cadence: %v", err)
	}
	if daily != `"30 0 * * *"` {
		t.Fatalf("daily cadence stored as %s", daily)
	}
	if resp := post(t, ts, client, "/api/admin/worlds/"+uuid.Nil.String()+"/config",
		`{"key":"tick.daily_cadence","value":"0 0 * * *"}`, adminCookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("set config on missing world = %d, want 404", resp.StatusCode)
	}

	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/status", `{"status":"archived"}`, adminCookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("archive world = %d, want 200", resp.StatusCode)
	}
	// Invalid transition surfaces as 400.
	if resp := post(t, ts, client, "/api/admin/worlds/"+worldID+"/status", `{"status":"active"}`, adminCookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("launch archived world = %d, want 400", resp.StatusCode)
	}
}

func TestHTTPJobOfferFlow(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()

	w := testdb.CreateWorld(t, pool, "W-OFFERS")
	clubID, _ := testdb.CreateClubWithAIManager(t, pool, w)
	candidate := testdb.CreateUser(t, pool, "candidate@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	testdb.MakeAdmin(t, pool, candidate) // only admins may log in during phase 1
	admin := testdb.CreateUser(t, pool, "admin2@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	testdb.MakeAdmin(t, pool, admin)

	adminCookies := login(t, ts, client, "admin2@example.com", "s3cret")

	var candManager uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE user_id = $1 AND world_id = $2`, candidate, w).Scan(&candManager); err != nil {
		t.Fatalf("candidate manager id: %v", err)
	}

	// Admin issues an offer on the AI club's behalf.
	offerBody := fmt.Sprintf(`{"club_id":%q,"manager_id":%q}`, clubID, candManager)
	resp := post(t, ts, client, "/api/admin/offers", offerBody, adminCookies)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("admin create offer = %d, want 201", resp.StatusCode)
	}
	var created map[string]any
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode offer: %v", err)
	}
	offerID, _ := created["id"].(string)
	if offerID == "" {
		t.Fatalf("offer = %v", created)
	}

	// The candidate logs in and sees the pending offer in their inbox.
	candidateCookies := login(t, ts, client, "candidate@example.com", "s3cret")
	resp = get(t, ts, client, "/api/managers/me/offers", candidateCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list offers = %d, want 200", resp.StatusCode)
	}
	raw, _ = io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), offerID) {
		t.Fatalf("offers response missing offer: %s", raw)
	}

	// Accept it as the candidate.
	resp = post(t, ts, client, "/api/offers/"+offerID+"/accept", "", candidateCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept offer = %d, want 200", resp.StatusCode)
	}

	// Resign frees the manager.
	resp = post(t, ts, client, "/api/managers/me/resign", "", candidateCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resign = %d, want 200", resp.StatusCode)
	}
	// Resigning again (jobless) is a 409 conflict.
	resp = post(t, ts, client, "/api/managers/me/resign", "", candidateCookies)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("double resign = %d, want 409", resp.StatusCode)
	}
}

func post(t *testing.T, ts *httptest.Server, client *http.Client, path, body, cookieHeader string) *http.Response {
	t.Helper()
	resp := do(t, ts, client, http.MethodPost, path, body, cookieHeader)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func get(t *testing.T, ts *httptest.Server, client *http.Client, path, cookieHeader string) *http.Response {
	t.Helper()
	resp := do(t, ts, client, http.MethodGet, path, "", cookieHeader)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func put(t *testing.T, ts *httptest.Server, client *http.Client, path, body, cookieHeader string) *http.Response {
	t.Helper()
	resp := do(t, ts, client, http.MethodPut, path, body, cookieHeader)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func do(t *testing.T, ts *httptest.Server, client *http.Client, method, path, body, cookieHeader string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func cookieMap(resp *http.Response) map[string]string {
	out := map[string]string{}
	for _, c := range resp.Cookies() {
		out[c.Name] = c.Value
	}
	return out
}

func cookieNames(resp *http.Response) []string {
	var out []string
	for _, c := range resp.Cookies() {
		out = append(out, c.Name)
	}
	return out
}

func cookieHeader(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for k, v := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, "; ")
}
