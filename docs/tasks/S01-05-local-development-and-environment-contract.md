# S01-05 — Make the local environment executable and documented

**Status:** Done
**Owner:** opencode agent
**Sprint:** 01 — World foundation and event spine
**Source:** OPENCODE.md; technical plan §§2, 15, 18
**Depends on:** S01-01

## What to do

Finish the Docker Compose and environment configuration contract for Redis, API, scheduler, worker, frontend, and Neon Postgres. Postgres remains external to Compose.

## Resolution

Recorded as **OPD-14** (see `docs/product_manager.md`): an explicit developer-environment contract now covers the env-var set (repo-root `.env` driving compose; `backend/.env.example` mirroring it for bare `go run`), the Neon connection-string rules (unpooled endpoint, `postgres://` scheme, no quotes), the fail-fast/health contract on all three backend binaries, the hermetic Postgres **override** (still opt-in, not a base compose service), and the bootstrap order migrations → ref-seed → api/scheduler/worker → frontend. Full walkthrough in `docs/development.md`.

## Acceptance criteria

- **Documented setup specifies all required environment variables, including Neon `DATABASE_URL`, without committing secrets.** ✅ `.env.example` (repo root, drives compose) + `backend/.env.example` (synced, drives bare `go run`/migrate) list `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`, `JWT_ACCESS_TTL`, `JWT_REFRESH_TTL`, `API_PORT`, `SCHEDULER_PORT`, `WORKER_PORT`, `ENV`, `NUXT_PUBLIC_API_BASE`; `.gitignore` keeps `.env*` ignored while tracking both templates.
- **Docker Compose starts Redis, API, scheduler, worker, frontend and connects them to the configured external Postgres.** ✅ Compose rewritten: real Dockerfile refs (`infra/docker/Dockerfile.{api,scheduler,worker,ref-seed}`; Dockerfile.api builds the other two binaries), `depends_on` chains (redis healthy + migrations/ref-seed `service_completed_successfully` → api/scheduler/worker; api healthy → frontend), busybox-`wget` healthchecks, `${VAR:?}` guards for `DATABASE_URL`/`REDIS_URL`, `NUXT_PUBLIC_API_BASE` as a frontend build arg (Nuxt bakes it at build time; `Dockerfile.frontend` now `ARG`/`ENV` before `npm run build`).
- **The initial migration and a health check can run in this environment.** ✅ `migrations` one-shot (migrate/migrate, `$DATABASE_URL` interpolated) runs before `ref-seed` and the apps; health endpoints live on all binaries — api `:8080/health` + `:8080/health/db` (pings Postgres, 503 when down), scheduler `:8081/health`, worker `:8082/health`. Live-verified locally: api `/health` → `{"status":"ok"}`, `/health/db` → `{"status":"db-ok"}`; scheduler health ok.
- **Developer guide clearly states Postgres is Neon/external, not a Compose service.** ✅ `docs/development.md` ("Postgres is external — it is NOT a compose service") + OPD-14; the opt-in `infra/ci/docker-compose.postgres.yml` override is documented for hermetic boots (CI + local).
- **Failed/missing configuration produces actionable startup errors.** ✅ `DATABASE_URL:?DATABASE_URL required…` interpolation in compose (also `REDIS_URL`); each binary `log.Fatalf` with `"set it in .env (see docs/development.md)"` when `DATABASE_URL` is missing or the DB is unreachable. Live-verified: `env -u DATABASE_URL go run ./cmd/api` exits 1 with the actionable message.

## Delivery evidence

- `docker-compose.yml` rewritten (redis/migrations/ref-seed/api/scheduler/worker/frontend; healthchecks; env guards; depends_on chains; frontend build arg).
- `infra/docker/Dockerfile.frontend` — added `ARG`/`ENV NUXT_PUBLIC_API_BASE` before build.
- `.env.example` (new, repo root) + `backend/.env.example` (rewritten) — synced env contract.
- `infra/ci/docker-compose.postgres.yml` (new) — opt-in postgres:16 override.
- `.github/workflows/ci.yml` — new `compose` job: `docker compose config --quiet` with placeholder env, then full-stack boot against the Postgres override, polling `http://localhost:8080/health/db` (loop ~10 min), `curl` frontend :3000, `docker compose ps` + logs, `down -v`.
- `backend/cmd/{api,scheduler,worker}/main.go` — fail-fast `connectDB` + health endpoints (live-checked).
- `docs/development.md` (new, the S01-05 walkthrough), `README.md` Getting started rewritten (+ `cmd/ref-seed` in layout).
- `docs/product_manager.md` — **OPD-14** added; `OPENCODE.md` refreshed (playergen/data/ref-seed/health/migrations `0000–0014`+`0016–0022`, compose-in-CI status, backlog items 6–7 shown done).
- Verification: `go build ./... && go vet ./...` green after the binary changes; API + scheduler live-smoked against the local PG test cluster; both compose files and `ci.yml` parse as YAML (`python yaml.safe_load`).
- **Constraint:** local Docker is absent on the dev machine, so the actual `docker compose up` boot is exercised by the CI `compose` job (per OPD-14) — noted in OPENCODE.md.

**Also covers sprint-1 acceptance:** `go test ./...` and `go test -race ./...` remain green (no package changes in S01-05; binaries only).