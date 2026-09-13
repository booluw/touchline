//go:build integration

package training

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/squad"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// feqEps compares with an explicit tolerance; used where the DB column width
// (numeric(5,4)) rounds the persisted value.
func feqEps(a, b, eps float64) bool { return math.Abs(a-b) < eps }

// trainingWorld builds an active world with one user-managed club (full squad,
// owning active manager) and returns the pool, world id, club id, manager id.
func trainingWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "training-world")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	worldID := w.ID

	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, worldID, "Harbour Training FC", "")
	if err != nil {
		t.Fatalf("bootstrap club: %v", err)
	}
	clubID := res.ClubID
	if _, err := internalworld.NewService(pool, nil).SetStatus(ctx, worldID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	var managerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`, clubID).
		Scan(&managerID); err != nil {
		t.Fatalf("load manager: %v", err)
	}
	return pool, worldID, clubID, managerID
}

type playerModel struct {
	pid   uuid.UUID
	age   int
	attrs map[string]int
}

func loadPlayerModel(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) map[uuid.UUID]*playerModel {
	t.Helper()
	ctx := context.Background()

	players := make(map[uuid.UUID]int)
	rows, err := pool.Query(ctx, `
		SELECT p.id, COALESCE(EXTRACT(YEAR FROM age(pe.date_of_birth)), 25)::int
		FROM player.players p JOIN person.people pe ON pe.id = p.person_id
		WHERE p.club_id = $1`, clubID)
	if err != nil {
		t.Fatalf("load players: %v", err)
	}
	for rows.Next() {
		var pid uuid.UUID
		var age int
		if err := rows.Scan(&pid, &age); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		players[pid] = age
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate players: %v", err)
	}

	attrs := make(map[uuid.UUID]map[string]int)
	rows, err = pool.Query(ctx, `
		SELECT a.player_id, a.attribute_key, a.value
		FROM player.player_attributes a JOIN player.players p ON p.id = a.player_id
		WHERE p.club_id = $1`, clubID)
	if err != nil {
		t.Fatalf("load attributes: %v", err)
	}
	for rows.Next() {
		var pid uuid.UUID
		var key string
		var val int
		if err := rows.Scan(&pid, &key, &val); err != nil {
			t.Fatalf("scan attribute: %v", err)
		}
		if attrs[pid] == nil {
			attrs[pid] = make(map[string]int)
		}
		attrs[pid][key] = val
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate attributes: %v", err)
	}

	out := make(map[uuid.UUID]*playerModel, len(players))
	for pid, age := range players {
		out[pid] = &playerModel{pid: pid, age: age, attrs: attrs[pid]}
	}
	return out
}

// rngFor replicates the service's documented deterministic rounding stream so
// tests can assert the exact persisted value for any (week, player, key).
func rngForTest(weekTick int64, pid uuid.UUID, key string) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte("training-apply:"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%s:%s", weekTick, pid.String(), key)))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

func setTick(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, tick int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE world.worlds SET current_tick = $1 WHERE id = $2`, tick, worldID); err != nil {
		t.Fatalf("set tick: %v", err)
	}
}

func TestSubmitPlanRoundTripsAndEvents(t *testing.T) {
	pool, worldID, clubID, managerID := trainingWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	setTick(t, pool, worldID, 42)
	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "attacking"); err != nil {
		t.Fatalf("submit plan: %v", err)
	}

	view, err := svc.GetPlan(ctx, clubID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if view.Archetype != "attacking" || view.EffectiveFromTick != 42 {
		t.Fatalf("view = %+v, want attacking from tick 42", view)
	}
	if view.LastAppliedWeek != nil {
		t.Fatalf("last_applied_week = %d, want nil after submit", *view.LastAppliedWeek)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'TRAINING_PLAN_SET'`, worldID).Scan(&count); err != nil {
		t.Fatalf("events: %v", err)
	}
	if count != 1 {
		t.Fatalf("TRAINING_PLAN_SET events = %d, want 1", count)
	}

	// Re-submitting resets the applied stamp and bumps the effective tick.
	setTick(t, pool, worldID, 43)
	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "physical"); err != nil {
		t.Fatalf("re-submit: %v", err)
	}
	view, err = svc.GetPlan(ctx, clubID)
	if err != nil {
		t.Fatalf("get plan 2: %v", err)
	}
	if view.Archetype != "physical" || view.EffectiveFromTick != 43 {
		t.Fatalf("re-submit view = %+v, want physical from tick 43", view)
	}

	// No plan yet set → zero value.
	other, err := svc.GetPlan(ctx, uuid.New())
	if err != nil {
		t.Fatalf("get missing plan: %v", err)
	}
	if other.Archetype != "" || other.ClubID != uuid.Nil {
		t.Fatalf("missing plan = %+v, want zero value", other)
	}
}

func TestSubmitPlanValidation(t *testing.T) {
	pool, _, clubID, managerID := trainingWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "yoga"); err != ErrInvalidArchetype {
		t.Fatalf("invalid archetype err = %v, want ErrInvalidArchetype", err)
	}
	if err := svc.SubmitPlan(ctx, Actor{ManagerID: uuid.New()}, clubID, "technical"); err != ErrNotOwned {
		t.Fatalf("ownership err = %v, want ErrNotOwned", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE world.worlds SET status = 'paused' WHERE id = (SELECT world_id FROM club.clubs WHERE id = $1)`, clubID); err != nil {
		t.Fatalf("pause world: %v", err)
	}
	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "technical"); err != ErrWorldNotActive {
		t.Fatalf("paused world err = %v, want ErrWorldNotActive", err)
	}
}

func TestApplyWeeklyMatchesDeterministicModel(t *testing.T) {
	pool, worldID, clubID, managerID := trainingWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	setTick(t, pool, worldID, 42)
	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "attacking"); err != nil {
		t.Fatalf("submit: %v", err)
	}

	before := loadPlayerModel(t, pool, clubID)

	n, err := svc.ApplyWeekly(ctx, worldID, 42)
	if err != nil {
		t.Fatalf("apply weekly: %v", err)
	}
	if n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}

	plan, _ := archetypeFor(ArchetypeAttacking)
	after := loadPlayerModel(t, pool, clubID)
	if len(after) != len(before) {
		t.Fatalf("player count changed: %d → %d", len(before), len(after))
	}
	for pid, m := range after {
		bm := before[pid]
		if bm == nil {
			t.Fatalf("player %s appeared during apply", pid)
		}
		// Every growth/decay key must equal the seeded binary-rounding stream;
		// untouched keys must be unchanged.
		for _, key := range distinctKeys(plan) {
			d := attrDelta(plan, bm.age, key)
			want := applyDelta(bm.attrs[key], d, rngForTest(42, pid, key))
			got := m.attrs[key]
			if got != want {
				t.Fatalf("player %s age %d key %s: value %d, want %d (delta %v)", pid, bm.age, key, got, want, d)
			}
		}
		// Never-written keys must still be absent.
		for key, val := range m.attrs {
			if val == 0 {
				t.Fatalf("player %s key %s persisted as 0", pid, key)
			}
		}
	}

	// Condition row for the first squad member: exact attacking deltas from
	// its lazy baseline, using its real injury susceptibility and position.
	var pid uuid.UUID
	for p := range before {
		pid = p
		break
	}
	var (
		position string
		injury   int
	)
	if err := pool.QueryRow(ctx, `
		SELECT pp.primary_position, COALESCE(h.injury_susceptibility, 50)
		FROM player.players pp LEFT JOIN player.player_hidden_traits h ON h.player_id = pp.id
		WHERE pp.id = $1`, pid).Scan(&position, &injury); err != nil {
		t.Fatalf("load player context: %v", err)
	}
	var c squad.PlayerCondition
	if err := pool.QueryRow(ctx, `
		SELECT fatigue, fitness, sharpness, injury_risk, tactical_familiarity
		FROM player.player_condition WHERE player_id = $1`, pid).Scan(
		&c.Fatigue, &c.Fitness, &c.Sharpness, &c.InjuryRisk, &c.TacticalFamiliarity); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	wantCondition := applyCondition(defaultCondition(pid, injury), plan, position)
	// player_condition columns are NUMERIC(5,4), so the DB rounds to 4dp.
	if !feqEps(c.Fatigue, wantCondition.Fatigue, 1e-4) || !feqEps(c.Fitness, wantCondition.Fitness, 1e-4) ||
		!feqEps(c.Sharpness, wantCondition.Sharpness, 1e-4) || !feqEps(c.InjuryRisk, wantCondition.InjuryRisk, 1e-4) ||
		!feqEps(c.TacticalFamiliarity, wantCondition.TacticalFamiliarity, 1e-4) {
		t.Fatalf("condition = %+v, want %+v", c, wantCondition)
	}

	// Stamp + weekly event bus row.
	var lastApplied *int64
	if err := pool.QueryRow(ctx,
		`SELECT last_applied_week FROM club.club_training_plans WHERE club_id = $1`, clubID).Scan(&lastApplied); err != nil {
		t.Fatalf("load stamp: %v", err)
	}
	if lastApplied == nil || *lastApplied != 42 {
		t.Fatalf("last_applied_week = %v, want 42", lastApplied)
	}
	var eventCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'TRAINING_WEEK' AND world_tick = 42`, worldID).Scan(&eventCount); err != nil {
		t.Fatalf("events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("TRAINING_WEEK events = %d, want 1", eventCount)
	}
}

func TestApplyWeeklyIdempotent(t *testing.T) {
	pool, worldID, clubID, managerID := trainingWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "technical"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if n, err := svc.ApplyWeekly(ctx, worldID, 50); err != nil || n != 1 {
		t.Fatalf("first apply = %d, err %v; want 1, nil", n, err)
	}
	// Redelivery of the same week applies nothing.
	if n, err := svc.ApplyWeekly(ctx, worldID, 50); err != nil || n != 0 {
		t.Fatalf("redelivery apply = %d, err %v; want 0, nil", n, err)
	}
	// A fresh week applies again.
	if n, err := svc.ApplyWeekly(ctx, worldID, 51); err != nil || n != 1 {
		t.Fatalf("next-week apply = %d, err %v; want 1, nil", n, err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = 'TRAINING_WEEK'`, worldID).Scan(&count); err != nil {
		t.Fatalf("events: %v", err)
	}
	if count != 2 {
		t.Fatalf("TRAINING_WEEK events = %d, want 2", count)
	}
}

func TestRecoveryDetrainsAfterThreeConsecutiveWeeks(t *testing.T) {
	pool, worldID, clubID, managerID := trainingWorld(t)
	svc := NewService(pool, nil)
	ctx := context.Background()

	// A controlled 25-year-old striker whose deterministic detrain stream
	// (+0.05/week at tick 63) guarantees the detrain branch executes. Using a
	// mid-20s age isolates the detrain path from veteran decay.
	controlled := uuid.MustParse("00000000-0000-0000-0000-0000000c0000")
	if _, err := pool.Exec(ctx, `
		INSERT INTO person.people (id, world_id, first_name, display_name, date_of_birth, nationality_code)
		VALUES ($1, $2, 'Controlled', 'Controlled', now() - interval '25 years', 'eng')`,
		controlled, worldID); err != nil {
		t.Fatalf("insert person: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.players (id, world_id, person_id, club_id, primary_position, squad_number)
		VALUES ($1, $2, $1, $3, 'ST', 99)`, controlled, worldID, clubID); err != nil {
		t.Fatalf("insert player: %v", err)
	}
	for key, val := range map[string]int{"pace": 60, "stamina": 60} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
			VALUES ($1, 'physical', $2, $3)`, controlled, key, val); err != nil {
			t.Fatalf("insert attr %s: %v", key, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_attributes (player_id, attribute_category, attribute_key, value)
		VALUES ($1, 'mental', 'decision_making', 50)`, controlled); err != nil {
		t.Fatalf("insert decision_making: %v", err)
	}

	if err := svc.SubmitPlan(ctx, Actor{ManagerID: managerID}, clubID, "recovery"); err != nil {
		t.Fatalf("submit recovery: %v", err)
	}
	// Seed the three immediately-preceding recovery weeks directly (the apply
	// path reads TRAINING_WEEK history to count consecutive recovery weeks).
	for _, wk := range []int64{60, 61, 62} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO world.events (world_id, world_tick, event_type, actor_type, payload)
			VALUES ($1, $2, 'TRAINING_WEEK', 'system', $3)`,
			worldID, wk, fmt.Sprintf(`{"club_id": %q, "archetype": "recovery"}`, clubID.String())); err != nil {
			t.Fatalf("seed recovery week %d: %v", wk, err)
		}
	}

	before := loadPlayerModel(t, pool, clubID)
	if n, err := svc.ApplyWeekly(ctx, worldID, 63); err != nil || n != 1 {
		t.Fatalf("apply week 63 = %d, err %v; want 1, nil", n, err)
	}
	after := loadPlayerModel(t, pool, clubID)

	plan, _ := archetypeFor(ArchetypeRecovery)
	for pid, m := range before {
		bm := m
		am := after[pid]
		if am == nil {
			t.Fatalf("player %s vanished during apply", pid)
		}
		// decision_making growth (seeded binary rounding at tick 63).
		want := applyDelta(bm.attrs["decision_making"],
			attrDelta(plan, bm.age, "decision_making"), rngForTest(63, pid, "decision_making"))
		if am.attrs["decision_making"] != want {
			t.Fatalf("decision_making = %d, want %d", am.attrs["decision_making"], want)
		}
		// pace: recovery never grows it; at ≥3 consecutive recovery weeks the
		// detrain term applies, and 30+ adds veteran decay — both deterministic.
		wantPace := bm.attrs["pace"]
		if rngForTest(63, pid, "pace-detrain").Float64() < 0.05 {
			wantPace--
		}
		if veteranDecay(bm.age, ArchetypeRecovery) != nil {
			if rngForTest(63, pid, "pace-age").Float64() < 0.05 {
				wantPace--
			}
		}
		if wantPace < 1 {
			wantPace = 1
		}
		if am.attrs["pace"] != wantPace {
			t.Fatalf("pace = %d, want %d", am.attrs["pace"], wantPace)
		}
	}

	// The controlled player must hit the detrain branch (guaranteed by choice
	// of id): pace 60 → 59; its stamina stream lands above 0.05 so stamina
	// stays 60.
	if got := after[controlled].attrs["pace"]; got != 59 {
		t.Fatalf("controlled pace = %d, want 59", got)
	}
	if got := after[controlled].attrs["stamina"]; got != 60 {
		t.Fatalf("controlled stamina = %d, want 60", got)
	}

	// Controlled player decision_making: growth stream is 0.955 so the +0.1
	// delta does not round up; value must stay 50.
	if got := after[controlled].attrs["decision_making"]; got != 50 {
		t.Fatalf("controlled decision_making = %d, want 50", got)
	}
	if got := before[controlled].attrs["stamina"]; got != 60 {
		t.Fatalf("controlled pre-apply stamina = %d, want 60", got)
	}

	// Controlled player condition, from the default baseline in one step:
	// fatigue 0, fitness 1, sharpness 0.5, familiarity 0.55.
	var c squad.PlayerCondition
	if err := pool.QueryRow(ctx, `
		SELECT fatigue, fitness, sharpness, tactical_familiarity
		FROM player.player_condition WHERE player_id = $1`, controlled).Scan(
		&c.Fatigue, &c.Fitness, &c.Sharpness, &c.TacticalFamiliarity); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	if c.Fatigue != 0 || c.Fitness != 1 || c.Sharpness != 0.5 || c.TacticalFamiliarity != 0.55 {
		t.Fatalf("recovery condition = fatigue %v fitness %v sharpness %v fam %v; want 0/1/0.5/0.55",
			c.Fatigue, c.Fitness, c.Sharpness, c.TacticalFamiliarity)
	}
}
