//go:build integration

package main

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/touchline/backend/internal/transfertest"
)

func TestHTTPBoardRoundTrip(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	const email = "board-http-owner@example.com"
	tw := transfertest.Provision(t, pool, "board-http", email)
	_ = tw
	cookies := loginManager(t, ts, pool, email)

	// Anonymous access is rejected.
	if resp := get(t, ts, client, "/api/managers/me/board", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous board = %d, want 401", resp.StatusCode)
	}

	// Board view returns confidence, the factor snapshot, and 4 mandates.
	resp := get(t, ts, client, "/api/managers/me/board", cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("board = %d, want 200 (body: %v)", code, resp.Status)
	}
	body := decodeTransferBody(t, resp)
	confidence, _ := body["confidence"].(float64)
	if confidence < 0 || confidence > 100 {
		t.Fatalf("confidence = %v, want [0,100]", body["confidence"])
	}
	snap, ok := body["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("board body = %v", body)
	}
	total, _ := snap["total_score"].(float64)
	if int(total) != int(confidence) {
		t.Errorf("snapshot total %v != confidence %v", total, confidence)
	}
	mandates, ok := body["mandates"].([]any)
	if !ok || len(mandates) != 4 {
		t.Fatalf("board mandates = %v, want 4", body["mandates"])
	}
	first := mandates[0].(map[string]any)
	mandateID := first["id"].(string)
	currentFinish := first["target_value"].(string)
	if first["target_type"] != "league_finish" {
		t.Fatalf("first mandate = %v, want league_finish", first)
	}

	// In-window softening is accepted.
	var newFinish string
	fmt.Sscanf(currentFinish, "%s", &newFinish)
	newFinish = fmt.Sprintf("%d", atoi(newFinish)+2)
	resp = post(t, ts, client, "/api/managers/me/board/mandates/"+mandateID+"/negotiate",
		fmt.Sprintf(`{"target_value":%q}`, newFinish), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("negotiate = %d, want 200", code)
	}
	respBody := decodeTransferBody(t, resp)
	if mandate, ok := respBody["mandate"].(map[string]any); !ok || mandate["status"] != "agreed" || mandate["target_value"] != newFinish {
		t.Fatalf("negotiated mandate = %v", respBody["mandate"])
	}

	// Off-window proposal → 400.
	resp = post(t, ts, client, "/api/managers/me/board/mandates/"+mandateID+"/negotiate",
		`{"target_value":"18"}`, cookies)
	if code := resp.StatusCode; code != http.StatusBadRequest {
		t.Fatalf("off-window negotiate = %d, want 400", code)
	}

	// Non-negotiable mandate type → 400 (financial/strategic).
	for _, m := range mandates {
		mm := m.(map[string]any)
		targetType := mm["target_type"].(string)
		if targetType == "league_finish" || targetType == "points_target" {
			continue
		}
		resp = post(t, ts, client, "/api/managers/me/board/mandates/"+mm["id"].(string)+"/negotiate",
			`{"target_value":"0"}`, cookies)
		if code := resp.StatusCode; code != http.StatusBadRequest {
			t.Fatalf("non-negotiable negotiate = %d, want 400", code)
		}
		break
	}

	// Unknown mandate id → 404.
	resp = post(t, ts, client, "/api/managers/me/board/mandates/00000000-0000-4000-8000-000000000999/negotiate",
		`{"target_value":"5"}`, cookies)
	if code := resp.StatusCode; code != http.StatusNotFound {
		t.Fatalf("unknown mandate = %d, want 404", code)
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}
