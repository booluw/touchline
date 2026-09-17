//go:build integration

package playerpool

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/playergen"
)

func seedBulkWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, *playergen.PlayerFactory) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	ctx := context.Background()
	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "bulk-it")
	if err != nil {
		t.Fatalf("create world: %v", err)
	}
	var countryID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO world.countries (world_id, code, name) VALUES ($1, 'eng', 'England')
		RETURNING id`, w.ID).Scan(&countryID); err != nil {
		t.Fatalf("create country: %v", err)
	}

	generator := &playergen.PoolGenerator{}
	if err := generator.AddPool("eng", []string{"Aaron", "Adam", "Alfie"}, []string{"Adams", "Allen", "Anderson"}); err != nil {
		t.Fatalf("add name pool: %v", err)
	}
	natPool := playergen.NewNationalityPool()
	natPool.Add(playergen.Nationality{Code: "eng", Name: "England", Weight: 10})
	factory := playergen.NewPlayerFactory(generator, natPool, rand.New(rand.NewSource(13)))
	return pool, w.ID, countryID, factory
}

func TestBulkCreateIntegration(t *testing.T) {
	pool, worldID, countryID, factory := seedBulkWorld(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	opts := BulkOpts{
		Count:       11,
		MinAge:      17,
		MaxAge:      19,
		Quality:     "elite",
		Positions:   []string{"GK", "ST"},
		Nationality: "eng",
	}
	ids, err := BulkCreate(ctx, tx, nil, worldID, countryID, opts, factory, time.Now().UTC())
	if err != nil {
		t.Fatalf("bulk create: %v", err)
	}
	if len(ids) != 11 {
		t.Fatalf("created %d players, want 11", len(ids))
	}

	// All players are free agents in the right country, teenagers, GK or ST.
	for _, id := range ids {
		var (
			status string
			club   *uuid.UUID
			pos    string
			origin string
			dob    time.Time
		)
		if err := pool.QueryRow(ctx, `
			SELECT p.status, p.club_id, p.primary_position, p.origin, pe.date_of_birth
			FROM player.players p JOIN person.people pe ON pe.id = p.person_id
			WHERE p.id = $1`, id).Scan(&status, &club, &pos, &origin, &dob); err != nil {
			t.Fatalf("load bulk player: %v", err)
		}
		if status != "free_agent" || club != nil {
			t.Fatalf("player %s status=%s club=%v, want free_agent nil", id, status, club)
		}
		if pos != "GK" && pos != "ST" {
			t.Errorf("player %s position = %s, want GK|ST", id, pos)
		}
		if origin != "generated" {
			t.Errorf("player %s origin = %s, want generated", id, origin)
		}
		age := time.Now().UTC().Year() - dob.Year()
		if age < 17 || age > 19 {
			t.Errorf("player %s age = %d, want 17..19", id, age)
		}
	}

	// Elite offset (+25) must lift the positional overall well above the low band.
	var lowOverall int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT p.id,
				AVG(a.value) FILTER (WHERE a.attribute_category = 'technical') AS t,
				AVG(a.value) FILTER (WHERE a.attribute_category = 'physical') AS ph,
				AVG(a.value) FILTER (WHERE a.attribute_category = 'mental') AS m
			FROM player.players p
			LEFT JOIN player.player_attributes a ON a.player_id = p.id
			WHERE p.id = ANY($1)
			GROUP BY p.id
		) agg WHERE COALESCE(t,0)+COALESCE(ph,0)+COALESCE(m,0) < 150`, ids).Scan(&lowOverall); err != nil {
		t.Fatalf("count low overall: %v", err)
	}
	if lowOverall > 0 {
		t.Errorf("%d elite players have a summed category mean under 150, want 0", lowOverall)
	}

	// Event recorded.
	var events int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM world.events
		WHERE world_id = $1 AND event_type = $2`, worldID, EventAdminBulkPlayerCreated).Scan(&events); err != nil {
		t.Fatalf("count bulk events: %v", err)
	}
	if events != 1 {
		t.Errorf("ADMIN_BULK_PLAYER_CREATED events = %d, want 1", events)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestBulkCreateValidatesRejectsBadOpts(t *testing.T) {
	pool, worldID, countryID, factory := seedBulkWorld(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := BulkCreate(ctx, tx, nil, worldID, countryID,
		BulkOpts{Count: 501, MinAge: 17, MaxAge: 25, Quality: "mid"}, factory, time.Now().UTC()); err == nil {
		t.Error("count=501 must be rejected")
	}
	if _, err := BulkCreate(ctx, tx, nil, worldID, countryID,
		BulkOpts{Count: 5, MinAge: 17, MaxAge: 25, Quality: "superstar"}, factory, time.Now().UTC()); err == nil {
		t.Error("unknown quality must be rejected")
	}
}
