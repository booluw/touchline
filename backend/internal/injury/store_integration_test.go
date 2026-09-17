//go:build integration

package injury

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// injuryWorld provisions an active world with one bootstrapped club and returns
// the world id, the club id and its active players.
func injuryWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, []uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "injury-world")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := bootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Harbour Injury FC", "")
	if err != nil {
		t.Fatalf("bootstrap club: %v", err)
	}
	if _, err := internalworld.NewService(pool, nil).SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	rows, err := pool.Query(ctx, `SELECT id FROM player.players WHERE club_id = $1 AND status = 'active' ORDER BY id`, res.ClubID)
	if err != nil {
		t.Fatalf("load players: %v", err)
	}
	defer rows.Close()
	var players []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		players = append(players, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate players: %v", err)
	}
	if len(players) < 3 {
		t.Fatalf("club has %d players, want ≥ 3", len(players))
	}
	return pool, w.ID, res.ClubID, players
}

// TestPersistMatchRoundTrip drives PersistMatch through the DB and re-derives
// the expected outcome from the same context Evaluate consumed, so the stored
// row, the injury-risk bump and the PLAYER_INJURED event all match the engine.
func TestPersistMatchRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, players := injuryWorld(t)

	pid := players[0]
	const (
		matchSeed = int64(4242)
		minutes   = 90
		tick      = int64(1201)
	)
	now := time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC)
	matchID := uuid.New()

	// Expected outcome: evaluate with the exact context the store loads.
	var out Outcome
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	pctx, err := loadPlayerContexts(ctx, tx, []uuid.UUID{pid})
	if err != nil {
		t.Fatalf("load contexts: %v", err)
	}
	c := pctx[pid]
	med, err := MedicalLevel(ctx, tx, clubID)
	if err != nil {
		t.Fatalf("medical level: %v", err)
	}
	out = Evaluate(Input{
		Seed:           int64(MatchStream(matchSeed, pid)),
		Fatigue:        c.Fatigue,
		Susceptibility: c.Susceptibility,
		MedicalLevel:   med,
		Minutes:        minutes,
		FoulContext:    true,
		RecurrenceLoad: c.RecurrenceLoad,
	})
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback plan: %v", err)
	}

	stored, err := persistInTx(t, pool, ctx, worldID, tick, matchID, matchSeed, now, []Candidate{{PlayerID: pid, Minutes: minutes}})
	if err != nil {
		t.Fatalf("persist match injury: %v", err)
	}
	if stored != 1 {
		t.Fatalf("persist match stored = %d, want 1", stored)
	}

	var got struct {
		Type       string
		Severity   int
		Expected   time.Time
		Recurrence float64
		OccurredAt time.Time
	}
	if err := pool.QueryRow(ctx, `
		SELECT injury_type, severity, expected_recovery_date::date, recurrence_risk::float8, occurred_at
		FROM player.injuries WHERE player_id = $1 AND actual_recovery_date IS NULL`, pid).
		Scan(&got.Type, &got.Severity, &got.Expected, &got.Recurrence, &got.OccurredAt); err != nil {
		t.Fatalf("load stored injury: %v", err)
	}
	if got.Type != string(out.Type) {
		t.Errorf("stored type = %s, want %s", got.Type, out.Type)
	}
	if got.Severity != out.Severity {
		t.Errorf("stored severity = %d, want %d", got.Severity, out.Severity)
	}
	if want := truncd(now.AddDate(0, 0, out.DaysOut)); !got.Expected.Equal(want) {
		t.Errorf("stored expected recovery = %s, want %s (%d days)", got.Expected, want, out.DaysOut)
	}
	if math.Abs(got.Recurrence-out.RecurrenceRisk) > 1e-3 {
		t.Errorf("stored recurrence = %.4f, want %.4f", got.Recurrence, out.RecurrenceRisk)
	}
	if !truncd(got.OccurredAt).Equal(truncd(now)) {
		t.Errorf("stored occurred_at = %s, want %s", got.OccurredAt, now)
	}

	var risk float64
	if err := pool.QueryRow(ctx,
		`SELECT injury_risk::float8 FROM player.player_condition WHERE player_id = $1`, pid).Scan(&risk); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	if math.Abs(risk-out.RecurrenceRisk) > 1e-3 {
		t.Errorf("condition injury_risk = %.4f, want %.4f", risk, out.RecurrenceRisk)
	}

	var ev struct {
		Seed    *int64
		Payload string
	}
	if err := pool.QueryRow(ctx, `
		SELECT random_seed, payload FROM world.events
		WHERE world_id = $1 AND event_type = $2`, worldID, EventPlayerInjured).Scan(&ev.Seed, &ev.Payload); err != nil {
		t.Fatalf("load event: %v", err)
	}
	if ev.Seed == nil || *ev.Seed != matchSeed {
		t.Errorf("PLAYER_INJURED random_seed = %v, want %d", ev.Seed, matchSeed)
	}
	var pl struct {
		PlayerID string `json:"player_id"`
		Type     string `json:"injury_type"`
	}
	if err := json.Unmarshal([]byte(ev.Payload), &pl); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	if pl.PlayerID != pid.String() || pl.Type != string(out.Type) {
		t.Errorf("event payload = %+v, want player %s / type %s", pl, pid, out.Type)
	}

	// Redelivery is a no-op: the player already has an open injury.
	re, err := persistInTx(t, pool, ctx, worldID, tick, matchID, matchSeed, now, []Candidate{{PlayerID: pid, Minutes: minutes}})
	if err != nil {
		t.Fatalf("re-persist: %v", err)
	}
	if re != 0 {
		t.Errorf("re-persist stored = %d, want 0 (open injury guard)", re)
	}
}

func persistInTx(t *testing.T, pool *pgxpool.Pool, ctx context.Context, worldID uuid.UUID, tick int64,
	matchID uuid.UUID, matchSeed int64, now time.Time, cands []Candidate,
) (int, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	n, err := PersistMatch(ctx, tx, nil, worldID, tick, matchID, matchSeed, now, cands)
	if err != nil {
		_ = tx.Rollback(ctx)
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return n, nil
}

// TestPersistTrainingRoundTrip drives the weekly training-injury writer: the
// stored row matches Evaluate for the same seed + context, the condition risk
// bumps to the outcome and PLAYER_INJURED is recorded.
func TestPersistTrainingRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, players := injuryWorld(t)
	pid := players[0]

	if _, err := pool.Exec(ctx, `
		INSERT INTO player.player_condition (player_id, fatigue, injury_risk, updated_at)
		VALUES ($1, 0.6, 0.8, now())
		ON CONFLICT (player_id) DO UPDATE SET fatigue = 0.6, injury_risk = 0.8, updated_at = now()`,
		pid); err != nil {
		t.Fatalf("set condition: %v", err)
	}

	const tick = int64(1501)
	seed := WeekStream(tick, pid, "training")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	pctx, err := loadPlayerContexts(ctx, tx, []uuid.UUID{pid})
	if err != nil {
		t.Fatalf("load contexts: %v", err)
	}
	c := pctx[pid]
	med, err := MedicalLevel(ctx, tx, clubID)
	if err != nil {
		t.Fatalf("medical level: %v", err)
	}
	out := Evaluate(Input{
		Seed:           int64(seed),
		Fatigue:        c.Fatigue,
		Susceptibility: c.Susceptibility,
		MedicalLevel:   med,
		RecurrenceLoad: c.RecurrenceLoad,
	})
	wrote, err := PersistTraining(ctx, tx, nil, worldID, tick, clubID, pid,
		c.Fatigue, c.Susceptibility, med, c.RecurrenceLoad, seed)
	if err != nil {
		t.Fatalf("persist training injury: %v", err)
	}
	if !wrote {
		t.Fatal("persist training wrote = false, want true")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var got struct {
		Type       string
		Severity   int
		Expected   time.Time
		Recurrence float64
	}
	if err := pool.QueryRow(ctx, `
		SELECT injury_type, severity, expected_recovery_date::date, recurrence_risk::float8
		FROM player.injuries WHERE player_id = $1 AND actual_recovery_date IS NULL`, pid).
		Scan(&got.Type, &got.Severity, &got.Expected, &got.Recurrence); err != nil {
		t.Fatalf("load stored injury: %v", err)
	}
	if got.Type != string(out.Type) || got.Severity != out.Severity {
		t.Errorf("stored type/severity = %s/%d, want %s/%d", got.Type, got.Severity, out.Type, out.Severity)
	}
	if math.Abs(got.Recurrence-out.RecurrenceRisk) > 1e-3 {
		t.Errorf("stored recurrence = %.4f, want %.4f", got.Recurrence, out.RecurrenceRisk)
	}

	var risk float64
	if err := pool.QueryRow(ctx, `
		SELECT injury_risk::float8 FROM player.player_condition WHERE player_id = $1`, pid).Scan(&risk); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	if math.Abs(risk-out.RecurrenceRisk) > 1e-3 {
		t.Errorf("condition injury_risk = %.4f, want %.4f", risk, out.RecurrenceRisk)
	}

	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = $2`, worldID, EventPlayerInjured).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 1 {
		t.Errorf("PLAYER_INJURED events = %d, want 1", n)
	}
}

// TestRecoverDueClosesDueInjury verifies the recovery stage: a due injury
// closes, PLAYER_RECOVERED is emitted, the injury_return history row lands and
// the training-side injury risk resets to susceptibility/200.
func TestRecoverDueClosesDueInjury(t *testing.T) {
	ctx := context.Background()
	pool, worldID, _, players := injuryWorld(t)
	pid := players[0]

	now := time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC)
	occurred := now.AddDate(0, 0, -10)
	expected := now.AddDate(0, 0, -1)
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, 'muscle', 3, $2, 0.3, $3)`, pid, expected, occurred); err != nil {
		t.Fatalf("insert open injury: %v", err)
	}

	sum, err := RecoverDue(ctx, pool, nil, worldID, 1301, now)
	if err != nil {
		t.Fatalf("recover due: %v", err)
	}
	if sum.Recovered != 1 || sum.Setbacks != 0 {
		t.Errorf("recover sum = %+v, want recovered 1 / setbacks 0", sum)
	}

	var actual time.Time
	if err := pool.QueryRow(ctx, `
		SELECT actual_recovery_date::date FROM player.injuries
		WHERE player_id = $1 AND injury_type = 'muscle'`, pid).Scan(&actual); err != nil {
		t.Fatalf("load recovery date: %v", err)
	}
	if !actual.Equal(truncd(now)) {
		t.Errorf("actual_recovery_date = %s, want %s", actual, truncd(now))
	}

	var ev struct{ Payload string }
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM world.events
		WHERE world_id = $1 AND event_type = $2`, worldID, EventPlayerRecovered).Scan(&ev.Payload); err != nil {
		t.Fatalf("load recovery event: %v", err)
	}
	var pl struct {
		PlayerID string `json:"player_id"`
		Type     string `json:"injury_type"`
	}
	if err := json.Unmarshal([]byte(ev.Payload), &pl); err != nil {
		t.Fatalf("decode recovery event: %v", err)
	}
	if pl.PlayerID != pid.String() || pl.Type != "muscle" {
		t.Errorf("recovery event payload = %+v", pl)
	}

	var history string
	if err := pool.QueryRow(ctx, `
		SELECT event_type FROM player.player_history
		WHERE player_id = $1 AND event_type = 'injury_return'`, pid).Scan(&history); err != nil {
		t.Fatalf("load history: %v", err)
	}

	if history != "injury_return" {
		t.Errorf("history event_type = %q, want injury_return", history)
	}

	var baseline int
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(injury_susceptibility, 50) FROM player.player_hidden_traits WHERE player_id = $1`, pid).
		Scan(&baseline); err != nil {
		t.Fatalf("load susceptibility: %v", err)
	}
	var risk float64
	if err := pool.QueryRow(ctx, `
		SELECT injury_risk::float8 FROM player.player_condition WHERE player_id = $1`, pid).Scan(&risk); err != nil {
		t.Fatalf("load condition: %v", err)
	}
	if want := float64(baseline) / 200; math.Abs(risk-want) > 1e-3 {
		t.Errorf("condition injury_risk after recovery = %.4f, want %.4f", risk, want)
	}
}

// TestRecoverDueSetbackIsIdempotent rolls one setback (found deterministically)
// and verifies a redelivered weekly tick never double-applies it.
func TestRecoverDueSetbackIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool, worldID, _, players := injuryWorld(t)
	pid := players[0]

	today := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
	occurred := today.AddDate(0, 0, -60)
	expected := today.AddDate(0, 0, 30) // total plan 90d, elapsed 60 ≥ 55% — and still ≥ 55% after any setback
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, 'ligament', 4, $2, 0.25, $3)`, pid, expected, occurred); err != nil {
		t.Fatalf("insert open injury: %v", err)
	}
	var injuryID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM player.injuries WHERE player_id = $1`, pid).Scan(&injuryID); err != nil {
		t.Fatalf("load injury id: %v", err)
	}

	var tick int64
	for i := int64(1); i <= 200; i++ {
		if Setback(SetbackStream(injuryID, i)) > 0 {
			tick = i
			break
		}
	}
	if tick == 0 {
		t.Fatal("no setback-firing tick found in 0..200 for this injury id")
	}

	sum1, err := RecoverDue(ctx, pool, nil, worldID, tick, today)
	if err != nil {
		t.Fatalf("recover due (1st): %v", err)
	}
	if sum1.Setbacks != 1 || sum1.Recovered != 0 {
		t.Fatalf("recover sum (1st) = %+v, want setback 1 / recovered 0", sum1)
	}
	var days int
	if err := pool.QueryRow(ctx, `SELECT days_added FROM player.injury_setbacks WHERE injury_id = $1 AND week = $2`,
		injuryID, tick).Scan(&days); err != nil {
		t.Fatalf("load setback: %v", err)
	}
	var extended time.Time
	if err := pool.QueryRow(ctx, `SELECT expected_recovery_date::date FROM player.injuries WHERE id = $1`,
		injuryID).Scan(&extended); err != nil {
		t.Fatalf("load extended date: %v", err)
	}
	if want := expected.AddDate(0, 0, days); !extended.Equal(want) {
		t.Errorf("expected_recovery_date after setback = %s, want %s", extended, want)
	}

	// A redelivered tick with the same week is a no-op (PK (injury_id, week)).
	sum2, err := RecoverDue(ctx, pool, nil, worldID, tick, today)
	if err != nil {
		t.Fatalf("recover due (2nd): %v", err)
	}
	if sum2.Setbacks != 0 {
		t.Errorf("recover sum (2nd) setbacks = %d, want 0 (idempotent)", sum2.Setbacks)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM player.injury_setbacks WHERE injury_id = $1`, injuryID).
		Scan(&rows); err != nil {
		t.Fatalf("count setbacks: %v", err)
	}
	if rows != 1 {
		t.Errorf("setback rows = %d, want 1", rows)
	}
}

// TestRushCloseStampsRecurrenceFloor verifies the manager's early-return closes
// the injury, floors recurrence at 0.65, emits PLAYER_RUSHED_RETURN and refuses
// a second close.
func TestRushCloseStampsRecurrenceFloor(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, players := injuryWorld(t)
	pid := players[0]

	now := time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, occurred_at)
		VALUES ($1, 'bone', 6, $2, 0.2, $3)`, pid, now.AddDate(0, 0, 30), now.AddDate(0, 0, -2)); err != nil {
		t.Fatalf("insert open injury: %v", err)
	}
	var injuryID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM player.injuries WHERE player_id = $1`, pid).Scan(&injuryID); err != nil {
		t.Fatalf("load injury id: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id2, typ, _, ok, err := OpenForPlayer(ctx, tx, pid)
	if err != nil || !ok {
		t.Fatalf("open for player = %v, %v", ok, err)
	}
	if id2 != injuryID || typ != TypeBone {
		t.Errorf("open injury = %s/%s, want %s/bone", id2, typ, injuryID)
	}
	if err := RushClose(ctx, tx, nil, worldID, 1401, clubID, pid, injuryID, now); err != nil {
		t.Fatalf("rush close: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var risk float64
	var actual time.Time
	if err := pool.QueryRow(ctx, `
		SELECT recurrence_risk::float8, actual_recovery_date::date FROM player.injuries WHERE id = $1`,
		injuryID).Scan(&risk, &actual); err != nil {
		t.Fatalf("load closed injury: %v", err)
	}
	if risk < RushRecurrenceFloor {
		t.Errorf("recurrence after rush = %.4f, want ≥ %.2f", risk, RushRecurrenceFloor)
	}
	if !actual.Equal(truncd(now)) {
		t.Errorf("actual_recovery_date = %s, want %s", actual, truncd(now))
	}

	var n int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = $2`,
		worldID, EventPlayerRushedReturn).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 1 {
		t.Errorf("PLAYER_RUSHED_RETURN events = %d, want 1", n)
	}

	// The injury is closed: opening / rushing again is refused.
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin2: %v", err)
	}
	if _, _, _, ok, err := OpenForPlayer(ctx, tx2, pid); err != nil || ok {
		t.Fatalf("open after close = %v (want false), %v", ok, err)
	}
	if err := RushClose(ctx, tx2, nil, worldID, 1401, clubID, pid, injuryID, now); err == nil {
		t.Fatal("rush close on a closed injury = nil, want ErrNoOpenInjury")
	}
	_ = tx2.Rollback(ctx)
}

// TestMedicalLevelNeutralDefaultAndMaterialised verifies the neutral default 5
// for a club without a medical row and the materialised row after an upgrade.
func TestMedicalLevelNeutralDefaultAndMaterialised(t *testing.T) {
	ctx := context.Background()
	pool, _, clubID, _ := injuryWorld(t)
	if lvl, err := medicalLevelFor(t, pool, ctx, clubID); err != nil {
		t.Fatalf("get medical level: %v", err)
	} else if lvl != 5 {
		t.Errorf("medical level (no row) = %d, want 5", lvl)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO club.facilities (club_id, facility_type, level)
		VALUES ($1, 'medical', 7) ON CONFLICT (club_id, facility_type) DO NOTHING`, clubID); err != nil {
		t.Fatalf("write medical row: %v", err)
	}
	if lvl, err := medicalLevelFor(t, pool, ctx, clubID); err != nil {
		t.Fatalf("get medical level: %v", err)
	} else if lvl != 7 {
		t.Errorf("medical level (row) = %d, want 7", lvl)
	}
}

func medicalLevelFor(t *testing.T, pool *pgxpool.Pool, ctx context.Context, clubID uuid.UUID) (int, error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	lvl, err := MedicalLevel(ctx, tx, clubID)
	_ = tx.Rollback(ctx)
	return lvl, err
}

func TestRecurrenceForPlayer(t *testing.T) {
	ctx := context.Background()
	pool, _, _, players := injuryWorld(t)
	pid := players[0]

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	r, err := RecurrenceForPlayer(ctx, tx, pid)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatalf("recurrence (fresh): %v", err)
	}
	if r != 0 {
		t.Errorf("recurrence (fresh) = %.4f, want 0", r)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO player.injuries
			(player_id, injury_type, severity, expected_recovery_date, recurrence_risk, actual_recovery_date, occurred_at)
		VALUES ($1, 'muscle', 2, now()::date, 0.55, now()::date, now() - interval '10 days')`, pid); err != nil {
		t.Fatalf("insert completed injury: %v", err)
	}
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin2: %v", err)
	}
	r, err = RecurrenceForPlayer(ctx, tx2, pid)
	_ = tx2.Rollback(ctx)
	if err != nil {
		t.Fatalf("recurrence (fresh): %v", err)
	}
	if math.Abs(r-0.55) > 1e-3 {
		t.Errorf("recurrence after completed injury = %.4f, want 0.55", r)
	}
}
