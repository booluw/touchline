//go:build integration

// HTTP integration coverage for the S06-01 transfer market routes:
// POST/GET /api/transfers/listings, GET /api/transfers/listings/:id,
// POST /api/transfers/listings/:id/withdraw, POST /api/transfers/bids,
// GET /api/transfers/bids and POST /api/transfers/bids/:id/respond. Auth is the
// standard manager cookie; the human fixture lists a player (AI clubs bid),
// places a direct full-price bid on an AI club's player (auto-accepted and
// completed), rejects an AI offer, and withdraws the listing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/transfer"
	"github.com/touchline/backend/internal/transfertest"
)

func decodeTransferBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode body: %v (%s)", err, raw)
	}
	return m
}

func stringField(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, _ := m[key].(string)
	return v
}

// transferValuation mirrors the engine's exact attribute SQL to compute the
// deterministic market value a full-price bid must clear.
func transferValuation(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID) int64 {
	t.Helper()
	var a transfer.PlayerAttrs
	if err := pool.QueryRow(context.Background(), `
		SELECT pl.primary_position,
		       (CURRENT_DATE - pp.date_of_birth) / 365,
		       COALESCE((SELECT (ct.end_date - CURRENT_DATE) FROM player.contracts ct
		                  WHERE ct.player_id = pl.id AND ct.status = 'active'
		                  ORDER BY ct.start_date DESC LIMIT 1), 0),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'technical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'physical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'mental'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'tactical'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'goalkeeping'), 50),
		       COALESCE((SELECT AVG(pa.value)::int FROM player.player_attributes pa
		                  WHERE pa.player_id = pl.id AND pa.attribute_category = 'positional'), 50)
		FROM player.players pl
		JOIN person.people pp ON pp.id = pl.person_id
		WHERE pl.id = $1`, playerID).Scan(
		&a.Position, &a.Age, &a.ContractEndDays,
		&a.Attributes.Technical, &a.Attributes.Physical, &a.Attributes.Mental,
		&a.Attributes.Tactical, &a.Attributes.Goalkeeping, &a.Attributes.Positional,
	); err != nil {
		t.Fatalf("read attrs of %s: %v", playerID, err)
	}
	return transfer.Valuation(a)
}

func pickClubPlayer(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id LIMIT 1`,
		clubID).Scan(&id); err != nil {
		t.Fatalf("pick player of %s: %v", clubID, err)
	}
	return id
}

func TestHTTPTransferMarketRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	const email = "transfer-http-owner@example.com"
	tw := transfertest.Provision(t, pool, "tr-http", email)
	cookies := loginManager(t, ts, pool, email)

	// Anonymous access is rejected.
	if resp := get(t, ts, client, "/api/transfers/listings", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous listings = %d, want 401", resp.StatusCode)
	}

	// Human lists a player; AI clubs immediately bid in the same transaction.
	player := pickClubPlayer(t, pool, tw.HumanClub)
	resp := post(t, ts, client, "/api/transfers/listings",
		fmt.Sprintf(`{"player_id":%q,"listing_type":"open_to_offers"}`, player), cookies)
	if code := resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("create listing = %d, want 201", code)
	}
	created := decodeTransferBody(t, resp)
	listingID := stringField(t, created, "id")
	if listingID == "" {
		t.Fatalf("listing = %v", created)
	}
	if bidCount, ok := created["bid_count"].(float64); !ok || int(bidCount) != 2 {
		t.Fatalf("listing bid_count = %v, want 2 AI bids", created["bid_count"])
	}
	if created["status"] != "active" {
		t.Fatalf("listing status = %v", created["status"])
	}

	// Market screens reflect the fresh listing.
	resp = get(t, ts, client, "/api/transfers/listings", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("list listings = %d, want 200", code)
	}
	listed := decodeTransferBody(t, resp)
	if arr, ok := listed["listings"].([]any); !ok || len(arr) < 1 {
		t.Fatalf("listings body = %v", listed)
	}
	resp = get(t, ts, client, "/api/transfers/listings/"+listingID, cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("get listing = %d, want 200", code)
	}
	if got := stringField(t, decodeTransferBody(t, resp), "id"); got != listingID {
		t.Fatalf("get listing id = %s, want %s", got, listingID)
	}
	// Unknown listing collapses to 404 (existence never leaks).
	if resp = get(t, ts, client, "/api/transfers/listings/"+uuid.New().String(), cookies); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown listing = %d, want 404", resp.StatusCode)
	}

	// Direct full-price bid on an AI club's player: the AI seller accepts
	// immediately and the HTTP response carries the completed transfer.
	aiPlayer := pickClubPlayer(t, pool, tw.AIOneClub)
	target := int64(float64(transferValuation(t, pool, aiPlayer)) * 1.10)
	resp = post(t, ts, client, "/api/transfers/bids",
		fmt.Sprintf(`{"player_id":%q,"terms":{"fee":%d,"weekly_wage":250000,"contract_length_months":24,"signing_bonus":1000000,"sell_on_percentage":15,"buy_back_amount":500000000}}`,
			aiPlayer, target), cookies)
	if code := resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("place bid = %d, want 201", code)
	}
	bidResp := decodeTransferBody(t, resp)
	if got := stringField(t, mustNested(t, bidResp, "bid"), "status"); got != transfer.BidStatusAccepted {
		t.Fatalf("bid status = %s, want accepted", got)
	}
	ct, ok := bidResp["transfer"].(map[string]any)
	if !ok || ct["to_club_id"] != tw.HumanClub.String() {
		t.Fatalf("completed transfer missing from response: %v", bidResp["transfer"])
	}

	// Bids inbox: the human is the seller on the acceptance thread AND the
	// buyer on the completed one.
	resp = get(t, ts, client, "/api/transfers/bids", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("list bids = %d, want 200", code)
	}
	inbox := decodeTransferBody(t, resp)
	if incoming, ok := inbox["incoming"].([]any); !ok || len(incoming) < 1 {
		t.Fatalf("incoming bids = %v", inbox["incoming"])
	}

	// The human rejects one AI offer on the listed player.
	resp = get(t, ts, client, "/api/transfers/listings/"+listingID, cookies)
	rejectBidID := stringField(t, mustNested(t, decodeTransferBody(t, resp), "latest_bid"), "id")
	if rejectBidID == "" {
		t.Fatalf("no latest_bid on listing after creation")
	}
	resp = post(t, ts, client, "/api/transfers/bids/"+rejectBidID+"/respond", `{"action":"reject"}`, cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("respond reject = %d, want 200", code)
	}
	if got := stringField(t, mustNested(t, decodeTransferBody(t, resp), "bid"), "status"); got != transfer.BidStatusRejected {
		t.Fatalf("rejected bid status = %s", got)
	}

	// Withdrawing the listing expires its remaining open bids.
	resp = post(t, ts, client, "/api/transfers/listings/"+listingID+"/withdraw", "", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("withdraw = %d, want 200", code)
	}
	if got := stringField(t, decodeTransferBody(t, resp), "status"); got != "withdrawn" {
		t.Fatalf("withdrawn status = %s", got)
	}

	// Malformed payloads are 400s.
	if resp = post(t, ts, client, "/api/transfers/listings", `not-json`, cookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad listing json = %d, want 400", resp.StatusCode)
	}
	if resp = post(t, ts, client, "/api/transfers/bids", `{}`, cookies); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty bid = %d, want 400 (no target)", resp.StatusCode)
	}
}

func mustNested(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	inner, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%s missing in %+v", key, m)
	}
	return inner
}
