# S01-01 — Create core schemas and world-scoped migrations

**Status:** Done  
**Owner:** opencode agent  
**Sprint:** 01 — World foundation and event spine  
**Source:** OPENCODE.md; technical plan §§3, 6, 15, 18  
**Depends on:** S00-01

## What to do

Add versioned `golang-migrate` migrations per engine schema. Establish the minimum Phase-0 `world`, `club`, `player`, and `manager` persistence model, including `world.events`, `world.world_config`, `world.nationality_pool`, and `world.news_stories`. Configure migrations for Helm pre-install/pre-upgrade execution.

## Acceptance criteria

- Migrations create the `club`, `player`, `manager`, `transfer`, `finance`, `social`, `match`, `competition`, and `world` schemas.
- Every world-bound entity introduced in the migration set carries `world_id`; scheduler and event records can be queried per world.
- `world.events` persists event identity, type, occurrence time, monotonic world tick, optional actor and causal IDs, and typed JSON payload.
- `world.world_config` can store runtime tick cadences and feature flags; `world.nationality_pool` supports weighted nationality records.
- `finance` has no stored mutable balance column; schema constraints preserve append-only ledger intent.
- Migrations run cleanly from an empty Neon-compatible Postgres database and are repeatable through the documented migration workflow.

## Delivery evidence

- **Migration set** (`backend/migrations/0000–0014`, golang-migrate): creates all nine required schemas plus `auth`, `ref`, `person`, `notification`, `moderation`; every world-scoped entity carries `world_id`; `world.events` persists identity, type, occurrence time, monotonic `world_tick`, optional actor/causal IDs, typed JSON payload (plus `explanation`/`random_seed`); `world.world_config` stores runtime tick cadences/feature flags; `finance` is ledger-only with no stored balance column. Workflow documented in `backend/migrations/README.md`.
- **Repeatability verified against a live Postgres (Neon, DATABASE_URL from `backend/.env`, never printed):** `migrate up` (0→14) → `migrate down -all` (0, clean) → `migrate up` (0→14) → `version` = 14, no errors.
- **Helm pre-install/pre-upgrade hook:** new `infra/helm/migrations` chart — `batch/v1` Job with `helm.sh/hook: pre-install,pre-upgrade`, `hook-weight: 0`, `hook-delete-policy: before-hook-creation,hook-succeeded`, running official `migrate/migrate:v4.18.1` with `DATABASE_URL` from the `touchline-db` secret; SQL mounted from a ConfigMap populated via `files/` symlink → `backend/migrations` (single source of truth). `helm lint` passes; `helm template` renders all 15 up-migrations.
- **Dev repeatability:** one-shot `migrations` service added to `docker-compose.yml`; `api`, `scheduler`, `worker` gate on `service_completed_successfully`. Validated with `docker compose config`.
- **CI gate:** new `migrations` job in `.github/workflows/ci.yml` runs up → down -all → re-up (with version check) against a `postgres:16` service container on every push/PR.
- **Open decision:** resolved decision OPD-11 recorded in `docs/product_manager.md` — weighted nationality pool lives in `ref.nationalities.generation_weight` + `ref.name_pool` (global), superseding the `world.nationality_pool` wording; `OPENCODE.md` Phase-0 backlog item updated to match.
