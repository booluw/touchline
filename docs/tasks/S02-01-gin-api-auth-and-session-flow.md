# S02-01 — Deliver authenticated API and session flow

**Status:** Done
**Owner:** opencode agent
**Sprint:** 02 — Authenticated, schedulable worlds
**Source:** OPENCODE.md; technical plan §§2, 12, 16  
**Depends on:** S01-01, S01-05

## What to do

Wire the Go API as a Gin REST gateway and complete the approved custom JWT access/refresh cookie flow between Nuxt and Go. Keep game logic out of Nuxt server routes.

## Acceptance criteria

- Gin serves health and the documented `POST /api/auth/login` and `POST /api/auth/refresh` endpoints.
- Login and refresh use the existing custom JWT package and httpOnly cookie transport as specified.
- Protected routes reject unauthenticated requests; request validation and authorization occur server-side.
- The Nuxt client can establish and refresh an authenticated session against the Go API.
- The required product policies that are not specified (registration, recovery, verification) are recorded as open decisions rather than guessed.

## Delivery evidence

- **AC1 — endpoints.** `backend/cmd/api/router.go`: Gin router (Logger/Recovery + `gin-contrib/cors` with `AllowOrigins: [APP_ORIGIN]`, `AllowCredentials: true`) serves `GET /health` + `/health/db` (OPD-14 contract, untouched) and the `/api` group with `POST /api/auth/login`, `POST /api/auth/refresh`, and protected `GET /api/dashboard`. Live smoke (local Postgres, `curl`): login `200` sets cookies, dashboard `200`, refresh `200`, replay of the pre-rotation refresh cookie `401`, bad credentials `401`, unauthenticated dashboard `401`.
- **AC2 — JWT + cookie transport.** `backend/pkg/auth/jwt.go` (reworked, unit tests `go test ./pkg/auth/...` ✓): `GenerateTokenPair` issues access (`sub`, `user_id`, `jti`, `exp`, `iat`, `type=access`) and refresh (`sub`, `jti`, `exp`, `iat`, `type=refresh`) tokens; `jti` guarantees per-token uniqueness so session rotation can never collide on `auth.sessions.refresh_token_hash`. `ValidateAccessToken`/`ValidateRefreshToken` pin HS256 (no algorithm-confusion; tests cover `none` alg + wrong secret + type swap). `backend/cmd/api/handlers.go` writes `access_token` (Path `/`) + `refresh_token` (Path `/api/auth`) cookies — `HttpOnly`, `SameSite=Lax`, `Secure` only when `ENV != development`. `internal/auth` stores only `HashRefreshToken` hashes (SHA-256 hex), never raw tokens.
- **AC3 — server-side protection + validation.** `backend/cmd/api/middleware.go` `requireAuth` rejects with `401` when the access cookie is missing/expired/invalid; login validates body (JSON, `email` contains `@`, non-empty password → `400`), maps business errors `401/403/500`. World-scope authorization is server-side: a `world_id` pick for a world the account has no manager row in returns `403` (`ErrNotMemberOfWorld`). Integration (`go test -p 1 -tags integration -race ./pkg/eventbus/... ./internal/auth/... ./cmd/api/...`, `TEST_DATABASE_URL` against Postgres) covers HTTP login/dashboard/refresh incl. rotation + old-cookie rejection (`backend/cmd/api/api_integration_test.go`), service ladder + rotation + unique-index world scoping (`backend/internal/auth/auth_integration_test.go`).
- **AC4 — Nuxt client session.** `frontend/composables/useAuth.ts` (new): `login` (posts `credentials:'include'`, surfaces the world-picker response), `refresh` (POST to `/api/auth/refresh` on `401`), `authedFetch` (transparent single refresh-on-401 retry); `frontend/pages/auth/login.vue` renders the world picker when `{"status":"worlds",…}` and navigates on success; `useDashboard.ts` reads `/api/dashboard` through `authedFetch`. `vue-tsc --noEmit` typecheck ✓ (baseline `stores/club.ts` `useFetch` generic fixed in passing).
- **AC5 — open decisions recorded, not guessed.** `OPD-15` in `docs/product_manager.md` resolves the account/world/job/session model and explicitly keeps **registration, login recovery, and email verification OPEN under OPD-02** (they remain in the Open Decisions Registry, impacted sprints updated); `cmd/user-create` is explicitly a dev bootstrap (not the product signup) and prints the OPD-02 note. Walkthrough/documentation in `docs/development.md` (auth endpoints, `cmd/user-create` usage, integration-test command); CI integration job extended to the auth+api packages with `-p 1` (shared-DB truncation serialization).

## Related decisions (OPD-15 highlights)

One account → many worlds (one `manager.managers` row per user per world); at most one job per user platform-wide; no `world_id` in JWTs (world derived from the manager row); sessions rotate + revoke against `auth.sessions`; registration/recovery/verification open (OPD-02). Rate limiting deferred (S02-04/S11); no auth events this sprint; CSRF policy open (httpOnly + SameSite=Lax covers dev).