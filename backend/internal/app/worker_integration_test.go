//go:build integration

package app

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	"github.com/touchline/backend/internal/transfertest"
	"github.com/touchline/backend/pkg/eventbus"
)

// driveWorkerDaily runs the single-daily-cadence dispatch for days 1..lastDay,
// advancing world.worlds.current_day/current_tick to D and invoking the
// extracted handler directly (the integration harness has no river loop, and
// the handler is the unit under test — the bus only feeds it). check runs after
// each day against the domain tables.
func driveWorkerDaily(t *testing.T, pool *pgxpool.Pool, a *App, worldID uuid.UUID, lastDay int64, check func(day int64)) {
	t.Helper()
	ctx := context.Background()
	for day := int64(1); day <= lastDay; day++ {
		if _, err := pool.Exec(ctx,
			`UPDATE world.worlds SET current_day = $1, current_tick = $1 WHERE id = $2`, day, worldID); err != nil {
			t.Fatalf("advance clock to day %d: %v", day, err)
		}
		ev := eventbus.Event{
			ID:        uuid.New(),
			WorldID:   worldID,
			WorldTick: day,
			EventType: "WORLD_TICK",
			Payload:   []byte(`{"granularity":"daily"}`),
		}
		if err := a.handleWorldTick(ctx, ev, "daily"); err != nil {
			t.Fatalf("day %d handleWorldTick: %v", day, err)
		}
		if check != nil {
			check(day)
		}
	}
}

// learnerClub plans the human club so the weekly training pass has a target.
func learnerClub(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO club.club_training_plans (club_id, archetype, effective_from_tick)
		VALUES ($1, 'physical', 0)`, clubID); err != nil {
		t.Fatalf("seed training plan: %v", err)
	}
}

// fundedAcademies gives every academy in the world a real running cost so the
// monthly maintenance pass posts ledger debits it can be measured on.
func fundedAcademies(t *testing.T, pool *pgxpool.Pool, a *App, worldID uuid.UUID) {
	t.Helper()
	if err := a.Academy.EnsureAcademies(context.Background(), worldID); err != nil {
		t.Fatalf("ensure academies: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE club.academies SET annual_cost = 1200000 WHERE world_id = $1`, worldID); err != nil {
		t.Fatalf("fund academies: %v", err)
	}
}

func TestSingleDailyCadenceDispatchDefaultCalendar(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w := transfertest.Provision(t, pool, "IM02 Daily Clock", "im02-daily@example.com")

	a, err := Build(ctx, Config{
		DatabaseURL: pool.Config().ConnConfig.ConnString(),
		JWTSecret:   "test-secret",
	})
	if err != nil {
		t.Fatalf("build app: %v", err)
	}
	defer a.Close()
	a.runnerEnabled = false // fixture world: no live matches to kick off

	learnerClub(t, pool, w.HumanClub)
	fundedAcademies(t, pool, a, w.WorldID)

	driveWorkerDaily(t, pool, a, w.WorldID, 30, func(day int64) {
		snapshots := countSnapshots(t, pool, w.WorldID)
		wageTicks := distinctLedgerTicks(t, pool, w.WorldID, "wages", `^wage:([0-9]+):`)
		academyTicks := distinctLedgerTicks(t, pool, w.WorldID, "academy", `^academy:maintenance:[0-9a-f-]+:([0-9]+)$`)

		// Monthly gate (days_per_month=30): the board review, wages and academy
		// maintenance fire exactly on day 30 — never on 7/14/21/28.
		if day%30 == 0 {
			if snapshots != 3 {
				t.Fatalf("day %d: board snapshots = %d, want 3", day, snapshots)
			}
			assertEqualTicks(t, day, wageTicks, []int64{30})
			assertEqualTicks(t, day, academyTicks, []int64{30})
		} else {
			if snapshots != 0 {
				t.Fatalf("day %d: board snapshots = %d (not a month boundary)", day, snapshots)
			}
			if len(wageTicks) != 0 {
				t.Fatalf("day %d: wages posted at ticks %v (month boundary only)", day, wageTicks)
			}
			if len(academyTicks) != 0 {
				t.Fatalf("day %d: academy posts at ticks %v (month boundary only)", day, academyTicks)
			}
		}

		// Weekly gate (days_per_week=7): the training plan's week stamp is the
		// last week day reached, i.e. floor(day/7)*7 once past day 7.
		stamp := trainingStamp(t, pool, w.HumanClub)
		want := int64(0)
		if day >= 7 {
			want = (day / 7) * 7
		}
		if want == 0 {
			want = -1 // NULL sentinel; applyClubWeekly stamps 7 onward
		}
		if stamp != want {
			t.Fatalf("day %d: training week stamp = %d, want %d", day, stamp, want)
		}
	})
}

func TestSingleDailyCadenceDispatchVariantCalendar(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w := transfertest.Provision(t, pool, "IM02 Variant Calendar", "im02-variant@example.com")

	// Configured week = 5 days, month = 10 days: the worker must derive its
	// passes from the world's calendar config, not compiled-in constants.
	setCalendar(t, pool, w.WorldID, 5, 10)

	a, err := Build(ctx, Config{
		DatabaseURL: pool.Config().ConnConfig.ConnString(),
		JWTSecret:   "test-secret",
	})
	if err != nil {
		t.Fatalf("build app: %v", err)
	}
	defer a.Close()
	a.runnerEnabled = false

	learnerClub(t, pool, w.HumanClub)
	fundedAcademies(t, pool, a, w.WorldID)

	driveWorkerDaily(t, pool, a, w.WorldID, 30, func(day int64) {
		snapshots := countSnapshots(t, pool, w.WorldID)
		wageTicks := distinctLedgerTicks(t, pool, w.WorldID, "wages", `^wage:([0-9]+):`)
		academyTicks := distinctLedgerTicks(t, pool, w.WorldID, "academy", `^academy:maintenance:[0-9a-f-]+:([0-9]+)$`)

		if day%10 == 0 {
			if snapshots != 3 {
				t.Fatalf("day %d: board snapshots = %d, want 3", day, snapshots)
			}
			assertEqualTicks(t, day, wageTicks, []int64{10, 20, 30})
			assertEqualTicks(t, day, academyTicks, []int64{10, 20, 30})
		} else if snapshots != 0 {
			t.Fatalf("day %d: board snapshots = %d (not a 10-day month boundary)", day, snapshots)
		}

		// 5-day week: the last week day reached by day D is D - D%5 (or 0).
		stamp := trainingStamp(t, pool, w.HumanClub)
		want := int64(0)
		if day >= 5 {
			want = day - day%5
		}
		if want == 0 {
			want = -1
		}
		if stamp != want {
			t.Fatalf("day %d: training week stamp = %d, want %d", day, stamp, want)
		}
	})
}

// TestSingleDailyCadenceDispatchSeasonalFallback drives a league-less world
// straight to day 364 (days_per_week=7, days_per_month=30): the seasonal
// fallback (Academy days_per_season) fires without error while the monthly
// gate (364 % 30 != 0) stays silent.
func TestSingleDailyCadenceDispatchSeasonalFallback(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	w := transfertest.Provision(t, pool, "IM02 Seasonal Fallback", "im02-seasonal@example.com")

	a, err := Build(ctx, Config{
		DatabaseURL: pool.Config().ConnConfig.ConnString(),
		JWTSecret:   "test-secret",
	})
	if err != nil {
		t.Fatalf("build app: %v", err)
	}
	defer a.Close()
	a.runnerEnabled = false

	driveWorkerDaily(t, pool, a, w.WorldID, 364, nil)

	if got := countSnapshots(t, pool, w.WorldID); got != 0 {
		t.Fatalf("seasonal fallback day 364: board snapshots = %d, want 0 (364 %% 30 != 0)", got)
	}
	if got := distinctLedgerTicks(t, pool, w.WorldID, "wages", `^wage:([0-9]+):`); len(got) != 0 {
		t.Fatalf("seasonal fallback day 364: wages posted at %v", got)
	}
}

// ---- assertion helpers ----

func setCalendar(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, daysPerWeek, daysPerMonth int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO world.world_config (world_id, config_key, config_value) VALUES
			($1, 'calendar.days_per_week', to_jsonb($2::int)),
			($1, 'calendar.days_per_month', to_jsonb($3::int))
		ON CONFLICT (world_id, config_key) DO UPDATE
			SET config_value = EXCLUDED.config_value, updated_at = now()`, worldID, daysPerWeek, daysPerMonth)
	if err != nil {
		t.Fatalf("set calendar config: %v", err)
	}
}

func countSnapshots(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM manager.job_security_snapshots s
		JOIN club.clubs c ON c.id = s.club_id
		WHERE c.world_id = $1`, worldID).Scan(&n); err != nil {
		t.Fatalf("count board snapshots: %v", err)
	}
	return n
}

func distinctLedgerTicks(t *testing.T, pool *pgxpool.Pool, worldID uuid.UUID, category, re string) []int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT DISTINCT substring(e.dedup_key from $3)::bigint
		FROM finance.ledger_entries e
		JOIN finance.accounts ac ON ac.id = e.account_id
		JOIN club.clubs c ON c.id = ac.club_id
		WHERE c.world_id = $1 AND e.category = $2 AND e.dedup_key ~ $3
		ORDER BY 1`, worldID, category, re)
	if err != nil {
		t.Fatalf("distinct %s ticks: %v", category, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var tick int64
		if err := rows.Scan(&tick); err != nil {
			t.Fatalf("scan %s tick: %v", category, err)
		}
		out = append(out, tick)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s ticks: %v", category, err)
	}
	return out
}

func trainingStamp(t *testing.T, pool *pgxpool.Pool, clubID uuid.UUID) int64 {
	t.Helper()
	var stamp *int64
	if err := pool.QueryRow(context.Background(),
		`SELECT last_applied_week FROM club.club_training_plans WHERE club_id = $1`, clubID).Scan(&stamp); err != nil {
		t.Fatalf("read training week stamp: %v", err)
	}
	if stamp == nil {
		return -1
	}
	return *stamp
}

func assertEqualTicks(t *testing.T, day int64, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("day %d: ticks = %v, want %v", day, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("day %d: ticks = %v, want %v", day, got, want)
		}
	}
}
