//go:build integration

package player_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/player"
	"github.com/touchline/backend/internal/squad"
)

// rosterCategoryMeans reads one player's six category means straight from the
// player_attributes EAV so the roster roll-up can be checked against the
// source of truth instead of against itself. A category the player carries no
// keys for (goalkeeping for an outfielder) stays 0, exactly as the roll-up
// reports it.
func rosterCategoryMeans(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID) player.PlayerAttributes {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT attribute_category, ROUND(AVG(value))::int
		FROM player.player_attributes
		WHERE player_id = $1
		GROUP BY attribute_category`, playerID)
	if err != nil {
		t.Fatalf("read category means: %v", err)
	}
	defer rows.Close()

	var out player.PlayerAttributes
	for rows.Next() {
		var cat string
		var mean int
		if err := rows.Scan(&cat, &mean); err != nil {
			t.Fatalf("scan category mean: %v", err)
		}
		switch cat {
		case "technical":
			out.Technical = mean
		case "physical":
			out.Physical = mean
		case "mental":
			out.Mental = mean
		case "tactical":
			out.Tactical = mean
		case "goalkeeping":
			out.Goalkeeping = mean
		case "positional":
			out.Positional = mean
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate category means: %v", err)
	}
	return out
}

// TestRosterCarriesAttributesAndOverall covers the IM17 roster read: every
// active player exposes the six category means plus the position-weighted
// overall, both derived from the EAV at read time.
func TestRosterCarriesAttributesAndOverall(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "roster-attributes")
	ctx := context.Background()
	pid := pickPlayer(t, pool, tw.HumanClub)

	// Pin every attribute row the player carries to a known value so the
	// expected category means are exact.
	if _, err := pool.Exec(ctx, `
		UPDATE player.player_attributes
		SET value = CASE attribute_category
			WHEN 'technical' THEN 60
			WHEN 'physical' THEN 61
			WHEN 'mental' THEN 62
			WHEN 'tactical' THEN 63
			WHEN 'goalkeeping' THEN 64
			ELSE 65
		END
		WHERE player_id = $1`, pid); err != nil {
		t.Fatalf("pin attributes: %v", err)
	}

	var position string
	if err := pool.QueryRow(ctx,
		`SELECT primary_position FROM player.players WHERE id = $1`, pid).Scan(&position); err != nil {
		t.Fatalf("read position: %v", err)
	}
	want := rosterCategoryMeans(t, pool, pid)
	if want.Technical == 0 || want.Physical == 0 {
		t.Fatalf("fixture player has no outfield attributes: %+v", want)
	}

	rows, err := svc.ListSquadMorale(ctx, tw.WorldID, tw.HumanMgr)
	if err != nil {
		t.Fatalf("list squad: %v", err)
	}
	var got *player.PlayerMoraleRow
	for i := range rows {
		if rows[i].Player != nil && rows[i].Player.ID == pid {
			got = &rows[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("player %s not in roster read", pid)
	}
	if got.Attributes != want {
		t.Errorf("attributes = %+v, want %+v", got.Attributes, want)
	}
	wantOverall := squad.PositionalOverall(position, squad.AttributeSnapshot{
		Technical:   want.Technical,
		Physical:    want.Physical,
		Mental:      want.Mental,
		Tactical:    want.Tactical,
		Goalkeeping: want.Goalkeeping,
		Positional:  want.Positional,
	})
	if got.Overall != wantOverall {
		t.Errorf("overall = %d, want %d", got.Overall, wantOverall)
	}
	if got.Overall < 1 || got.Overall > squad.OverallCap {
		t.Errorf("overall = %d, want 1..%d", got.Overall, squad.OverallCap)
	}
}

// TestRosterOverallCapsAt99 proves the roster honours the 99 display ceiling
// even when every attribute is at the 100 band maximum.
func TestRosterOverallCapsAt99(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "roster-cap")
	ctx := context.Background()
	pid := pickPlayer(t, pool, tw.HumanClub)

	if _, err := pool.Exec(ctx,
		`UPDATE player.player_attributes SET value = 100 WHERE player_id = $1`, pid); err != nil {
		t.Fatalf("max attributes: %v", err)
	}

	rows, err := svc.ListSquadMorale(ctx, tw.WorldID, tw.HumanMgr)
	if err != nil {
		t.Fatalf("list squad: %v", err)
	}
	for _, r := range rows {
		if r.Player == nil || r.Player.ID != pid {
			continue
		}
		if r.Overall != squad.OverallCap {
			t.Errorf("overall = %d, want capped at %d", r.Overall, squad.OverallCap)
		}
		return
	}
	t.Fatalf("player %s not in roster read", pid)
}

// IM62: the roster carries the lineup gate (injured players unavailable, with
// the reason) and condition fitness.
func TestRosterAvailabilityAndFitness(t *testing.T) {
	svc, _, pool, tw := newPlayerFixture(t, "roster-avail")
	ctx := context.Background()
	pid := pickPlayer(t, pool, tw.HumanClub)

	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, 'muscle', 3, now() + interval '30 days', 0.3, now())`, pid); err != nil {
		t.Fatalf("insert open injury: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, fitness) VALUES ($1, 0.8)
		ON CONFLICT (player_id) DO UPDATE SET fitness = 0.8`, pid); err != nil {
		t.Fatalf("set fitness: %v", err)
	}

	rows, err := svc.ListSquadMorale(ctx, tw.WorldID, tw.HumanMgr)
	if err != nil {
		t.Fatalf("list squad: %v", err)
	}
	available := 0
	for _, r := range rows {
		if r.Player.ID == pid {
			if r.Available || r.UnavailableReason != "injured" || r.Fitness != 0.8 {
				t.Fatalf("injured row = available %v reason %q fitness %v", r.Available, r.UnavailableReason, r.Fitness)
			}
			continue
		}
		if r.Available {
			available++
		}
	}
	if available == 0 {
		t.Fatal("every other contracted player should be available")
	}
}
