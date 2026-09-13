//go:build integration

package form

import (
	"context"
	"testing"

	"github.com/touchline/backend/internal/testdb"
)

func TestStoreRoundTrip(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	worldID := testdb.CreateWorld(t, pool, "form-it")
	clubID := testdb.CreateClub(t, pool, worldID)

	store := NewStore(pool)
	if _, ok, err := store.Get(ctx, clubID); err != nil {
		t.Fatalf("get fresh club: %v", err)
	} else if ok {
		t.Fatal("fresh club must have no form row yet")
	}

	f := Neutral(clubID, 4)
	f = Update(f, 1.25, AlphaDefault, 5)
	f.FormString = "W-W"
	if err := store.Update(ctx, f); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	got, ok, err := store.Get(ctx, clubID)
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if !ok {
		t.Fatal("row must exist after upsert")
	}
	if !epsilon(got.CurrentRating, f.CurrentRating) || got.LastUpdatedTick != 5 || got.FormString != "W-W" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	f2 := Update(f, 0.8, AlphaDefault, 9)
	f2.FormString = "W-W-L"
	if err := store.Update(ctx, f2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, _, err = store.Get(ctx, clubID)
	if err != nil {
		t.Fatalf("get after second upsert: %v", err)
	}
	if got.LastUpdatedTick != 9 || got.FormString != "W-W-L" {
		t.Fatalf("upsert must overwrite, not duplicate: %+v", got)
	}

	// One row only — the PK must have absorbed both writes.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM club.form_state WHERE club_id = $1`, clubID).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected one form_state row, got %d", n)
	}
}

func TestWorldTick(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	// CreateWorld seeds current_tick=0; bump it so the read is meaningful.
	worldID := testdb.CreateWorld(t, pool, "form-tick")
	if _, err := pool.Exec(ctx,
		`UPDATE world.worlds SET current_tick = 42 WHERE id = $1`, worldID); err != nil {
		t.Fatalf("bump tick: %v", err)
	}

	store := NewStore(pool)
	tick, err := store.WorldTick(ctx, worldID)
	if err != nil {
		t.Fatalf("world tick: %v", err)
	}
	if tick != 42 {
		t.Fatalf("expected tick 42, got %d", tick)
	}
}

func TestDBSchemaEnforcesBand(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	worldID := testdb.CreateWorld(t, pool, "form-band")
	clubID := testdb.CreateClub(t, pool, worldID)

	// The CHECK constraint mirrors the Go clamp; a direct out-of-band insert
	// must be rejected by the database itself.
	if _, err := pool.Exec(ctx, `
		INSERT INTO club.form_state (club_id, current_rating, last_updated_tick, form_string)
		VALUES ($1, 1.2, 0, '')`, clubID); err == nil {
		t.Fatal("current_rating above the [0.85, 1.15] band must be rejected")
	}

	// And a valid boundary value is accepted.
	if _, err := pool.Exec(ctx, `
		INSERT INTO club.form_state (club_id, current_rating, last_updated_tick, form_string)
		VALUES ($1, 1.15, 0, '')`, clubID); err != nil {
		t.Fatalf("band boundary must be accepted: %v", err)
	}
}