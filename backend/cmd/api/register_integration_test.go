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

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/testdb"
)

func TestHTTPLogin_WorldPickerAndExplicitPick(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()

	w1 := testdb.CreateWorld(t, pool, "W-PICK-1")
	w2 := testdb.CreateWorld(t, pool, "W-PICK-2")
	testdb.CreateUser(t, pool, "picker@example.com", "s3cret", []testdb.Join{{WorldID: w1}, {WorldID: w2}})

	// First post: the multi-world jobless account gets the picker, no cookies.
	resp := post(t, ts, client, "/api/auth/login", `{"email":"picker@example.com","password":"s3cret"}`, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("picker login = %d, want 200", resp.StatusCode)
	}
	if got := cookieNames(resp); len(got) != 0 {
		t.Fatalf("picker response set cookies %v — must not mint a session", got)
	}
	var body struct {
		Status string `json:"status"`
		Worlds []struct {
			WorldID uuid.UUID `json:"world_id"`
			Name    string    `json:"name"`
		} `json:"worlds"`
	}
	if err := decodeJSON(t, resp, &body); err != nil {
		t.Fatalf("decode picker: %v", err)
	}
	if body.Status != "worlds" || len(body.Worlds) != 2 {
		t.Fatalf("picker = %+v, want status worlds with 2 options", body)
	}

	// Re-post with the chosen world_id -> a real session in that world.
	resp = post(t, ts, client, "/api/auth/login",
		fmt.Sprintf(`{"email":"picker@example.com","password":"s3cret","world_id":%q}`, w2), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("explicit-pick login = %d, want 200", resp.StatusCode)
	}
	cookies := cookieMap(resp)
	if cookies["access_token"] == "" || cookies["refresh_token"] == "" {
		t.Fatal("explicit pick must set session cookies")
	}

	// A world_id this account is not a member of -> 403 ErrNotMember.
	other := testdb.CreateWorld(t, pool, "W-PICK-OTHER")
	resp = post(t, ts, client, "/api/auth/login",
		fmt.Sprintf(`{"email":"picker@example.com","password":"s3cret","world_id":%q}`, other), "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign world_id = %d, want 403", resp.StatusCode)
	}
	if body2 := readBody(t, resp); !strings.Contains(body2, "not a member") {
		t.Fatalf("foreign world error = %s", body2)
	}
}

func TestHTTPRegister_OnboardsWithAutoOffer(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()

	w := testdb.CreateWorld(t, pool, "W-ONBOARD")
	club, _ := testdb.CreateClubWithAIManager(t, pool, w)

	resp := post(t, ts, client, "/api/auth/register",
		`{"email":"broker@example.com","password":"s3cret","display_name":"Broker"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d, want 201 (body: %s)", resp.StatusCode, readBody(t, resp))
	}
	var created struct {
		ID          uuid.UUID `json:"id"`
		Email       string    `json:"email"`
		DisplayName string    `json:"display_name"`
		IsAdmin     bool      `json:"is_admin"`
		World       *struct {
			WorldID uuid.UUID `json:"world_id"`
			Status  string    `json:"status"`
		} `json:"world"`
		Offer *struct {
			ID       uuid.UUID `json:"id"`
			ClubID   uuid.UUID `json:"club_id"`
			Status   string    `json:"status"`
			ClubName string    `json:"club_name"`
		} `json:"offer"`
	}
	if err := decodeJSON(t, resp, &created); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if created.IsAdmin {
		t.Error("registered account must not be an admin")
	}
	if created.World == nil || created.World.WorldID != w || created.World.Status != "active" {
		t.Fatalf("world = %+v, want joined %s active", created.World, w)
	}
	if created.Offer == nil {
		t.Fatal("expected an auto-issued first offer")
	}
	if created.Offer.ClubID != club {
		t.Errorf("offer club = %s, want the seeded AI club %s", created.Offer.ClubID, club)
	}
	if created.Offer.Status != "proposed" {
		t.Errorf("offer status = %s, want proposed", created.Offer.Status)
	}

	// The real (non-minted) login path opens a managing session, and the
	// manager can read the world and accept the auto-offer.
	cookies := login(t, ts, client, "broker@example.com", "s3cret")
	if resp := get(t, ts, client, "/api/clubs", cookies); resp.StatusCode != http.StatusOK {
		t.Fatalf("manager clubs = %d, want 200", resp.StatusCode)
	}

	resp = post(t, ts, client, "/api/offers/"+created.Offer.ID.String()+"/accept", "", cookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept auto-offer = %d, want 200 (body: %s)", resp.StatusCode, readBody(t, resp))
	}

	// The club is now human-managed; the takeovers are recorded.
	ctx := context.Background()
	var humanManaged bool
	if err := pool.QueryRow(ctx,
		`SELECT NOT is_ai_controlled FROM club.clubs WHERE id = $1`, club).Scan(&humanManaged); err != nil {
		t.Fatalf("read club: %v", err)
	}
	if !humanManaged {
		t.Error("club must flip to human-managed after accepting the onboarding offer")
	}
}

func TestHTTPRegister_NoAIClubGivesNoOffer(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()

	// Playable world but no club yet: account + join succeed, the offer is
	// absent (best-effort auto-offer; an admin can offer later).
	w := testdb.CreateWorld(t, pool, "W-NOCLUBS")
	_ = w

	resp := post(t, ts, client, "/api/auth/register",
		`{"email":"unclubbed@example.com","password":"s3cret"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d, want 201 (body: %s)", resp.StatusCode, readBody(t, resp))
	}
	var created struct {
		World *struct {
			WorldID uuid.UUID `json:"world_id"`
		} `json:"world"`
		Offer json.RawMessage `json:"offer"`
	}
	if err := decodeJSON(t, resp, &created); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if created.World == nil {
		t.Fatal("world should still be joined")
	}
	if string(created.Offer) != "null" {
		t.Fatalf("offer = %s, want null with no AI club", created.Offer)
	}
}

func TestHTTPRegister_WorldlessAccount(t *testing.T) {
	ts, _ := testHTTPServer(t) //nolint:errcheck
	client := ts.Client()

	// No playable world: the account is created world-less (offer null), and
	// its login is refused with ErrNoManager until an admin joins it.
	resp := post(t, ts, client, "/api/auth/register",
		`{"email":"worldless@example.com","password":"s3cret"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %d, want 201 (body: %s)", resp.StatusCode, readBody(t, resp))
	}
	if body := readBody(t, resp); !strings.Contains(body, `"world":null`) {
		t.Fatalf("register body = %s, want world:null", body)
	}

	resp = post(t, ts, client, "/api/auth/login",
		`{"email":"worldless@example.com","password":"s3cret"}`, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("worldless login = %d, want 403", resp.StatusCode)
	}
}

func TestHTTPRegister_DuplicateEmail(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()

	w := testdb.CreateWorld(t, pool, "W-DUPE")
	_ = w

	resp := post(t, ts, client, "/api/auth/register",
		`{"email":"taken@example.com","password":"s3cret"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first register = %d, want 201", resp.StatusCode)
	}
	resp = post(t, ts, client, "/api/auth/register",
		`{"email":"taken@example.com","password":"other-pass"}`, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate register = %d, want 409", resp.StatusCode)
	}
}

// readBody drains and returns a response body (also used for error bodies).
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}
