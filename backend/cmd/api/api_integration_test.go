//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internalauth "github.com/touchline/backend/internal/auth"
	"github.com/touchline/backend/internal/testdb"
	pkgauth "github.com/touchline/backend/pkg/auth"
)

// testHTTPServer boots the real Gin router against a migrated, truncated DB
// and returns the test server plus its pool (for fixtures).
func testHTTPServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)

	cfg := pkgauth.JWTConfig{Secret: "api-integration-secret", AccessTTL: time.Hour, RefreshTTL: 30 * 24 * time.Hour}
	s := &server{
		svc:           internalauth.NewService(pool, cfg),
		jwtCfg:        cfg,
		pool:          pool,
		cookiesSecure: false,
		appOrigin:     "http://localhost:3000",
	}

	ts := httptest.NewServer(s.router())
	t.Cleanup(ts.Close)
	return ts, pool
}

func TestHTTPLoginSetsCookiesAndProtectsDashboard(t *testing.T) {
	ts, pool := testHTTPServer(t)
	w := testdb.CreateWorld(t, pool, "W-HTTP-A")
	testdb.CreateUser(t, pool, "http@example.com", "s3cret", []testdb.Join{{WorldID: w}})
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
