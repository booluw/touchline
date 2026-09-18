//go:build integration

// HTTP integration coverage for the S09-02 dressing-room dynamics endpoint
// (GET /api/clubs/:id/dynamics): ownership gating, the computed read-model
// shape, and surfacing of a recent SQUAD_UNREST_TRIGGERED after a sale.
package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	internalfaction "github.com/touchline/backend/internal/faction"
	"github.com/touchline/backend/internal/transfertest"
)

// TestHTTPGetSquadDynamics covers auth/ownership gating and the read-model
// shape for the owning manager.
func TestHTTPGetSquadDynamics(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "dynamics-owner@example.com"
	tw := transfertest.Provision(t, pool, "dynamics-http", email)
	cookies := loginManager(t, ts, pool, email)

	// Unauthenticated is rejected.
	resp := get(t, ts, client, fmt.Sprintf("/api/clubs/%s/dynamics", tw.HumanClub), "")
	if code := resp.StatusCode; code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dynamics = %d, want 401", code)
	}

	// The owning manager reads the computed model.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/dynamics", tw.HumanClub), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("dynamics = %d, want 200", code)
	}
	body := decodeTransferBody(t, resp)
	tiers, ok := body["tiers"].([]any)
	if !ok || len(tiers) == 0 {
		t.Fatalf("tiers missing/empty: %v", body["tiers"])
	}
	var players int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM player.players WHERE club_id = $1 AND status = 'active'`,
		tw.HumanClub).Scan(&players); err != nil {
		t.Fatalf("count players: %v", err)
	}
	if len(tiers) != players {
		t.Fatalf("tiers = %d, want %d (one per squad member)", len(tiers), players)
	}
	if mv, _ := body["dressing_room_mood"].(float64); mv < 0 || mv > 100 {
		t.Fatalf("dressing_room_mood = %v, want [0,100]", body["dressing_room_mood"])
	}
	if ms, _ := body["manager_support"].(float64); ms < 0 || ms > 100 {
		t.Fatalf("manager_support = %v, want [0,100]", body["manager_support"])
	}

	// A foreign (AI) club is forbidden.
	resp = get(t, ts, client, fmt.Sprintf("/api/clubs/%s/dynamics", tw.AIOneClub), cookies)
	if code := resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("foreign dynamics = %d, want 403", code)
	}
}

// TestHTTPGetSquadDynamicsReflectsUnrest drives a sale through the faction
// service inside a transaction (the same hook acceptBid uses) and asserts the
// HTTP read model surfaces the en-masse delegation.
func TestHTTPGetSquadDynamicsReflectsUnrest(t *testing.T) {
	ts, pool := testHTTPServer(t)
	client := ts.Client()
	ctx := context.Background()
	const email = "dynamics-unrest@example.com"
	tw := transfertest.Provision(t, pool, "dynamics-unrest", email)
	cookies := loginManager(t, ts, pool, email)

	rows, err := pool.Query(ctx,
		`SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id`,
		tw.HumanClub)
	if err != nil {
		t.Fatalf("load players: %v", err)
	}
	var players []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		players = append(players, id)
	}
	rows.Close()
	if len(players) < 3 {
		t.Fatalf("club has %d players, want ≥ 3", len(players))
	}

	// Guarantee a connected dressing room.
	for i := 0; i < len(players); i++ {
		for j := i + 1; j < len(players); j++ {
			a, b := players[i].String(), players[j].String()
			if a > b {
				a, b = b, a
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO social.relationships
					(world_id, entity_a_id, entity_a_type, entity_b_id, entity_b_type,
					 relationship_type, strength, trust, sentiment, last_interaction_at)
				VALUES ($1, $2, 'player', $3, 'player', 'friendship', 60, 40, 60, now())
				ON CONFLICT (entity_a_id, entity_b_id, relationship_type) DO NOTHING`,
				tw.WorldID, a, b); err != nil {
				t.Fatalf("seed clique: %v", err)
			}
		}
	}

	svc := internalfaction.NewService(pool, nil)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := svc.OnPlayerSold(ctx, tx, tw.WorldID, 900, tw.HumanClub, players[0]); err != nil {
		tx.Rollback(ctx)
		t.Fatalf("on player sold: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	resp := get(t, ts, client, fmt.Sprintf("/api/clubs/%s/dynamics", tw.HumanClub), cookies)
	if code := resp.StatusCode; code != http.StatusOK {
		t.Fatalf("dynamics = %d, want 200", code)
	}
	body := decodeTransferBody(t, resp)
	unrest, ok := body["unrest"].(map[string]any)
	if !ok {
		t.Fatalf("unrest missing after sale: %v", body["unrest"])
	}
	if demand := stringField(t, unrest, "demand"); demand != internalfaction.DemandEnMasseTransferReqs {
		t.Fatalf("demand = %q, want %q", demand, internalfaction.DemandEnMasseTransferReqs)
	}
}
