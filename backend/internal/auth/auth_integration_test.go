//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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

func TestLogin_AdminSession(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-A")
	userID := testdb.CreateUser(t, pool, "admin@example.com", "s3cret", []testdb.Join{{WorldID: w}})
	testdb.MakeAdmin(t, pool, userID)

	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "admin@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Identity == nil || res.TokenPair == nil {
		t.Fatal("expected a console session")
	}
	// An admin session is world-less: admins run the global console, not a
	// manager's world.
	if res.Identity.WorldID != uuid.Nil || res.Identity.ManagerID != uuid.Nil {
		t.Errorf("console identity must be world-less, got %+v", res.Identity)
	}
	if res.Identity.UserID != userID {
		t.Errorf("UserID = %v, want %v", res.Identity.UserID, userID)
	}
	if !res.IsAdmin {
		t.Error("admin login must report is_admin=true")
	}
	// An admin runs the global console; there is no manager club to surface.
	if res.Club != nil {
		t.Errorf("admin login must carry no club, got %+v", res.Club)
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

func TestLogin_AdminWithoutWorld(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	// The launch sequence: cmd/user-create creates the first admin on an empty
	// DB (no -world-id), so there is no manager row to resolve.
	userID := testdb.CreateUser(t, pool, "root@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, userID)

	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "root@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Identity == nil || res.Identity.WorldID != uuid.Nil {
		t.Errorf("world-less admin login, got %+v", res.Identity)
	}
}

func TestLogin_ManagerSingleWorld(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-A")
	userID := testdb.CreateUser(t, pool, "pro@example.com", "s3cret", []testdb.Join{{WorldID: w}})

	var managerID uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM manager.managers WHERE world_id = $1`, w).Scan(&managerID); err != nil {
		t.Fatalf("read manager: %v", err)
	}

	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "pro@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	// OPD-15(4)(c): a non-admin with exactly one joined world logs into it.
	if res.Identity == nil || res.TokenPair == nil {
		t.Fatal("expected a manager session")
	}
	if res.Identity.ManagerID != managerID || res.Identity.WorldID != w {
		t.Errorf("identity = %+v, want manager %s in world %s", res.Identity, managerID, w)
	}
	if res.Identity.UserID != userID {
		t.Errorf("UserID = %v, want %v", res.Identity.UserID, userID)
	}
	if res.IsAdmin {
		t.Error("manager login must report is_admin=false")
	}
	if res.Worlds != nil {
		t.Errorf("single-world login returned a picker: %+v", res.Worlds)
	}
	// A jobless manager (no current club) surfaces a nil club on login.
	if res.Club != nil {
		t.Errorf("unemployed login must carry no club, got %+v", res.Club)
	}
}

func TestLogin_ManagerJobWins(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	wJob := testdb.CreateWorld(t, pool, "W-JOB")
	wOther := testdb.CreateWorld(t, pool, "W-OTHER")
	testdb.CreateUser(t, pool, "job@example.com", "s3cret", []testdb.Join{
		{WorldID: wJob, Employed: true},
		{WorldID: wOther},
	})

	var managerID, worldID uuid.UUID
	var clubID *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT id, world_id, current_club_id FROM manager.managers WHERE user_id = (SELECT id FROM auth.users WHERE email = 'job@example.com') AND status = 'active'`,
	).Scan(&managerID, &worldID, &clubID); err != nil {
		t.Fatalf("read employed manager: %v", err)
	}
	if clubID == nil {
		t.Fatal("test fixture must employ the manager (current_club_id set)")
	}

	// OPD-15(4)(a): the world where the account has a job always wins.
	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "job@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.Identity == nil || res.Identity.ManagerID != managerID || res.Identity.WorldID != worldID {
		t.Errorf("job world must win, got %+v (want manager %s in %s)", res.Identity, managerID, worldID)
	}
	// The login response carries the manager's current club.
	if res.Club == nil {
		t.Fatal("employed login must carry the current club")
	}
	if res.Club.ID != *clubID {
		t.Errorf("login club.ID = %v, want %v", res.Club.ID, *clubID)
	}
	if res.Club.Name == "" {
		t.Error("login club missing name")
	}

	// The rotation path must re-derive the same club for the same manager.
	refreshed, err := svc.Refresh(context.Background(), res.TokenPair.RefreshToken, nil, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Club == nil || refreshed.Club.ID != *clubID {
		t.Errorf("refresh club = %+v, want id %v", refreshed.Club, *clubID)
	}
}

func TestLogin_WorldPicker(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w1 := testdb.CreateWorld(t, pool, "W-1")
	w2 := testdb.CreateWorld(t, pool, "W-2")
	testdb.CreateUser(t, pool, "multi@example.com", "s3cret", []testdb.Join{{WorldID: w1}, {WorldID: w2}})

	// OPD-15(4)(d): a jobless multi-world account gets the picker, no session.
	res, err := svc.Login(context.Background(), auth.LoginParams{Email: "multi@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if res.TokenPair != nil || res.Identity != nil {
		t.Fatal("picker response must not mint a session")
	}
	if len(res.Worlds) != 2 {
		t.Fatalf("worlds = %+v, want 2 choices", res.Worlds)
	}
	got := map[uuid.UUID]bool{}
	for _, w := range res.Worlds {
		got[w.ID] = true
	}
	if !got[w1] || !got[w2] {
		t.Errorf("picker must offer both joined worlds, got %+v", res.Worlds)
	}
}

func TestLogin_ExplicitWorldID(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w1 := testdb.CreateWorld(t, pool, "W-1")
	w2 := testdb.CreateWorld(t, pool, "W-2")
	testdb.CreateUser(t, pool, "pick@example.com", "s3cret", []testdb.Join{{WorldID: w1}, {WorldID: w2}})

	// OPD-15(4)(b): re-posting the picker response with a chosen world_id.
	res, err := svc.Login(context.Background(), auth.LoginParams{
		Email: "pick@example.com", Password: "s3cret", WorldID: &w2,
	})
	if err != nil {
		t.Fatalf("login with world_id: %v", err)
	}
	if res.Identity == nil || res.Identity.WorldID != w2 {
		t.Errorf("explicit pick must mint the chosen world, got %+v", res.Identity)
	}

	// A world the account is not a member of is rejected.
	other := testdb.CreateWorld(t, pool, "W-OTHER")
	if _, err := svc.Login(context.Background(), auth.LoginParams{
		Email: "pick@example.com", Password: "s3cret", WorldID: &other,
	}); !errors.Is(err, auth.ErrNotMember) {
		t.Errorf("wrong world: got %v, want ErrNotMember", err)
	}
}

func TestLogin_NoManager(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	// A plain account with no manager row (no world joined) is refused.
	testdb.CreateUser(t, pool, "raw@example.com", "s3cret", nil)

	_, err := svc.Login(context.Background(), auth.LoginParams{Email: "raw@example.com", Password: "s3cret"})
	if !errors.Is(err, auth.ErrNoManager) {
		t.Errorf("got %v, want ErrNoManager", err)
	}

	// A rejected login must not create a session.
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM auth.sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("blocked login created %d sessions", n)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	userID := testdb.CreateUser(t, pool, "creds@example.com", "right-pass", nil)
	testdb.MakeAdmin(t, pool, userID)

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

	userID := testdb.CreateUser(t, pool, "rot@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, userID)

	first, err := svc.Login(context.Background(), auth.LoginParams{Email: "rot@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	refreshed, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Identity.WorldID != uuid.Nil {
		t.Errorf("admin refresh must stay world-less, got %v", refreshed.Identity.WorldID)
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

func TestRefresh_ManagerSession(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-A")
	testdb.CreateUser(t, pool, "mgr@example.com", "s3cret", []testdb.Join{{WorldID: w}})

	first, err := svc.Login(context.Background(), auth.LoginParams{Email: "mgr@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// A manager session (world-bound manager row) must survive refresh with the
	// same world — the JWT carries no world_id (OPD-15); it re-derives from the
	// manager row.
	refreshed, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Identity.ManagerID != first.Identity.ManagerID ||
		refreshed.Identity.WorldID != first.Identity.WorldID {
		t.Errorf("refresh changed identity: %+v -> %+v", first.Identity, refreshed.Identity)
	}
	// Unemployed at first login: no club on either response.
	if first.Club != nil {
		t.Errorf("unemployed login club = %+v, want nil", first.Club)
	}
	if refreshed.Club != nil {
		t.Errorf("unemployed refresh club = %+v, want nil", refreshed.Club)
	}

	// Accepting a job mid-session must surface the new club on the next refresh.
	clubID := testdb.CreateClub(t, pool, w)
	if _, err := pool.Exec(context.Background(),
		`UPDATE manager.managers SET status = 'active', current_club_id = $1 WHERE id = $2`,
		clubID, first.Identity.ManagerID); err != nil {
		t.Fatalf("employ manager: %v", err)
	}
	mid, err := svc.Refresh(context.Background(), refreshed.TokenPair.RefreshToken, nil, "")
	if err != nil {
		t.Fatalf("refresh after employment: %v", err)
	}
	if mid.Club == nil || mid.Club.ID != clubID {
		t.Errorf("refresh after employment club = %+v, want id %v", mid.Club, clubID)
	}
}

func TestRefresh_RevokedAdminBlocked(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	userID := testdb.CreateUser(t, pool, "rev@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, userID)

	first, err := svc.Login(context.Background(), auth.LoginParams{Email: "rev@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Admin rights revoked: a live session must stop refreshing, not carry on
	// as an admin forever.
	if _, err := pool.Exec(context.Background(),
		`UPDATE auth.users SET is_admin = FALSE WHERE id = $1`, userID); err != nil {
		t.Fatalf("revoke admin: %v", err)
	}

	if _, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, ""); !errors.Is(err, auth.ErrNotAuthorized) {
		t.Errorf("got %v, want ErrNotAuthorized after admin revocation", err)
	}
}

func TestRefresh_RejectsGarbage(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	if _, err := svc.Refresh(context.Background(), "not-a-jwt", nil, ""); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Errorf("garbage refresh: got %v, want ErrInvalidRefresh", err)
	}
}

// liveSessions returns the LIVE (not yet revoked/deleted) session count for one
// account — the one-session-per-user invariant (IM13).
func liveSessions(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM auth.sessions WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&n); err != nil {
		t.Fatalf("count live sessions: %v", err)
	}
	return n
}

func TestLogin_ReplacesPriorSession(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	userID := testdb.CreateUser(t, pool, "one@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, userID)

	first, err := svc.Login(context.Background(), auth.LoginParams{Email: "one@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if n := liveSessions(t, pool, userID); n != 1 {
		t.Fatalf("live sessions after first login = %d, want 1", n)
	}

	// Logging in again must invalidate AND delete the previous session: the old
	// refresh token can no longer refresh, and only one live row remains.
	second, err := svc.Login(context.Background(), auth.LoginParams{Email: "one@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if n := liveSessions(t, pool, userID); n != 1 {
		t.Errorf("live sessions after second login = %d, want 1", n)
	}
	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM auth.sessions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if total != 1 {
		t.Errorf("total sessions = %d, want 1 (prior session hard-deleted, not tombstoned)", total)
	}
	if _, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, ""); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Errorf("refreshing replaced session: got %v, want ErrInvalidRefresh", err)
	}
	if _, err := svc.Refresh(context.Background(), second.TokenPair.RefreshToken, nil, ""); err != nil {
		t.Errorf("refreshing current session: %v", err)
	}

	// The partial unique index is the DB-level guarantee; a raw INSERT of a
	// second live row must fail (23505) even bypassing the service.
	_, err = pool.Exec(context.Background(),
		`INSERT INTO auth.sessions (user_id, refresh_token_hash, expires_at)
		 VALUES ($1, $2, now() + interval '1 day')`,
		userID, pkgauth.HashRefreshToken("duplicate"))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("inserting a second live session: got %v, want unique_violation 23505", err)
	}
}

func TestLogout_DeletesServingSession(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	userID := testdb.CreateUser(t, pool, "out@example.com", "s3cret", nil)
	testdb.MakeAdmin(t, pool, userID)

	first, err := svc.Login(context.Background(), auth.LoginParams{Email: "out@example.com", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if n := liveSessions(t, pool, userID); n != 1 {
		t.Fatalf("live sessions before logout = %d, want 1", n)
	}

	if err := svc.Logout(context.Background(), first.TokenPair.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// The server-side session is gone: no session remains, and the refresh
	// token that identified it is rejected.
	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM auth.sessions WHERE user_id = $1`, userID).Scan(&total); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if total != 0 {
		t.Errorf("sessions after logout = %d, want 0 (row deleted, not tombstoned)", total)
	}
	if _, err := svc.Refresh(context.Background(), first.TokenPair.RefreshToken, nil, ""); !errors.Is(err, auth.ErrInvalidRefresh) {
		t.Errorf("refresh after logout: got %v, want ErrInvalidRefresh", err)
	}

	// Logout must be idempotent: repeating it, or logging out a session that
	// never existed (or with no token at all), is never an error.
	if err := svc.Logout(context.Background(), first.TokenPair.RefreshToken); err != nil {
		t.Errorf("repeat logout: %v", err)
	}
	if err := svc.Logout(context.Background(), "not-a-real-refresh-token"); err != nil {
		t.Errorf("logout unknown token: %v", err)
	}
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Errorf("logout empty token: %v", err)
	}
}

func TestRegister_SinglePlayableWorld(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	w := testdb.CreateWorld(t, pool, "W-ONBOARD")

	res, err := svc.Register(context.Background(), auth.RegisterParams{
		Email: "NewPlayer@Touchline.Local", Password: "s3cret", DisplayName: "New Player",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.JoinedWorld == nil || res.ManagerID == nil {
		t.Fatalf("expected a world auto-join, got %+v", res)
	}
	if res.JoinedWorld.ID != w || res.JoinedWorld.Status != "active" {
		t.Errorf("JoinedWorld = %+v, want world %s active", res.JoinedWorld, w)
	}
	if res.DisplayName != "New Player" {
		t.Errorf("DisplayName = %q, want the supplied name", res.DisplayName)
	}

	// The account exists as a plain non-admin unemployed manager in the world.
	var isAdmin bool
	var status string
	var clubID *uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT u.is_admin, m.status, m.current_club_id
		FROM auth.users u JOIN manager.managers m ON m.user_id = u.id
		WHERE u.id = $1`, res.UserID).Scan(&isAdmin, &status, &clubID); err != nil {
		t.Fatalf("read joined account: %v", err)
	}
	if isAdmin {
		t.Error("registered account must not be an admin")
	}
	if status != "unemployed" || clubID != nil {
		t.Errorf("expected an unemployed manager, got status=%s club=%v", status, clubID)
	}

	// The new account can now log in via the resolution path.
	login, err := svc.Login(context.Background(), auth.LoginParams{Email: "newplayer@touchline.local", Password: "s3cret"})
	if err != nil {
		t.Fatalf("login as registered account: %v", err)
	}
	if login.Identity.UserID != res.UserID || login.Identity.WorldID != w {
		t.Errorf("login identity = %+v, want user %s in world %s", login.Identity, res.UserID, w)
	}
}

func TestRegister_NoPlayableWorld(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	res, err := svc.Register(context.Background(), auth.RegisterParams{
		Email: "lonely@example.com", Password: "s3cret",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Recorded decision: zero playable worlds -> account only, no join.
	if res.JoinedWorld != nil || res.ManagerID != nil {
		t.Errorf("no playable world must mean no join, got %+v", res)
	}
	if res.DisplayName != "lonely" {
		t.Errorf("default display name = %q, want the email local part", res.DisplayName)
	}
}

func TestRegister_SeveralPlayableWorlds(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	testdb.CreateWorld(t, pool, "W-1")
	testdb.CreateWorld(t, pool, "W-2")

	res, err := svc.Register(context.Background(), auth.RegisterParams{
		Email: "picky@example.com", Password: "s3cret",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// Recorded decision: several playable worlds -> the account must pick; an
	// admin joins it later. Never an arbitrary auto-join.
	if res.JoinedWorld != nil || res.ManagerID != nil {
		t.Errorf("several playable worlds must mean no auto-join, got %+v", res)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	pool := testdb.New(t)
	svc := auth.NewService(pool, testCfg())

	testdb.CreateUser(t, pool, "dupe@example.com", "s3cret", nil)

	if _, err := svc.Register(context.Background(), auth.RegisterParams{
		Email: "DUPE@example.com", Password: "other-pass",
	}); !errors.Is(err, auth.ErrEmailTaken) {
		t.Errorf("duplicate registration: got %v, want ErrEmailTaken", err)
	}
}
