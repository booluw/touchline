//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/touchline/backend/internal/auth"
	"github.com/touchline/backend/internal/testdb"
	pkgauth "github.com/touchline/backend/pkg/auth"
)

func testCfg() pkgauth.JWTConfig {
	return pkgauth.JWTConfig{
		Secret:     "integration-test-secret",
		AccessTTL:  time.Hour,
		RefreshTTL: 30 * 24 * time.Hour,
	}
}

func TestLogin_SingleActiveWorld(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-A")
	testdb.CreateUser(t, pool, "single@example.com", "s3cret", []testdb.Join{{WorldID: w}})

	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "single@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Worlds != nil {
		t.Fatalf("unexpected world list, want a session: %+v", res.Worlds)
	}
	if res.Identity == nil || res.TokenPair == nil {
		t.Fatal("expected a session")
	}
	if res.Identity.WorldID != w {
		t.Errorf("WorldID = %v, want %v", res.Identity.WorldID, w)
	}

	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM auth.sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}

	// The raw refresh token must never be stored — only its hash.
	var storedHash string
	if err := pool.QueryRow(context.Background(), `SELECT refresh_token_hash FROM auth.sessions`).Scan(&storedHash); err != nil {
		t.Fatalf("read session hash: %v", err)
	}
	if storedHash == res.TokenPair.RefreshToken || storedHash == "" {
		t.Error("refresh_token_hash must be a hash, not the raw token")
	}
	if storedHash != pkgauth.HashRefreshToken(res.TokenPair.RefreshToken) {
		t.Error("stored hash does not match hashed refresh token")
	}
}

func TestLogin_JobWorldWinsOverActive(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	worldA := testdb.CreateWorld(t, pool, "W-A") // active but jobless
	worldB := testdb.CreateWorld(t, pool, "W-B") // employed
	testdb.CreateUser(t, pool, "job@example.com", "s3cret", []testdb.Join{
		{WorldID: worldA, Status: "active"},
		{WorldID: worldB, Employed: true},
	})

	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "job@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Identity.WorldID != worldB {
		t.Errorf("login must default to the job world, got %v want %v", res.Identity.WorldID, worldB)
	}
}

func TestLogin_MultiActiveWorlds_ReturnsPicker(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	worldA := testdb.CreateWorld(t, pool, "W-A")
	worldB := testdb.CreateWorld(t, pool, "W-B")
	testdb.CreateUser(t, pool, "multi@example.com", "s3cret", []testdb.Join{
		{WorldID: worldA, Status: "active"},
		{WorldID: worldB, Status: "active"},
	})

	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "multi@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.TokenPair != nil || res.Identity != nil {
		t.Fatal("jobless multi-world must NOT mint a session")
	}
	if len(res.Worlds) != 2 {
		t.Fatalf("want 2 world options, got %+v", res.Worlds)
	}

	// An explicit pick binds the session to the chosen world.
	wid := worldA
	res, err = svc.Login(context.Background(), auth.LoginParams{Email: "multi@example.com", Password: "s3cret", WorldID: &wid})
	if err != nil {
		t.Fatalf("login with pick: %v", err)
	}
	if res.Identity == nil || res.Identity.WorldID != worldA {
		t.Errorf("picked world must be %v, got %+v", worldA, res.Identity)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-A")
	testdb.CreateUser(t, pool, "creds@example.com", "right-pass", []testdb.Join{{WorldID: w}})

	if _, err := svc.Login(context.Background(), auth.LoginParams{Email: "creds@example.com", Password: "wrong"}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("wrong password: got %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(context.Background(), auth.LoginParams{Email: "ghost@example.com", Password: "right-pass"}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("unknown email: got %v, want ErrInvalidCredentials", err)
	}

	// A failed login must not create a session.
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM auth.sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("failed logins created %d sessions", n)
	}
}

func TestLogin_NoManager(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	testdb.CreateUser(t, pool, "none@example.com", "s3cret", nil)

	_, err := svc.Login(context.Background(), auth.LoginParams{Email: "none@example.com", Password: "s3cret"})
	if !errors.Is(err, auth.ErrNoManager) {
		t.Errorf("got %v, want ErrNoManager", err)
	}
}

func TestLogin_UniqueIndexesEnforceWorldScoping(t *testing.T) {
	pool := testdb.New(t)

	w := testdb.CreateWorld(t, pool, "W-A")
	w2 := testdb.CreateWorld(t, pool, "W-B")
	testdb.CreateUser(t, pool, "idx@example.com", "s3cret", []testdb.Join{{WorldID: w, Employed: true}})

	var userID, firstManagerID string
	if err := pool.QueryRow(context.Background(),
		`SELECT m.user_id, m.id FROM manager.managers m WHERE m.world_id = $1`, w).Scan(&userID, &firstManagerID); err != nil {
		t.Fatalf("read manager: %v", err)
	}

	// uq_managers_user_world: a second row for the same user in the same world
	// must be rejected.
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO manager.managers (world_id, user_id, status) VALUES ($1, $2, 'unemployed')`, w, userID); err == nil {
		t.Fatal("duplicate same-world manager row was NOT rejected (uq_managers_user_world)")
	}

	// A row in ANOTHER world is allowed (multi-world membership).
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO manager.managers (world_id, user_id, status) VALUES ($1, $2, 'unemployed')`, w2, userID); err != nil {
		t.Fatalf("second-world row should be allowed: %v", err)
	}

	// uq_manager_one_job_per_user: promoting that second-world row to employed
	// while the first row is still employed must be rejected.
	clubID := testdb.CreateClub(t, pool, w2)
	if _, err := pool.Exec(context.Background(), `
		UPDATE manager.managers SET status = 'active', current_club_id = $1
		WHERE world_id = $2 AND user_id = $3`, clubID, w2, userID); err == nil {
		t.Fatal("second global job was NOT rejected (uq_manager_one_job_per_user)")
	}

	// Sanity: the first manager is untouched.
	if err := pool.QueryRow(context.Background(), `SELECT status FROM manager.managers WHERE id = $1`, firstManagerID).Scan(new(string)); err != nil {
		t.Fatalf("first manager intact: %v", err)
	}
}

func TestRefresh_RotatesAndRevokes(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-A")
	testdb.CreateUser(t, pool, "rot@example.com", "s3cret", []testdb.Join{{WorldID: w}})

	first, err := svc.Login(context.Background(), auth.LoginParams{Email: "rot@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	refreshed, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Identity.WorldID != w {
		t.Errorf("refresh must stay in the login world, got %v", refreshed.Identity.WorldID)
	}

	// The old refresh token must now be rejected (rotation).
	if _, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, ""); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Errorf("reused old refresh: got %v, want ErrInvalidRefresh", err)
	}

	// The new token must work (and rotate again).
	if _, err := svc.Refresh(context.Background(), refreshed.TokenPair.RefreshToken, nil, ""); err != nil {
		t.Errorf("new refresh: %v", err)
	}

	// Net effect: exactly one live session (revoked rows still exist as audit).
	var live int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM auth.sessions WHERE revoked_at IS NULL`).Scan(&live); err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	if live != 1 {
		t.Errorf("live sessions = %d, want 1", live)
	}
}

func TestRefresh_RejectsGarbage(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	if _, err := svc.Refresh(context.Background(), "not-a-jwt", nil, ""); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Errorf("garbage refresh: got %v, want ErrInvalidRefresh", err)
	}
}
