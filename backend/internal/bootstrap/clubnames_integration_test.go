//go:build integration

package bootstrap

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
)

func TestClubNamePartsRoundTrip(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedClubNameParts(t, pool)
	ctx := context.Background()
	svc := NewService(pool, nil)

	firstStems, firstSuffixes, err := svc.ListClubNameParts(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(firstStems) == 0 || len(firstSuffixes) == 0 {
		t.Fatalf("seeded pools are empty: %d stems, %d suffixes", len(firstStems), len(firstSuffixes))
	}

	// Add a new stem + suffix, then confirm they round-trip.
	if err := svc.AddClubNamePart(ctx, "stem", "Panther"); err != nil {
		t.Fatalf("add stem: %v", err)
	}
	if err := svc.AddClubNamePart(ctx, "suffix", "Wanderers"); err != nil {
		t.Fatalf("add suffix: %v", err)
	}
	stems, suffixes, err := svc.ListClubNameParts(ctx)
	if err != nil {
		t.Fatalf("list after add: %v", err)
	}
	if !contains(stems, "Panther") || !contains(suffixes, "Wanderers") {
		t.Fatalf("added parts missing: stems=%v suffixes=%v", stems, suffixes)
	}
	if countValue(stems, "Panther") != 1 || countValue(suffixes, "Wanderers") != 1 {
		t.Fatalf("expected exactly one Panther/Wanderers immediately after add")
	}

	// Add is idempotent (upsert).
	if err := svc.AddClubNamePart(ctx, "stem", "Panther"); err != nil {
		t.Fatalf("re-add stem: %v", err)
	}
	stems, _, err = svc.ListClubNameParts(ctx)
	if err != nil {
		t.Fatalf("list after re-add: %v", err)
	}
	if got := countValue(stems, "Panther"); got != 1 {
		t.Fatalf("duplicate after upsert: %d Panther(s)", got)
	}

	// Remove.
	if err := svc.RemoveClubNamePart(ctx, "stem", "Panther"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	stems, _, err = svc.ListClubNameParts(ctx)
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if contains(stems, "Panther") {
		t.Fatal("stem still present after remove")
	}

	// Removing an absent entry is a no-op.
	if err := svc.RemoveClubNamePart(ctx, "suffix", "Panther"); err != nil {
		t.Fatalf("remove absent: %v", err)
	}
}

func TestClubNamePartValidation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := NewService(pool, nil)

	if err := svc.AddClubNamePart(ctx, "prefix", "X"); err != ErrInvalidClubNamePart {
		t.Fatalf("invalid kind = %v, want ErrInvalidClubNamePart", err)
	}
	if err := svc.AddClubNamePart(ctx, "stem", "  "); err != ErrClubNamePartRequired {
		t.Fatalf("blank value = %v, want ErrClubNamePartRequired", err)
	}
	if err := svc.RemoveClubNamePart(ctx, "stem", ""); err != ErrClubNamePartRequired {
		t.Fatalf("blank remove = %v, want ErrClubNamePartRequired", err)
	}
}

func TestLoadClubNamePartsEmptyError(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	// Ref pool is shared across tests/databases, so isolate the emptiness
	// assertion inside a transaction that we roll back.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM ref.club_name_parts`); err != nil {
		t.Fatalf("clear pool: %v", err)
	}
	if _, _, err := LoadClubNameParts(ctx, tx); err == nil {
		t.Fatal("expected error on empty pool")
	}
}

func TestLoadClubNamePartsStableOrder(t *testing.T) {
	pool := testdb.New(t)
	testdb.SeedClubNameParts(t, pool)

	firstStems, firstSuffixes := poolRead(t, pool)
	secondStems, secondSuffixes := poolRead(t, pool)
	if !reflect.DeepEqual(firstStems, secondStems) || !reflect.DeepEqual(firstSuffixes, secondSuffixes) {
		t.Fatal("LoadClubNameParts must be stable across calls (deterministic seeding)")
	}
}

func poolRead(t *testing.T, pool *pgxpool.Pool) (stems, suffixes []string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	stems, suffixes, err = LoadClubNameParts(ctx, tx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return stems, suffixes
}

func countValue(xs []string, v string) int {
	n := 0
	for _, x := range xs {
		if x == v {
			n++
		}
	}
	return n
}
