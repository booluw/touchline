//go:build integration

package academy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/eventbus"
)

// medicalFixture boots one active world with a human-owned club and returns the
// pool, world id, club id and owner manager id.
func medicalFixture(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, "academy-medical")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	res, err := internalbootstrap.NewService(pool, nil).BootstrapWorld(ctx, w.ID, "Harbour Medical FC", "")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	userID := testdb.CreateUser(t, pool, "medical-owner@example.com", "s3cret", nil)
	if _, err := pool.Exec(ctx, `
		UPDATE manager.managers
		SET user_id = $1, is_policy_bot = FALSE
		WHERE current_club_id = $2 AND status = 'active'`, userID, res.ClubID); err != nil {
		t.Fatalf("owner manager: %v", err)
	}
	var managerID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT id FROM manager.managers WHERE current_club_id = $1 AND status = 'active' LIMIT 1`,
		res.ClubID).Scan(&managerID); err != nil {
		t.Fatalf("load owner: %v", err)
	}
	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}
	return pool, w.ID, res.ClubID, managerID
}

// TestUpgradeMedicalFacility verifies the neutral default, the billed step
// upgrade (finance ledger under a per-target dedup key), the emitted event and
// ownership gating.
func TestUpgradeMedicalFacility(t *testing.T) {
	ctx := context.Background()
	pool, worldID, clubID, managerID := medicalFixture(t)
	svc := NewService(pool, logOnlyBus{})

	// Neutral default 5 with no materialised row.
	f, err := svc.GetMedicalFacility(ctx, clubID)
	if err != nil {
		t.Fatalf("get medical: %v", err)
	}
	if f.Level != MedicalNeutralLevel || f.UpgradedAt != nil {
		t.Fatalf("neutral medical = %+v, want level 5 / nil upgraded_at", f)
	}

	// Upgrade 5 -> 6.
	up, err := svc.UpgradeMedical(ctx, managerID, clubID)
	if err != nil {
		t.Fatalf("upgrade medical: %v", err)
	}
	if up.Level != 6 || up.UpgradedAt == nil {
		t.Fatalf("upgraded medical = %+v, want level 6 / upgraded_at set", up)
	}
	var amount int64
	if err := pool.QueryRow(ctx, `
		SELECT l.amount FROM finance.ledger_entries l
		JOIN finance.accounts a ON a.id = l.account_id
		WHERE a.club_id = $1 AND l.dedup_key = $2`,
		clubID, "medical:upgrade:"+clubID.String()+":6").Scan(&amount); err != nil {
		t.Fatalf("load ledger debit: %v", err)
	}
	if want := MedicalUpgradeCost(5, 6); amount != want {
		t.Errorf("ledger debit = %d, want %d", amount, want)
	}
	var events int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events WHERE world_id = $1 AND event_type = $2`,
		worldID, EventMedicalUpgrade).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Errorf("MEDICAL_FACILITY_UPGRADED events = %d, want 1", events)
	}

	// A manager who does not hold the club is refused.
	foreign := uuid.New()
	if _, err := svc.UpgradeMedical(ctx, foreign, clubID); !errors.Is(err, ErrNotOwned) {
		t.Errorf("foreign upgrade err = %v, want ErrNotOwned", err)
	}

	// Drive to the cap and confirm the further upgrade is a silent no-op.
	for {
		cur, err := svc.GetMedicalFacility(ctx, clubID)
		if err != nil {
			t.Fatalf("get medical: %v", err)
		}
		if cur.Level >= MedicalMaxLevel {
			break
		}
		if _, err := svc.UpgradeMedical(ctx, managerID, clubID); err != nil {
			t.Fatalf("upgrade to cap: %v", err)
		}
	}
	before, err := svc.GetMedicalFacility(ctx, clubID)
	if err != nil {
		t.Fatalf("get medical at cap: %v", err)
	}
	after, err := svc.UpgradeMedical(ctx, managerID, clubID)
	if err != nil {
		t.Fatalf("upgrade at cap: %v", err)
	}
	if after.Level != MedicalMaxLevel || after.Level != before.Level {
		t.Errorf("capped upgrade = %d, want %d", after.Level, before.Level)
	}
	if after.UpgradedAt == nil || !after.UpgradedAt.Equal(*before.UpgradedAt) {
		t.Errorf("capped upgrade changed upgraded_at: %v -> %v", before.UpgradedAt, after.UpgradedAt)
	}
}

// TestMedicalUpgradeCost pins the cumulative cost curve used by the command.
func TestMedicalUpgradeCost(t *testing.T) {
	if got := MedicalUpgradeCost(5, 5); got != 0 {
		t.Errorf("MedicalUpgradeCost(5,5) = %d, want 0", got)
	}
	if got, want := MedicalUpgradeCost(5, 6), int64(1_000_000); got != want {
		t.Errorf("MedicalUpgradeCost(5,6) = %d, want %d", got, want)
	}
	if got, want := MedicalUpgradeCost(1, 10), MedicalCostBase*int64(10*9-0)/2; got != want {
		t.Errorf("MedicalUpgradeCost(1,10) = %d, want %d", got, want)
	}
}

// logOnlyBus records academy events into world.events inside the caller's tx
// without enqueueing any dispatch job.
type logOnlyBus struct{}

func (logOnlyBus) PublishTx(ctx context.Context, tx pgx.Tx, e *eventbus.Event) error {
	return eventbus.RecordTx(ctx, tx, e)
}
