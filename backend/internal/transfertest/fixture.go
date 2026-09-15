//go:build integration

// Package transfertest provisions the S06-01 transfer-market world fixture
// shipped to the service-level and HTTP integration suites. It lives in its
// own package (rather than internal/testdb) so that importing the market
// bootstrap machinery never forces the testdb -> bootstrap/world cycle that
// would break their own integration test binaries: nothing imports back here.
package transfertest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalworld "github.com/touchline/backend/internal/world"

	"github.com/touchline/backend/internal/testdb"
)

// World is the transfer-market fixture: one human-owned starter club and two
// AI-controlled clubs, each bootstrapped with a full drafted squad, contracts,
// wage commitments and financial genesis.
type World struct {
	WorldID   uuid.UUID
	HumanClub uuid.UUID
	HumanMgr  uuid.UUID
	HumanUser uuid.UUID
	AIOneClub uuid.UUID
	AIOneMgr  uuid.UUID
	AITwoClub uuid.UUID
	AITwoMgr  uuid.UUID
}

// Provision boots the fixture described on World.
func Provision(t *testing.T, pool *pgxpool.Pool, name, email string) World {
	t.Helper()
	ctx := context.Background()
	testdb.SeedRefData(t, pool)

	worldSvc := internalworld.NewService(pool, nil)
	w, err := worldSvc.CreateWorld(ctx, name)
	if err != nil {
		t.Fatalf("create world: %v", err)
	}

	bs := internalbootstrap.NewService(pool, nil)
	res, err := bs.BootstrapWorld(ctx, w.ID, name+" One FC", "")
	if err != nil {
		t.Fatalf("bootstrap ai seller: %v", err)
	}

	// Two further AI clubs drafted from the replenished world pool inside one
	// transaction (S04-01-style league materialization).
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin ai clubs tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	two, err := internalbootstrap.GenerateAIClub(ctx, nil, tx, w.ID, name+" Two FC", "", "england", nil)
	if err != nil {
		t.Fatalf("generate ai club two: %v", err)
	}
	human, err := internalbootstrap.GenerateAIClub(ctx, nil, tx, w.ID, name+" Human FC", "", "england", nil)
	if err != nil {
		t.Fatalf("generate human club: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ai clubs: %v", err)
	}

	// Repurpose the third club as a human-managed one, mirroring the tactics
	// fixture: the bootstrap policy bot becomes the owner's manager seat.
	userID := testdb.CreateUser(t, pool, email, "s3cret", nil)
	if _, err := pool.Exec(ctx, `
		UPDATE manager.managers
		SET user_id = $1, is_policy_bot = FALSE
		WHERE id = $2 AND status = 'active'`, userID, human.ManagerID); err != nil {
		t.Fatalf("owner manager: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE club.clubs SET is_ai_controlled = FALSE WHERE id = $1`, human.ClubID); err != nil {
		t.Fatalf("make club human: %v", err)
	}

	if _, err := worldSvc.SetStatus(ctx, w.ID, "active"); err != nil {
		t.Fatalf("launch world: %v", err)
	}

	return World{
		WorldID:   w.ID,
		HumanClub: human.ClubID, HumanMgr: human.ManagerID, HumanUser: userID,
		AIOneClub: res.ClubID, AIOneMgr: res.ManagerID,
		AITwoClub: two.ClubID, AITwoMgr: two.ManagerID,
	}
}
