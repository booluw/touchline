# IM13 — Server-side logout + one session per user account

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (auth / session hygiene)
**Source:** Product decision (manual session)
**Depends on:** `0001_auth` (the `auth.sessions` schema), OPD-15 (the session
contract: rotating refresh sessions stored hashed). No other in-flight
improvement touches `auth.sessions`.

## Delivery evidence

- `backend/migrations/0055_auth_one_session_per_user.up.sql` — collapses any
  duplicate live sessions (keeps the newest per user by `(created_at, id)`),
  then `CREATE UNIQUE INDEX uq_sessions_one_live_per_user
  ON auth.sessions(user_id) WHERE revoked_at IS NULL` — a DB-level guarantee
  that one account holds at most one *live* refresh session, whatever the code
  path. `.down.sql` drops the index; `migrations/README.md` row added.
- `internal/auth/auth.go` —
  - `Service.Logout(ctx, rawRefresh string) error` — deletes the session row by
    the hashed refresh token; **idempotent by design** (empty/unknown/already-
    consumed token matches no row and returns nil).
  - `completeLogin` — inside the existing login transaction, deletes any prior
    live sessions for the account (`DELETE ... WHERE user_id = $1`) **before**
    `insertSession`, so login invalidates and deletes the old session and the
    partial unique index is never violated. Applies to admin console sessions
    and manager sessions alike; the world-picker path mints no session and
    needs no cleanup.
- `internal/httpapi/handlers.go` — `handleLogout`: reads the `refresh_token`
  cookie, calls `Logout`, then `clearAuthCookies`; responds `200 {"status":"ok"}`.
  Deliberately **unauthenticated** (logout must work once the access token has
  already expired, and must be a no-op when nothing is presented).
  `clearAuthCookies` mirrors `setAuthCookies`: both cookie names cleared with
  `MaxAge: -1`, `Expires: past`, and the same `HttpOnly`/`SameSite`/`Secure`
  flags and `Path` values (`/` and `/api/auth`).
- `internal/httpapi/router.go` — `api.POST("/auth/logout", s.handleLogout)`
  after `/auth/refresh`.
- `internal/apidocs/openapi.yaml` — `/api/auth/logout` (operationId `logout`,
  `security: []`, `200` + `500`), preserving the `TestDocsCoverRouter` /
  `TestDocsOpenAPIValid` gates.
- Tests (integration, compile-gated locally — live Postgres required):
  - `internal/auth/auth_integration_test.go` — `TestLogin_ReplacesPriorSession`
    (one live row after a second login; the replaced refresh token returns
    `ErrInvalidRefresh`; a raw second live insert fails `23505`, proving the
    index), `TestLogout_DeletesServingSession` (row deleted, `refresh` after
    logout → `ErrInvalidRefresh`, repeat/unknown/empty logout all no-op).
  - `cmd/api/api_integration_test.go` — `TestHTTPLogout_RevokesAndClearsCookies`
    (200, `status: ok`, both cookies cleared with correct paths, session row
    count 0, refresh with the logged-out cookie → 401),
    `TestHTTPLogout_NoSessionIdempotent` (logout with no cookies / garbage
    cookie → 200 + both cookies cleared).
- Docs: `docs/product_manager.md` OPD-39, `docs/development.md` "Auth session
  flow".
- Verify: `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` all
  green (includes the openapi gating tests); touched files `gofmt`-clean.

## Recorded decisions

- **Old sessions are hard-deleted, not tombstoned.** "Invalidated and deleted":
  login and logout `DELETE` the affected rows. Refresh rotation keeps its
  existing soft-revoke (`revoked_at`) behaviour for audit, but login/logout
  remove the row outright — the one-session invariant is enforced by a partial
  unique index over live rows only, so tombstones never block a fresh session.
- **Access tokens stay stateless — a documented grace window.** `requireAuth`
  performs no DB lookup; an already-issued access JWT keeps authenticating
  until its own TTL (default 15m) after logout or being kicked by a new login.
  The session row is gone, so the refresh dies instantly: the kicked device can
  never mint a new pair. This matches the existing rotation behaviour (OPD-15(5))
  and avoids a per-request DB hit plus rework of the token-minting test helpers.
- **Logout is unauthenticated and idempotent.** It keys solely on the
  `refresh_token` cookie, works when the access token has already expired, and
  always returns 200 with both cookies cleared. Nothing special to distinguish —
  a missing or already-used token simply matches no row.
- **The invariant is enforced in the database.** Code alone (delete-before-
  insert in `completeLogin`) would suffice today, but the partial unique index
  guarantees it against future write paths and concurrent logins.
- **Frontend is out of scope.** The server clears the HttpOnly cookies itself,
  so the future UI step is just calling `POST /api/auth/logout` and resetting
  the auth store; no JS can read the tokens anyway.