//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/touchline/backend/internal/testdb"
)

// TestHTTPClubNameParts covers the admin club-name-pool surface (S04 data-driven
// pools): listing, adding (upsert), and removing global stems/suffixes.
func TestHTTPClubNameParts(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedClubNameParts(t, pool)
	client := ts.Client()

	w := testdb.CreateWorld(t, pool, "W-POOLS")
	admin := testdb.CreateUser(t, pool, "poolsadmin@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	testdb.MakeAdmin(t, pool, admin)
	plain := testdb.CreateUser(t, pool, "poolsplain@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	_ = plain
	adminCookies := login(t, ts, client, "poolsadmin@example.com", "s3cret")
	plainCookies := login(t, ts, client, "poolsplain@example.com", "s3cret")

	// Non-admins are forbidden.
	if r := get(t, ts, client, "/api/admin/club-name-parts", plainCookies); r.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin list = %d, want 403", r.StatusCode)
	}
	if r := post(t, ts, client, "/api/admin/club-name-parts",
		`{"kind":"stem","value":"Atlas"}`, plainCookies); r.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin add = %d, want 403", r.StatusCode)
	}

	// List returns seeded pools.
	var list struct {
		Stems    []string `json:"stems"`
		Suffixes []string `json:"suffixes"`
	}
	r := get(t, ts, client, "/api/admin/club-name-parts", adminCookies)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("admin list = %d, want 200", r.StatusCode)
	}
	if err := decodeBody(t, r, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Stems) == 0 || len(list.Suffixes) == 0 {
		t.Fatalf("empty pools after seed: %v / %v", list.Stems, list.Suffixes)
	}

	// Validate body errors.
	if r := post(t, ts, client, "/api/admin/club-name-parts",
		`{"kind":"prefix","value":"X"}`, adminCookies); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad kind = %d, want 400", r.StatusCode)
	}
	if r := post(t, ts, client, "/api/admin/club-name-parts",
		`{"kind":"stem","value":"   "}`, adminCookies); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank value = %d, want 400", r.StatusCode)
	}

	// Add (201), re-add is a no-op upsert (still 201), then present in list.
	if r := post(t, ts, client, "/api/admin/club-name-parts",
		`{"kind":"suffix","value":"Wanderers"}`, adminCookies); r.StatusCode != http.StatusCreated {
		t.Fatalf("add = %d, want 201", r.StatusCode)
	}
	if r := post(t, ts, client, "/api/admin/club-name-parts",
		`{"kind":"suffix","value":"Wanderers"}`, adminCookies); r.StatusCode != http.StatusCreated {
		t.Fatalf("re-add = %d, want 201", r.StatusCode)
	}
	wantSuffixes(t, ts, client, adminCookies, "Wanderers")

	// Remove, then absent from list; removing again still succeeds.
	if r := del(t, ts, client, "/api/admin/club-name-parts/suffix/Wanderers", adminCookies); r.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", r.StatusCode)
	}
	unwantSuffixes(t, ts, client, adminCookies, "Wanderers")
	if r := del(t, ts, client, "/api/admin/club-name-parts/suffix/Wanderers", adminCookies); r.StatusCode != http.StatusNoContent {
		t.Fatalf("remove absent = %d, want 204", r.StatusCode)
	}

	// Bad kind on the DELETE path.
	if r := del(t, ts, client, "/api/admin/club-name-parts/prefix/Wanderers", adminCookies); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("remove bad kind = %d, want 400", r.StatusCode)
	}
}

func wantSuffixes(t *testing.T, ts *httptest.Server, client *http.Client, cookies, want string) {
	t.Helper()
	list := listPools(t, ts, client, cookies)
	for _, s := range list.Suffixes {
		if s == want {
			return
		}
	}
	t.Fatalf("suffix %q not in list: %v", want, list.Suffixes)
}

func unwantSuffixes(t *testing.T, ts *httptest.Server, client *http.Client, cookies, notWant string) {
	t.Helper()
	list := listPools(t, ts, client, cookies)
	for _, s := range list.Suffixes {
		if s == notWant {
			t.Fatalf("suffix %q still present: %v", notWant, list.Suffixes)
		}
	}
}

func listPools(t *testing.T, ts *httptest.Server, client *http.Client, cookies string) struct {
	Stems    []string `json:"stems"`
	Suffixes []string `json:"suffixes"`
} {
	t.Helper()
	var out struct {
		Stems    []string `json:"stems"`
		Suffixes []string `json:"suffixes"`
	}
	r := get(t, ts, client, "/api/admin/club-name-parts", cookies)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("list = %d, want 200", r.StatusCode)
	}
	if err := decodeBody(t, r, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func decodeBody(t *testing.T, r *http.Response, v any) error {
	t.Helper()
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func del(t *testing.T, ts *httptest.Server, client *http.Client, path string, cookies string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("delete req: %v", err)
	}
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	return resp
}
