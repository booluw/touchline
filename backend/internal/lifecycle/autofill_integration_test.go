//go:build integration

package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/academy"
	internalcompetition "github.com/touchline/backend/internal/competition"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
)

// seedAutofillWorld returns a world with one country + a 2-club league so
// AutoFill has AI clubs and a country pool to draw against.
func seedAutofillWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, []uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	ctx := context.Background()
	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "autofill-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	compSvc := internalcompetition.NewService(pool, nil)
	country, err := compSvc.CreateCountry(ctx, w.ID, "fra", "France")
	if err != nil {
		t.Fatalf("create country: %v", err)
	}
	if _, err := compSvc.CreateLeague(ctx, internalcompetition.LeagueParams{
		CountryID: country.ID, Name: "Ligue 1", Tier: 1, TeamCount: 2,
	}); err != nil {
		t.Fatalf("create league: %v", err)
	}
	if _, err := compSvc.SeedWorld(ctx, w.ID); err != nil {
		t.Fatalf("seed world: %v", err)
	}

	clubs, err := txSelect(ctx, pool, `SELECT id FROM club.clubs WHERE world_id = $1 ORDER BY id`, w.ID)
	if err != nil || len(clubs) == 0 {
		t.Fatalf("load clubs: %v (got %d)", err, len(clubs))
	}
	return pool, w.ID, country.ID, clubs
}

func txSelect(ctx context.Context, pool *pgxpool.Pool, q string, args ...any) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TestAutoFillRestoresThinSquad releases the first AI club's entire squad
// (leaving 0 active players), runs AutoFill, and asserts the squad is back at
// the target size with professional contracts, correct wage, and one
// AI_AUTO_FILL event.
func TestAutoFillRestoresThinSquad(t *testing.T) {
	pool, worldID, countryID, clubs := seedAutofillWorld(t)
	ctx := context.Background()
	svc := NewService(pool, nil, academy.NewService(pool, nil))
	club := clubs[0]

	// Simulate a squad gutted by retirements: every active player becomes a
	// free agent (the retirement path already nulls club_id + terminates
	// contracts; here we skip straight to the outcome).
	if _, err := pool.Exec(ctx, `
		UPDATE player.players SET club_id = NULL, status = 'free_agent', squad_number = NULL
		WHERE club_id = $1 AND status = 'active'`, club); err != nil {
		t.Fatalf("gut squad: %v", err)
	}
	if n := activePlayers(ctx, pool, club); n >= TargetSquadSize {
		t.Fatalf("squad not thinned: %d active players remain", n)
	}

	ref := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	total, err := svc.AutoFill(ctx, tx, worldID, countryID, 6, ref)
	if err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		t.Fatalf("autofill: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if total < TargetSquadSize {
		t.Errorf("AutoFill signed %d, want >=%d", total, TargetSquadSize)
	}

	after := activePlayers(ctx, pool, club)
	if after < TargetSquadSize {
		t.Errorf("squad after autofill = %d, want >=%d", after, TargetSquadSize)
	}

	// Every signed player has an active senior contract whose wage equals
	// overall*150 and belongs to the club again.
	var signed, wageMismatch int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM player.players p
		JOIN player.contracts c ON c.player_id = p.id AND c.status = 'active'
		WHERE p.club_id = $1`, club).Scan(&signed); err != nil {
		t.Fatalf("count signed: %v", err)
	}
	if signed < TargetSquadSize {
		t.Errorf("players with active contracts = %d, want >=%d", signed, TargetSquadSize)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM player.players p
		JOIN player.contracts c ON c.player_id = p.id AND c.status = 'active'
		JOIN (
			SELECT player_id, ROUND(AVG(value))::int AS overall
			FROM player.player_attributes GROUP BY player_id
		) a ON a.player_id = p.id
		WHERE p.club_id = $1 AND c.weekly_wage <> a.overall * 150`, club).Scan(&wageMismatch); err != nil {
		t.Fatalf("count wage mismatches: %v", err)
	}
	if wageMismatch != 0 {
		t.Errorf("contracts with wrong wage = %d, want 0", wageMismatch)
	}

	// One AI_AUTO_FILL event for this club.
	var events int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = $2 AND payload->>'club_id' = $3::text`,
		worldID, EventAIAutoFill, club.String()).Scan(&events); err != nil {
		t.Fatalf("count autofill events: %v", err)
	}
	if events != 1 {
		t.Errorf("AI_AUTO_FILL events for club = %d, want 1", events)
	}

	// Re-running with a full squad signs nothing and emits nothing new.
	before := activePlayers(ctx, pool, club)
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	again, err := svc.AutoFill(ctx, tx2, worldID, countryID, 6, ref)
	if err != nil {
		tx2.Rollback(ctx) //nolint:errcheck
		t.Fatalf("autofill re-run: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit tx2: %v", err)
	}
	if again != 0 {
		t.Errorf("re-run on full squad signed %d, want 0", again)
	}
	if n := activePlayers(ctx, pool, club); n != before {
		t.Errorf("squad size changed on re-run: %d -> %d", before, n)
	}
}

func activePlayers(ctx context.Context, pool *pgxpool.Pool, club uuid.UUID) int {
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM player.players WHERE club_id = $1 AND status = 'active'`, club).Scan(&n); err != nil {
		return -1
	}
	return n
}
