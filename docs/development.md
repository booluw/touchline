# Touchline local development

This is the developer-environment contract (S01-05, OPD-14). It specifies the
required environment variables, how the Docker Compose stack boots, and how to
run the backend without Docker.

## Postgres is external — it is NOT a compose service

The backend runs on **PostgreSQL via Neon (serverless Postgres)** from day one.
The base `docker-compose.yml` deliberately defines no Postgres service; you
bring your own `DATABASE_URL`. Self-hosted Postgres is a later-phase option and
would swap in behind the same connection string.

To try the *whole stack* against a real database without Neon (e.g. in CI), use
the opt-in override:

```bash
docker compose -f docker-compose.yml -f infra/ci/docker-compose.postgres.yml up
```

This adds a `postgres` service; set `DATABASE_URL` to
`postgres://postgres:postgres@postgres:5432/touchline?sslmode=disable` first
(see the header of that file). The base compose file stays external on purpose.

## Environment variables

Compose reads a **repo-root `.env`** (copy `.env.example`). The backend
processes read the same vars from the environment; for bare `go run` use
`set -a; source .env; set +a`. The backend-only template is
`backend/.env.example` — keep the two templates in sync.

| Variable | Required | Example | Notes |
|---|---|---|---|
| `DATABASE_URL` | Yes | `postgres://user:pass@ep-….neon.tech/touchline?sslmode=require` | Postgres (Neon). Scheme must be `postgres://` — golang-migrate and river reject `postgresql://`. |
| `REDIS_URL` | Yes | `redis://localhost:6379` | WebSocket fan-out transport (S02-04, OPD-19). The API subscribes + publishes, the worker publishes `world_tick`. **Fail-soft:** if `REDIS_URL` is unset or unreachable, the API/worker log a warning and fall back to an in-process broker (bare `go run` works). |
| `JWT_SECRET` | Yes* | `change-me-in-production` | *compose falls back to a dev default; set a real secret outside dev. |
| `JWT_ACCESS_TTL` | No | `15m` | Access token lifetime. |
| `JWT_REFRESH_TTL` | No | `720h` | Refresh token lifetime. |
| `API_PORT` | No | `8080` | API listen port. |
| `SCHEDULER_PORT` | No | `8081` | Scheduler health listener. |
| `SCHEDULER_POLL_INTERVAL` | No | `15s` | How often the scheduler re-reads `world_config` cadences and reconciles its cron registry. |
| `WORKER_PORT` | No | `8082` | Worker health listener. |
| `ENV` | No | `development` | Environment tag. Any non-`development` value makes the API set `Secure` on auth cookies. |
| `APP_ORIGIN` | No | `http://localhost:3000` | Exact browser origin allowed by CORS and credentialed cookie requests. |
| `NUXT_PUBLIC_API_BASE` | No | `http://localhost:8080` | Baked into the frontend client bundle at build time (compose build arg). |

Secrets are never committed: `.gitignore` excludes `.env*` while keeping
`.env.example`.

### Neon gotchas

- Use the **unpooled** endpoint: remove `-pooler` from the host
  (`ep-….pooler.….neon.tech` → `ep-….….neon.tech`). The pooler resets
  `search_path` to `''` (schema resolution breaks) and rejects `search_path`
  startup parameters.
- Scheme must be `postgres://` for golang-migrate/river tooling.
- Strip surrounding quotes if you paste the string from a dashboard/shell.

## Docker Compose quick start

```bash
cp .env.example .env                      # then fill in DATABASE_URL (Neon)
docker compose up --build
```

Bootstrap happens automatically in order:

1. `migrations` — applies `backend/migrations` (golang-migrate up).
2. `ref-seed` — populates `ref.nationalities` + `ref.name_pool` from the
   curated `data/names` files (reference data only; idempotent).
3. `api` (8080), `scheduler` (8081), `worker` (8082) — wait for Postgres and
   the reference tables, then start and report healthy.
4. `frontend` (3000) — starts once the API is healthy.

Verify:

```bash
curl -s http://localhost:8080/health     # {"status":"ok"}
curl -s http://localhost:8080/health/db  # {"status":"db-ok"} (pings Postgres)
curl -s http://localhost:3000            # frontend index
docker compose ps                        # all services Up/healthy, one-shots exited 0
```

Tear down: `docker compose down`. Missing `DATABASE_URL`/`REDIS_URL` aborts
compose with an actionable message; a missing/unreachable `DATABASE_URL` makes
each backend binary fail fast at boot with the same guidance.

## Without Docker (bare metal)

```bash
cd backend
set -a; source ../.env; set +a   # or export DATABASE_URL yourself
go run ./cmd/api                  # :8080, GET /health and /health/db
go run ./cmd/scheduler            # :8081, GET /health
go run ./cmd/worker               # :8082, GET /health
```

Migrations via golang-migrate (`brew install golang-migrate`, or
`go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`):

```bash
cd backend
migrate -database "$DATABASE_URL" -path migrations up
```

Bare `go run` processes need `JWT_SECRET` set — the API fails fast without it
(compose supplies a dev default). The auth flow also wants `APP_ORIGIN` and
`ENV=development` (controls CORS + cookie `Secure`):

```bash
cd backend
set -a; source ../.env; set +a
go run ./cmd/api   # POST /api/auth/{login,refresh}, GET /api/dashboard
```

Create a dev account (world must already exist — see "Worlds and job offers"
below for the supported admin flow):

```bash
cd backend
go run ./cmd/user-create -email you@example.com -password 'hunter2' -world-id <uuid>
go run ./cmd/user-create -password-env ADMIN_PW -email admin@example.com -world-id <uuid> -admin
```

Reference data without compose:

```bash
cd backend
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names
```

### Integration tests

Integration tests (tag `integration`) live in `pkg/eventbus`, `internal/auth`,
`internal/world`, `internal/manager`, `internal/scheduler`,
`internal/bootstrap`, `internal/club`, and `cmd/api`, sharing a harness in
`internal/testdb` that migrates and truncates a real Postgres. Point them at
any reachable instance with `TEST_DATABASE_URL` (without it they fall back to
testcontainers, which requires Docker):

```bash
TEST_DATABASE_URL="$DATABASE_URL" go test -p 1 -tags integration -race \
  ./pkg/eventbus/... ./internal/auth/... ./internal/world/... \
  ./internal/manager/... ./internal/scheduler/... ./internal/bootstrap/... \
  ./internal/club/... ./cmd/api/...
```

`-p 1` serializes packages: each test truncates the shared database, so
concurrent packages would wipe each other's fixtures mid-test. The realtime
unit tests (`go test -race ./pkg/realtime/...`) need no external services —
they run against an in-process `miniredis`, and the `cmd/api` WebSocket
integration tests use the same in-process broker.

### Auth session flow

- `POST /api/auth/login` — verifies credentials, resolves the manager's world,
  and sets `access_token` (Path `/`) + `refresh_token` (Path `/api/auth`)
  httpOnly cookies. A jobless account spanning multiple worlds returns
  `{"status":"worlds","worlds":[...]}` with **no** cookies; re-post with
  `world_id` to pick.
- `POST /api/auth/refresh` — rotates the refresh-token session in
  `auth.sessions` (tokens stored hashed only) and sets a fresh cookie pair.
- `GET /api/dashboard` — protected demo route; returns the empty
  `{urgent,important,interesting}` shape until S07-01.

One account can hold one manager row per world (`uq_managers_user_world`) and
at most one *job* across all worlds (`uq_manager_one_job_per_user`); the JWT
carries no `world_id` — the world is derived from the manager row. See OPD-15.

### Worlds and job offers (S02-02)

Worlds and clubs are created by an admin at game start (OPD-16). All admin
routes require an `is_admin` account (create one with `user-create -admin`).
With the API running:

```bash
# Admin provisions a world, then launches it into 'active' (playable).
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' -d '{"email":"admin@example.com","password":"…"}'
curl -b /tmp/jar -X POST localhost:8080/api/admin/worlds \
  -H 'Content-Type: application/json' -d '{"name":"My World"}'
curl -b /tmp/jar -X POST localhost:8080/api/admin/worlds/<world-id>/status \
  -H 'Content-Type: application/json' -d '{"status":"active"}'
```

Versions of the lifecycle: `active`/`open_beta` are playable, `paused` sleeps,
`archived` is terminal. Launching seeds `world.world_config` cadence keys that
the S02-03 scheduler reads.

A manager's **first club must come from a job offer** — there is no
auto-assignment. While a world is still `provisioning`, an admin first runs the
S03-01 bootstrap, which turns the empty world into an AI starter club with a
policy-bot manager and a generated 24-player squad in one atomic operation:

```bash
# World clock resampling interval (default 15s)
curl -b /tmp/jar -X POST localhost:8080/api/admin/worlds/<world-id>/bootstrap \
  -H 'Content-Type: application/json' -d '{"name":"Harbour United","short_name":"HAR"}'
# -> 201 with club_id, manager_id, random_seed, and the full squad
```

Then an admin issues a job offer for the AI club, exactly as before:

```bash
curl -b /tmp/jar -X POST localhost:8080/api/admin/offers \
  -H 'Content-Type: application/json' \
  -d '{"club_id":"<club-id>","manager_id":"<unemployed-manager-id>"}'
```

The generated club and squad are readable through the authenticated API
(world-scoped to the caller's manager row, OPD-18(5)):

```bash
curl -b /tmp/jar localhost:8080/api/clubs
curl -b /tmp/jar localhost:8080/api/clubs/<club-id>
```

The candidate manager can then see, accept, decline, and resign:

```bash
curl -b /tmp/jar localhost:8080/api/managers/me/offers
curl -b /tmp/jar -X POST localhost:8080/api/offers/<offer-id>/accept
curl -b /tmp/jar -X POST localhost:8080/api/offers/<offer-id>/decline
curl -b /tmp/jar -X POST localhost:8080/api/managers/me/resign
```

Accepting hands the club over: the incumbent AI manager stands down,
`is_ai_controlled` flips FALSE, a `manager.manager_history` row opens; resign
or sack closes it (history is never deleted). See OPD-16.

### World clock (S02-03)

The scheduler binary is the world clock. It reads each **playable** world's
cadence values from `world.world_config` and fires world-scoped `WORLD_TICK`
events whose payload carries the tick `granularity`
(`hourly|daily|weekly|monthly|seasonal`). Cadences are re-read every
`SCHEDULER_POLL_INTERVAL`, so a runtime change takes effect without a redeploy.
The scheduler takes a Postgres advisory lock on boot — a second instance waits,
so only one scheduler can ever fire a cadence. Each emitted tick advances that
world's monotonic `current_tick`.

```bash
# World clock running (with the worker consuming WORLD_TICK log lines):
go run ./cmd/scheduler
go run ./cmd/worker
```

An admin tunes a cadence at runtime (no redeploy):

```bash
curl -b /tmp/jar -X POST localhost:8080/api/admin/worlds/<world-id>/config \
  -H 'Content-Type: application/json' \
  -d '{"key":"tick.daily_cadence","value":"30 0 * * *"}'
```

`tick.match_cadence` is seeded but deliberately ignored by the world clock:
live match ticks belong to the match engine's per-match goroutines (S04), not
to world-cron jobs (see OPD-17).

### Realtime (WebSocket + Redis, S02-04)

There is exactly **one** WebSocket: `GET /ws`, authenticated by the same
httpOnly `access_token` cookie as REST (the browser sends it automatically on
the handshake). The server checks the `Origin` against `APP_ORIGIN`, derives
the world from the caller's manager row (OPD-15), and scopes every delivered
event to that world — a socket can never receive another world's stream.

Event envelope (server→client and client→server):

```json
{ "type": "world_tick", "payload": { "event_id": "…", "granularity": "daily", "tick": 1 },
  "world_id": "…", "ts": "2026-09-11T02:03:04Z" }
```

| `type` (server→client) | Payload | Source |
|---|---|---|
| `world_tick` | `{event_id, granularity, tick}` | scheduler worker pushes every `WORLD_TICK` over Redis (S02-04 proof event) |
| `match_tick`, `notification`, `board_update`, … | feature-specific | future engines (S04+) — the socket forwards any type |

| `type` (client→server) | Response |
|---|---|
| `ping` | `pong` |
| anything else | `error` envelope `{code, message}` (connection stays open — AC5) |

Questions _to_ the server other than `ping` are not part of Phase 0; commands
stay REST (async-first, plan §1.5).

**Fan-out.** The API subscribes to a Redis pub/sub channel
(`touchline:realtime`); the worker publishes `world_tick` there. Every API pod
subscribes, so a browser connected to any pod sees every tick, and one pod can
serve a whole fleet with consistent delivery. If `REDIS_URL` is missing or
unreachable the processes log a warning and fall back to an in-process broker,
so bare `go run` works unchanged. See OPD-19.

**Client (Nuxt).** `composables/useSocket.ts` owns the single connection per
app. It reconnects with exponential backoff + jitter while enabled, dispatches
events to subscribers by `type`, and ignores unknown types (one warning). A
missed live push is harmless: authoritative state and replay live in
`world.events` (Postgres), never in the fire-and-forget Redis transport.
`stores/realtime.ts` owns the lifecycle and the latest `world_tick`/`error`;
`pages/auth/login.vue` connects on successful login.

**Verification** — the browser is required for the handshake, but the seam is
covered in CI/back-end tests (miniredis) and can be observed by hand:

```bash
# With Redis up and a launched world, set the daily cadence to every minute:
curl -b /tmp/jar -X POST localhost:8080/api/admin/worlds/<world-id>/config \
  -H 'Content-Type: application/json' \
  -d '{"key":"tick.daily_cadence","value":"* * * * *"}'
# Log in via the UI, then in browser devtools:
#   new WebSocket(`${location.origin.replace(/^http/,'ws')}:8080/ws`)  // or the app's socket
#   onmessage  -> a world_tick envelope arrives each minute
```
Kill Redis and restart it: the API logs the fallback warning, the browser
socket reconnects with backoff, and pushes resume once Redis returns (no
redeploy).

## CI

`.github/workflows/ci.yml` runs, among others, a **`compose`** job that
boots the full stack against the containerized-Postgres override and asserts
the API health checks and frontend respond — the contract is exercised on every
push/PR even though local Docker may be absent.