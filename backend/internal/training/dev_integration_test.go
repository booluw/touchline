//go:build integration

package training

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

func devWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "dev-world")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Dev Club", "")
	if err != nil {
		t.Fatalf("bootstrap club: %v", err)
	}
	clubID := res.ClubID
	if _, err := internalworld.NewService(pool, nil).SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	var managerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`, clubID).
		Scan(&managerID); err != nil {
		t.Fatalf("load manager: %v", err)
	}
	return pool, w.ID, clubID, managerID
}

// insertControlledPlayer creates a person/player (with skills and hidden
// traits) belonging to clubID, and a completed match recording one rated
// appearance for it.
func insertControlledPlayer(t *testing.T, pool *pgxpool.Pool, worldID, clubID, pid uuid.UUID,
	age int, position string, pro, potential, rating int, minPct float64, skills map[string]int) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO person.people (id, world_id, first_name, display_name, date_of_birth, nationality_code)
		VALUES ($1, $2, 'Dev', 'Player', now() - make_interval(years => $3), 'eng')`,
		pid, worldID, age); err != nil {
		t.Fatalf("insert person: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.players (id, world_id, person_id, club_id, primary_position, squad_number)
		VALUES ($1, $2, $1, $3, $4, 99)`, pid, worldID, clubID, position); err != nil {
		t.Fatalf("insert player: %v", err)
	}
	for key, val := range skills {
		cat := categoryForTestKey(key)
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
			VALUES ($1, $2, $3, $4)`, pid, cat, key, val); err != nil {
			t.Fatalf("insert attr %s: %v", key, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_hidden_traits
			(player_id, potential, consistency, injury_susceptibility, adaptability,
			 professionalism, ambition, loyalty, temperament, pressure_handling, learning_speed)
		VALUES ($1, $2, 50, 50, 50, $3, 60, 60, 60, 50, 60)`,
		pid, potential, pro, ); err != nil {
		t.Fatalf("insert hidden traits: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, morale, playing_time_pct, updated_at)
		VALUES ($1, 0.7, $2, now())`, pid, minPct); err != nil {
		t.Fatalf("insert condition: %v", err)
	}

	if rating > 0 {
		var away uuid.UUID
		if err := pool.QueryRow(ctx,
			`SELECT id FROM club.clubs WHERE id <> $1 LIMIT 1`, clubID).Scan(&away); err != nil {
			t.Fatalf("load away club: %v", err)
		}
		compID := uuid.MustParse("00000000-0000-0000-0000-00000000a001")
		fixtureID := uuid.MustParse("00000000-0000-0000-0000-000000000f01")
		matchID := uuid.MustParse("00000000-0000-0000-0000-0000000f0001")
		if _, err := pool.Exec(ctx, `
			INSERT INTO competition.competitions (id, world_id, name, competition_type)
			VALUES ($1, $2, 'Dev Cup', 'league')`, compID, worldID); err != nil {
			t.Fatalf("insert competition: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO match.fixtures (id, world_id, competition_id, home_club_id, away_club_id, scheduled_at, status)
			VALUES ($1, $2, $3, $4, $5, now(), 'completed')`,
			fixtureID, worldID, compID, clubID, away); err != nil {
			t.Fatalf("insert fixture: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO match.matches (id, fixture_id, world_id, seed, engine_version, home_score, away_score, status, ended_at)
			VALUES ($1, $2, $3, 0, '1.6', 1, 0, 'completed', now())`,
			matchID, fixtureID, worldID); err != nil {
			t.Fatalf("insert match: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.player_appearances (player_id, match_id, started, minutes, rating, goals, assists)
			VALUES ($1, $2, TRUE, 90, $3, 1, 0)`, pid, matchID, rating); err != nil {
			t.Fatalf("insert appearance: %v", err)
		}
	}
	// Neutral academy absent → facility level 5.
}

func categoryForTestKey(key string) string {
	switch key {
	case "finishing", "passing", "dribbling":
		return "technical"
	case "pace", "stamina", "strength":
		return "physical"
	default:
		return "mental"
	}
}

// TestDevelopmentWeeklyFlexExpansion is the headline S08-02 behaviour: a
// 19-year-old elite star who has reached their hidden ceiling flexes it.
func TestDevelopmentWeeklyFlexExpansion(t *testing.T) {
	pool, worldID, clubID, managerID := devWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	wonderkid := uuid.MustParse("00000000-0000-0000-0000-000000000d01")
	insertControlledPlayer(t, pool, worldID, clubID, wonderkid, 19, "ST", 85, 85, 8, 0.8,
		map[string]int{"finishing": 84, "off_the_ball": 80, "pace": 75, "composure": 82})

	setTick(t, pool, worldID, 100)
	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "attacking"); err != nil {
		t.Fatalf("submit plan: %v", err)
	}
	if n, err := svc.ApplyWeekly(ctx, worldID, 100); err != nil || n != 1 {
		t.Fatalf("apply weekly = %d, err %v; want 1, nil", n, err)
	}

	// Potential 85 → 87 (+1 base, +1 youth bonus); budget 3 → 2; not locked.
	var potential int
	var locked bool
	if err := pool.QueryRow(ctx, `
		SELECT potential, potential_ceiling_locked FROM player.player_hidden_traits WHERE player_id = $1`,
		wonderkid).Scan(&potential, &locked); err != nil {
		t.Fatalf("load hidden traits: %v", err)
	}
	if potential != 87 {
		t.Fatalf("potential = %d, want 87", potential)
	}
	if locked {
		t.Fatal("ceiling must not lock after a single expansion")
	}
	var expansions int
	if err := pool.QueryRow(ctx, `
		SELECT potential_expansions_remaining FROM player.player_development WHERE player_id = $1`,
		wonderkid).Scan(&expansions); err != nil {
		t.Fatalf("load dev state: %v", err)
	}
	if expansions != 2 {
		t.Fatalf("expansions left = %d, want 2", expansions)
	}

	// The DEVELOPMENT_WEEK event carries the auditable explanation.
	var payload []byte
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM world.events
		WHERE world_id = $1 AND event_type = 'DEVELOPMENT_WEEK' AND world_tick = 100`, worldID).Scan(&payload); err != nil {
		t.Fatalf("load dev event: %v", err)
	}
	var evt devWeekPayload
	if err := json.Unmarshal(payload, &evt); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	foundWonderkid := false
	for _, p := range evt.Players {
		if p.PlayerID == wonderkid.String() && p.Explanation != nil {
			foundWonderkid = true
			if p.Explanation.Subject != "player_development" || len(p.Explanation.Factors) == 0 {
				t.Fatalf("explanation = %+v", p.Explanation)
			}
		}
	}
	if !foundWonderkid {
		t.Fatal("DEV event must carry the wonderkid's explanation")
	}

	// A youth starter with minutes must strictly grow (multiplier > 1).
	after := loadPlayerModel(t, pool, clubID)
	if after[wonderkid].attrs["finishing"] <= 84 {
		t.Fatalf("finishing must grow for a wonderkid starter: got %d", after[wonderkid].attrs["finishing"])
	}
}

// TestDevelopmentWeeklyStagnationWritesState tracks a low-usage player through
// three weeks: the consecutive-stagnant counter climbs and player_development
// is written deterministically with the counter.
func TestDevelopmentWeeklyStagnationWritesState(t *testing.T) {
	pool, worldID, clubID, managerID := devWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	bench := uuid.MustParse("00000000-0000-0000-0000-000000000d02")
	insertControlledPlayer(t, pool, worldID, clubID, bench, 25, "CM", 40, 80, 0, 0.10,
		map[string]int{"passing": 70, "work_rate": 65, "pace": 60})

	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "technical"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	for i, tick := range []int64{200, 201, 202} {
		setTick(t, pool, worldID, tick)
		if n, err := svc.ApplyWeekly(ctx, worldID, tick); err != nil || n != 1 {
			t.Fatalf("apply %d = %d, err %v; want 1, nil", tick, n, err)
		}
		var consec int
		if err := pool.QueryRow(ctx, `
			SELECT consecutive_stagnant_weeks FROM player.player_development WHERE player_id = $1`,
			bench).Scan(&consec); err != nil {
			t.Fatalf("load dev state: %v", err)
		}
		if want := i + 1; consec != want {
			t.Fatalf("week %d: stagnant count = %d, want %d", tick, consec, want)
		}
	}
}