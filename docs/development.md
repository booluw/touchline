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
| `REDIS_URL` | Yes | `redis://localhost:6379` | Consumed by the stack; wired into apps in S02-04. |
| `JWT_SECRET` | Yes* | `change-me-in-production` | *compose falls back to a dev default; set a real secret outside dev. |
| `JWT_ACCESS_TTL` | No | `15m` | Access token lifetime. |
| `JWT_REFRESH_TTL` | No | `720h` | Refresh token lifetime. |
| `API_PORT` | No | `8080` | API listen port. |
| `SCHEDULER_PORT` | No | `8081` | Scheduler health listener. |
| `WORKER_PORT` | No | `8082` | Worker health listener. |
| `ENV` | No | `development` | Environment tag. |
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

Reference data without compose:

```bash
cd backend
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names
```

### Integration tests

`pkg/eventbus` integration tests (tag `integration`) need a real Postgres.
Point them at any reachable instance with `TEST_DATABASE_URL`
(migrations are applied automatically by the test); without it they fall back
to testcontainers, which requires Docker:

```bash
TEST_DATABASE_URL="$DATABASE_URL" go test -tags integration ./pkg/eventbus/...
```

## CI

`.github/workflows/ci.yml` runs, among others, a **`compose`** job that
boots the full stack against the containerized-Postgres override and asserts
the API health checks and frontend respond — the contract is exercised on every
push/PR even though local Docker may be absent.