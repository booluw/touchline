//go:build integration

package playerpool

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/internal/finance"
	"github.com/touchline/backend/internal/testdb"
	internalworld "github.com/touchline/backend/internal/world"
	"github.com/touchline/backend/pkg/playergen"
)

// seedSignWorld returns a world, country, two clubs (one human-owned, one AI),
// and a freshly seeded country pool. Registration follows the A06 lifecycle
// integration test: world + country + league seeded via the competition
// service, then an explicit pool so the free-agent list is non-empty.
func seedSignWorld(t *testing.T) (*pgxpool.Pool, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	testdb.SeedRefData(t, pool)
	testdb.SeedClubNameParts(t, pool)

	ctx := context.Background()
	w, err := internalworld.NewService(pool, nil).CreateWorld(ctx, "sign-it")
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
	names := []string{"Aaron", "Adam", "Alfie", "Archie", "Arthur", "Benjamin", "Charlie"}
	surnames := []string{"Adams", "Allen", "Anderson", "Atkinson", "Bailey", "Baker", "Ball"}
	if err := generator.AddPool("eng", names, surnames); err != nil {
		t.Fatalf("add name pool: %v", err)
	}
	natPool := playergen.NewNationalityPool()
	natPool.Add(playergen.Nationality{Code: "eng", Name: "England", Weight: 10})

	factory := playergen.NewPlayerFactory(generator, natPool, rand.New(rand.NewSource(7)))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := SeedPool(ctx, tx, nil, w.ID, &countryID, 5, factory, time.Now().UTC()); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	clubID := testdb.CreateClub(t, pool, w.ID)
	if _, err := finance.EnsureAccount(ctx, tx, w.ID, clubID); err != nil {
		t.Fatalf("ensure finance account: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit pool: %v", err)
	}

	return pool, w.ID, countryID, clubID, uuid.Nil
}

func TestSignReleaseIntegration(t *testing.T) {
	pool, worldID, countryID, clubID, _ := seedSignWorld(t)
	ctx := context.Background()

	filter := FreeAgentFilter{CountryID: &countryID}
	agents, _, err := ListFreeAgents(ctx, pool, worldID, filter, 1, 25)
	if err != nil {
		t.Fatalf("list free agents: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("no free agents seeded")
	}
	playerID := agents[0].ID

	t.Run("sign assigns club and creates contract", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := SignFreeAgent(ctx, tx, nil, worldID, playerID, clubID, 50000, 3, time.Now().UTC()); err != nil {
			t.Fatalf("sign: %v", err)
		}

		var status string
		var club *uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT status, club_id FROM player.players WHERE id = $1`, playerID,
		).Scan(&status, &club); err != nil {
			t.Fatalf("load player: %v", err)
		}
		if status != "active" || club == nil || *club != clubID {
			t.Fatalf("player status=%s club=%v, want active + club %s", status, club, clubID)
		}

		var contractCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM player.contracts
			WHERE player_id = $1 AND club_id = $2 AND status = 'active'`,
			playerID, clubID).Scan(&contractCount); err != nil {
			t.Fatalf("count contracts: %v", err)
		}
		if contractCount != 1 {
			t.Fatalf("active contracts = %d, want 1", contractCount)
		}

		var wageCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM finance.wage_commitments w
			JOIN player.contracts c ON c.id = w.contract_id
			WHERE c.player_id = $1 AND w.club_id = $2`,
			playerID, clubID).Scan(&wageCount); err != nil {
			t.Fatalf("count wage commitments: %v", err)
		}
		if wageCount != 1 {
			t.Fatalf("wage commitments = %d, want 1", wageCount)
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	})

	t.Run("cannot re-sign an owned player", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		err = SignFreeAgent(ctx, tx, nil, worldID, playerID, clubID, 30000, 1, time.Now().UTC())
		if !errors.Is(err, ErrNotFreeAgent) {
			t.Fatalf("re-sign err = %v, want ErrNotFreeAgent", err)
		}
	})

	t.Run("release returns player to pool", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := ReleasePlayer(ctx, tx, nil, playerID, "test release"); err != nil {
			t.Fatalf("release: %v", err)
		}

		var status string
		var club *uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT status, club_id FROM player.players WHERE id = $1`, playerID,
		).Scan(&status, &club); err != nil {
			t.Fatalf("load player: %v", err)
		}
		if status != "free_agent" || club != nil {
			t.Fatalf("player status=%s club=%v, want free_agent + nil club", status, club)
		}

		var activeContracts int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM player.contracts
			WHERE player_id = $1 AND status = 'active'`, playerID).Scan(&activeContracts); err != nil {
			t.Fatalf("count active contracts: %v", err)
		}
		if activeContracts != 0 {
			t.Fatalf("active contracts after release = %d, want 0", activeContracts)
		}

		var committed bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM finance.wage_commitments w
				JOIN player.contracts c ON c.id = w.contract_id
				WHERE c.player_id = $1 AND w.end_date > CURRENT_DATE)`,
			playerID).Scan(&committed); err != nil {
			t.Fatalf("check wage commitments: %v", err)
		}
		if committed {
			t.Fatal("wage commitment still active after release")
		}

		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	})

	t.Run("release of a free agent errors", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := ReleasePlayer(ctx, tx, nil, playerID, "again"); !errors.Is(err, ErrNoActiveContract) {
			t.Fatalf("release free agent err = %v, want ErrNoActiveContract", err)
		}
	})
}
