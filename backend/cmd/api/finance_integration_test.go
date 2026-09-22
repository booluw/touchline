//go:build integration

// HTTP integration coverage for the S05-02 club finance reads:
// GET /clubs/:id/finances, GET /clubs/:id/ledger and GET /clubs/:id/contracts.
// All three are ownership-gated (401 anonymous, 403 foreign/world-mismatched,
// 404 nonexistent club), and the finances response is exercised across a wage
// run so the cash/operating-profit movement is visible end-to-end.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/testdb"
)

func decodeFinanceBody(t *testing.T, resp *http.Response) map[string]any {
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

func num(t *testing.T, m map[string]any, key string) float64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%s missing or not a number in %+v", key, m)
	}
	return v
}

func TestHTTPFinancesRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	_, clubID, cookies := tacticsClub(t, ts, pool, "finance-owner@example.com")

	resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/finances", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("finances = %d, want 200", code)
	}
	body := decodeFinanceBody(t, resp)
	if got := num(t, body, "cash"); got != float64(finance.OpeningCapital) {
		t.Fatalf("cash = %v, want %d", got, finance.OpeningCapital)
	}
	if got := num(t, body, "operating_profit"); got != 0 {
		t.Fatalf("operating_profit = %v, want 0", got)
	}
	if body["currency"] != "USD" {
		t.Fatalf("currency = %v, want USD", body["currency"])
	}
	tb, ok := body["transfer_budget"].(map[string]any)
	if !ok || num(t, tb, "allocated") != float64(finance.TransferBudget) {
		t.Fatalf("transfer_budget = %v", body["transfer_budget"])
	}
	wb, ok := body["wage_budget"].(map[string]any)
	if !ok || num(t, wb, "allocated") != float64(finance.WageBudget) {
		t.Fatalf("wage_budget = %v", body["wage_budget"])
	}
	wc, ok := body["wage_commitments"].(map[string]any)
	if !ok || num(t, wc, "count") != 24 {
		t.Fatalf("wage_commitments = %v", body["wage_commitments"])
	}
	factors, ok := body["factors"].([]any)
	if !ok || len(factors) == 0 {
		t.Fatalf("factors missing or empty: %v", body["factors"])
	}

	// Ledger: the genesis credit only.
	resp = get(t, ts, client, "/api/clubs/"+clubID.String()+"/ledger", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("ledger = %d, want 200", code)
	}
	raw, _ := io.ReadAll(resp.Body)
	var ledger []map[string]any
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("decode ledger: %v", err)
	}
	if len(ledger) != 1 {
		t.Fatalf("ledger length = %d, want 1 (genesis)", len(ledger))
	}
	if ledger[0]["entry_type"] != "credit" || ledger[0]["category"] != "other" {
		t.Fatalf("genesis entry wrong: %v", ledger[0])
	}

	// Contracts: the full seeded squad.
	resp = get(t, ts, client, "/api/clubs/"+clubID.String()+"/contracts", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("contracts = %d, want 200", code)
	}
	raw, _ = io.ReadAll(resp.Body)
	var contracts []map[string]any
	if err := json.Unmarshal(raw, &contracts); err != nil {
		t.Fatalf("decode contracts: %v", err)
	}
	if len(contracts) != 24 {
		t.Fatalf("contracts = %d, want 24", len(contracts))
	}
	for _, c := range contracts {
		player, _ := c["player"].(map[string]any)
		if player == nil || player["name"] == "" || num(t, c, "weekly_wage") <= 0 ||
			c["start_date"] == "" || c["end_date"] == "" || c["status"] != "active" {
			t.Fatalf("contract shape wrong: %v", c)
		}
	}
}

func TestHTTPFinancesReflectWageRun(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	worldID, clubID, cookies := tacticsClub(t, ts, pool, "finance-wages@example.com")

	if _, err := finance.NewService(pool, nil).ApplyMonthlyWages(context.Background(), worldID, 41); err != nil {
		t.Fatalf("apply monthly wages: %v", err)
	}

	resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/finances", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("finances = %d, want 200", code)
	}
	body := decodeFinanceBody(t, resp)
	if got := num(t, body, "cash"); got >= float64(finance.OpeningCapital) {
		t.Fatalf("cash = %v, want < %d after a wage run", got, finance.OpeningCapital)
	}
	if got := num(t, body, "operating_profit"); got >= 0 {
		t.Fatalf("operating_profit = %v, want negative (wages only)", got)
	}

	resp = get(t, ts, client, "/api/clubs/"+clubID.String()+"/ledger", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("ledger = %d, want 200", code)
	}
	raw, _ := io.ReadAll(resp.Body)
	var ledger []map[string]any
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatalf("decode ledger: %v", err)
	}
	if len(ledger) != 25 {
		t.Fatalf("ledger length = %d, want 25 (genesis + 24 wages)", len(ledger))
	}
}

func TestHTTPFinancesAuthSemantics(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	_, clubID, cookies := tacticsClub(t, ts, pool, "finance-owner2@example.com")

	// Anonymous reads are rejected on every finance route.
	for _, path := range []string{"finances", "ledger", "contracts"} {
		if resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/"+path, ""); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d, want 401", path, resp.StatusCode)
		}
	}

	// A manager in a different world cannot read this club's books (403).
	worldOther := testdb.CreateWorld(t, pool, "finance-other-world")
	testdb.CreateUser(t, pool, "finance-foreign@example.com", "s3cret", []testdb.Join{{WorldID: worldOther}})
	foreign := loginManager(t, ts, pool, "finance-foreign@example.com")
	for _, path := range []string{"finances", "ledger", "contracts"} {
		if resp := get(t, ts, client, "/api/clubs/"+clubID.String()+"/"+path, foreign); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("foreign %s = %d, want 403", path, resp.StatusCode)
		}
	}

	// Nonexistent club → 404 on every route.
	ghost := uuid.New().String()
	for _, path := range []string{"finances", "ledger", "contracts"} {
		if resp := get(t, ts, client, "/api/clubs/"+ghost+"/"+path, cookies); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("ghost %s = %d, want 404", path, resp.StatusCode)
		}
	}

	// Malformed club id → 400.
	for _, path := range []string{"finances", "ledger", "contracts"} {
		if resp := get(t, ts, client, "/api/clubs/not-a-uuid/"+path, cookies); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("malformed %s = %d, want 400", path, resp.StatusCode)
		}
	}
}
