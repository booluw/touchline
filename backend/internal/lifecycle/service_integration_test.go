//go:build integration

package lifecycle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/academy"
	internalcompetition "github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// seedLifecycleWorld returns a world with a country, a seeded 4-team league
// (clubs + drafted squads + a country pool), plus two synthetic 37-year-old
// veterans on the first club so the retirement pass has guaranteed material
// (age ≥ 37 force-retires).
func seedLifecycleWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, []uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	ctx := context.Background()
	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "lifecycle-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	compSvc := internalcompetition.NewService(pool, nil)
	country, err := compSvc.CreateCountry(ctx, w.ID, "eng", "England")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	if _, err := compSvc.CreateLeague(ctx, internalcompetition.LeagueParams{
		CountryID: country.ID, Name: "Premier", Tier: 1, TeamCount: 4,
	}); err != nil {
		t.Fatalf("create league: %v", err)
	}
	if _, err := compSvc.SeedWorld(ctx, w.ID); err != nil {
		t.Fatalf("seed world: %v", err)
	}

	var clubID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY id LIMIT 1`, w.ID).Scan(&clubID); err != nil {
		t.Fatalf("load club: %v", err)
	}
	veterans := insertVeterans(t, pool, w.ID, clubID)
	return pool, w.ID, country.ID, veterans, clubID
}

// insertVeterans creates two 37-year-old active players on the same club with
// full profile columns (attributes, hidden traits, personality) and active
// contracts, so retirement + contract termination has rows to act on.
func insertVeterans(t *testing.T, pool *pgxpool.Pool, worldID, clubID uuid.UUID) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	ids := make([]uuid.UUID, 0, 2)
	for i := 0; i < 2; i++ {
		var personID, playerID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO person.people (world_id, first_name, last_name, display_name, date_of_birth, nationality_code)
			VALUES ($1, $2, $3, $4, '1988-01-01', 'eng') RETURNING id`,
			worldID, fmt.Sprintf("Vet%d", i), "Surname", fmt.Sprintf("Vet%d", i)).Scan(&personID); err != nil {
			t.Fatalf("insert veteran person: %v", err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO player.players (world_id, person_id, club_id, primary_position, status, origin)
			VALUES ($1, $2, $3, 'CB', 'active', 'generated') RETURNING id`,
			worldID, personID, clubID).Scan(&playerID); err != nil {
			t.Fatalf("insert veteran player: %v", err)
		}
		for _, cat := range []string{"technical", "physical", "mental", "tactical", "positional"} {
			if _, err := pool.Exec(ctx, `
				INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
				VALUES ($1, $2, 'general', 60)`, playerID, cat); err != nil {
				t.Fatalf("insert veteran attrs: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.player_hidden_traits
				(player_id, potential, potential_ceiling_locked, consistency, injury_susceptibility,
				 adaptability, professionalism, ambition, loyalty, temperament, pressure_handling, learning_speed)
			VALUES ($1, 60, TRUE, 50, 30, 50, 50, 50, 50, 50, 50, 50)`, playerID); err != nil {
			t.Fatalf("insert veteran hidden traits: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.player_personality
				(player_id, professionalism, ambition, loyalty, ego, sociability, adaptability,
				 patience, leadership, emotional_volatility)
			VALUES ($1, 50, 50, 50, 50, 50, 50, 50, 50, 50)`, playerID); err != nil {
			t.Fatalf("insert veteran personality: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.contracts
				(player_id, club_id, weekly_wage, signing_bonus, start_date, end_date, status)
			VALUES ($1, $2, 15000, 0, '2024-01-01', '2030-01-01', 'active')`,
			playerID, clubID); err != nil {
			t.Fatalf("insert veteran contract: %v", err)
		}
		ids = append(ids, playerID)
	}
	return ids
}

func TestLifecycleOnSeasonCompleted(t *testing.T) {
	pool, worldID, countryID, veterans, _ := seedLifecycleWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil, academy.NewService(pool, nil))

	ref := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	res, err := svc.OnSeasonCompleted(ctx, worldID, &countryID, 1, ref)
	if err != nil {
		t.Fatalf("rollover 1: %v", err)
	}
	if res.PoolSize < 90 {
		t.Errorf("pool size after rollover = %d, want >=90 (replenish ran)", res.PoolSize)
	}

	// Both 37-year-olds must be retired (forced) with terminated contracts.
	retired := 0
	for _, pid := range veterans {
		var status string
		var club *uuid.UUID
		if err := pool.QueryRow(ctx,
			`SELECT status, club_id FROM player.players WHERE id = $1`, pid).Scan(&status, &club); err != nil {
			t.Fatalf("load veteran: %v", err)
		}
		if status == "retired" && club == nil {
			retired++
		}
		var contractStatus string
		if err := pool.QueryRow(ctx,
			`SELECT status FROM player.contracts WHERE player_id = $1 ORDER BY id LIMIT 1`, pid).
			Scan(&contractStatus); err != nil {
			t.Fatalf("load veteran contract: %v", err)
		}
		if contractStatus != "terminated" {
			t.Errorf("veteran %s contract status = %s, want terminated", pid, contractStatus)
		}
	}
	if retired != len(veterans) {
		t.Errorf("force-retired veterans = %d/%d", retired, len(veterans))
	}

	// Events emitted: WORLD_LIFECYCLE_SEASON_COMPLETED once + PLAYER_RETIRED.
	var lifecycleEvents, retiredEvents int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = $2`,
		worldID, EventWorldLifecycleCompleted).Scan(&lifecycleEvents); err != nil {
		t.Fatalf("count lifecycle events: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = $2`,
		worldID, EventPlayerRetired).Scan(&retiredEvents); err != nil {
		t.Fatalf("count retired events: %v", err)
	}
	if lifecycleEvents != 1 {
		t.Errorf("WORLD_LIFECYCLE events = %d, want 1", lifecycleEvents)
	}
	if retiredEvents < len(veterans) {
		t.Errorf("PLAYER_RETIRED events = %d, want >=%d", retiredEvents, len(veterans))
	}

	// Redelivery of the same season must be a no-op (idempotency guard).
	second, err := svc.OnSeasonCompleted(ctx, worldID, &countryID, 1, ref)
	if err != nil {
		t.Fatalf("redelivered rollover: %v", err)
	}
	if !second.AlreadyDone {
		t.Error("redelivered rollover must report AlreadyDone")
	}
	var after int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = $2`,
		worldID, EventWorldLifecycleCompleted).Scan(&after); err != nil {
		t.Fatalf("count lifecycle events again: %v", err)
	}
	if after != 1 {
		t.Errorf("lifecycle events after redelivery = %d, want still 1", after)
	}

	// A NEW season (+1 year) runs the hook again; the veterans are already
	// retired so no additional retirement, and the guard is fresh.
	ref2 := ref.AddDate(1, 0, 0)
	third, err := svc.OnSeasonCompleted(ctx, worldID, &countryID, 2, ref2)
	if err != nil {
		t.Fatalf("rollover 2: %v", err)
	}
	if third.AlreadyDone {
		t.Error("new season must not be reported as already done")
	}
}
