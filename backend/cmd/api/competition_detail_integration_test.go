//go:build integration

package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/testdb"
)

// TestHTTPAdminCompetitionDetail covers the IM15 admin dossier endpoint: the
// discriminated league/cup response, non-admin denial, and 404 on an unknown
// competition.
func TestHTTPAdminCompetitionDetail(t *testing.T) {
	ts, pool := testHTTPServer(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	client := ts.Client()

	worldID := uuid.New()
	countryID := uuid.New()
	leagueID := uuid.New()
	cupID := uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO world.worlds (id, name, status) VALUES ($1, 'Detail Town', 'active')`, worldID); err != nil {
		t.Fatalf("insert world: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO world.countries (id, world_id, code, name) VALUES ($1, $2, 'eng', 'England')`, countryID, worldID); err != nil {
		t.Fatalf("insert country: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.competitions
			(id, world_id, country_id, name, competition_type, reputation, status, tier, team_count)
		VALUES ($1, $2, $3, 'Premier', 'league', 85, 'active', 1, 4)`, leagueID, worldID, countryID); err != nil {
		t.Fatalf("insert league: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.competition_rules (competition_id, format)
		VALUES ($1, 'round_robin')`, leagueID); err != nil {
		t.Fatalf("insert league rules: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.competitions
			(id, world_id, country_id, name, competition_type, status)
		VALUES ($1, $2, $3, 'National Cup', 'domestic_cup', 'active')`, cupID, worldID, countryID); err != nil {
		t.Fatalf("insert cup: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO competition.competition_rules (competition_id, format, final_date_mode)
		VALUES ($1, 'knockout', 'calculated')`, cupID); err != nil {
		t.Fatalf("insert cup rules: %v", err)
	}

	admin := testdb.CreateUser(t, pool, "detailadmin@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, admin)
	adminCookies := login(t, ts, client, "detailadmin@example.com", "s3cret")

	// League dossier.
	resp := get(t, ts, client, "/api/admin/competitions/"+leagueID.String()+"/detail", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("league detail = %d, want 200", resp.StatusCode)
	}
	m := decodeMap(t, resp)
	if m["competition_type"] != "league" {
		t.Fatalf("competition_type = %v, want league", m["competition_type"])
	}
	if m["name"] != "Premier" || m["team_count"] != float64(4) {
		t.Fatalf("league base = name %v team_count %v, want Premier/4", m["name"], m["team_count"])
	}
	if m["league"] == nil || m["cup"] != nil {
		t.Fatalf("discriminator: league=%v cup=%v", m["league"], m["cup"])
	}
	league := m["league"].(map[string]any)
	if league["movement"] == nil || len(league["standings"].([]any)) != 0 {
		t.Fatalf("league dossier = %+v, want empty movement/standings", league)
	}
	country := m["country"].(map[string]any)
	if country["code"] != "eng" || country["name"] != "England" {
		t.Fatalf("country = %+v, want eng/England", country)
	}

	// Cup dossier (discriminated union, other branch).
	resp = get(t, ts, client, "/api/admin/competitions/"+cupID.String()+"/detail", adminCookies)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cup detail = %d, want 200", resp.StatusCode)
	}
	m = decodeMap(t, resp)
	if m["competition_type"] != "domestic_cup" {
		t.Fatalf("competition_type = %v, want domestic_cup", m["competition_type"])
	}
	if m["cup"] == nil || m["league"] != nil {
		t.Fatalf("discriminator: cup=%v league=%v", m["cup"], m["league"])
	}

	// Unknown competition -> 404.
	resp = get(t, ts, client, "/api/admin/competitions/"+uuid.New().String()+"/detail", adminCookies)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown detail = %d, want 404", resp.StatusCode)
	}

	// Non-admin is denied by the admin gate.
	plain := testdb.CreateUser(t, pool, "detailplain@example.com", "s3cret", nil)
	plainCookies := login(t, ts, client, "detailplain@example.com", "s3cret")
	resp = get(t, ts, client, "/api/admin/competitions/"+leagueID.String()+"/detail", plainCookies)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin detail = %d, want 403", resp.StatusCode)
	}
	_ = plain
}
