# Chapter 29 — Engineering workflow: environment, verification and the improvement process

## 29.1 Environment

| Variable | Required | Notes |
| --- | --- | --- |
| `DATABASE_URL` | yes | `postgres://` scheme (golang-migrate and River reject `postgresql://`); for Neon use the **unpooled** host (strip `-pooler`) |
| `JWT_SECRET` | yes (bare metal fails fast) | compose supplies a dev default |
| `REDIS_URL` | no | fail-soft → in-process broker |
| `APP_ORIGIN` | browser flow | exact CORS / WS origin |
| `ENV` | — | anything but `development` sets `Secure` cookies |
| `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL` | no | `15m` / `720h` |
| `API_PORT` / `SCHEDULER_PORT` / `WORKER_PORT` | no | 8080 / 8081 / 8082 |
| `SCHEDULER_POLL_INTERVAL` | no | `15s` |
| `NUXT_PUBLIC_API_BASE` | no | baked into the frontend build |

Templates: repo-root `.env.example` (compose) and `backend/.env.example` —
keep them in sync. Postgres is **external** by default (Neon); a hermetic
Postgres is the opt-in `infra/ci/docker-compose.postgres.yml` override.

### Running

```bash
# compose
cp .env.example .env && docker compose up --build    # migrations → ref-seed → touchline → frontend

# bare metal
cd backend && set -a; source ../.env; set +a
migrate -database "$DATABASE_URL" -path migrations up
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names -clubdata data/clubs
go run ./cmd/touchline                 # serve: api + scheduler + worker
cd ../frontend && pnpm install && pnpm dev
```

API reference while running: `/api/openapi.yaml`, interactive `/api/docs`.

## 29.2 Verification — run in this order from `backend/`

1. `gofmt -w` on every touched Go file
2. `go build ./...`
3. `go vet ./...`
4. `go vet -tags integration ./internal/... ./pkg/...` (compile-gate for
   integration tests)
5. `go test ./...`

### Gates you will hit

- **OpenAPI parity** — `TestDocsCoverRouter` and `TestDocsOpenAPIValid` in
  `internal/httpapi`: every new or renamed route must be mirrored in
  `backend/internal/apidocs/openapi.yaml`.
- **Golden match replay** — `pkg/matchsim/simulate_test.go` pins seed 424242;
  engine changes bump `EngineVersion` and re-pin ([Ch. 15](15-match-engine.md)).
- **Explanation sums** — board, wages, finance summary and transfer value
  explanations are tested to sum exactly ([Ch. 3](03-events-and-explanations.md)).

### Integration tests

`//go:build integration` tests need a live Postgres (`TEST_DATABASE_URL`, or
testcontainers with Docker; `make db-up`). Run packages serially (`-p 1`) —
each test truncates the shared database:

```bash
TEST_DATABASE_URL="$DATABASE_URL" go test -p 1 -tags integration -race ./internal/<pkg>/...
```

Without a database they only compile-check, so **run the suites you touched**
when any Postgres is available — IM30 found runtime SQL bugs that `go vet
-tags integration` cannot see. Never drop the `integration` build tag. Never
point tests at a database you care about (the harness truncates it).

## 29.3 Migrations

- `backend/migrations/NNNN_name.{up,down}.sql`, applied with golang-migrate.
- **Never renumber** existing files; add a row to `migrations/README.md` for
  every change.
- Prefer partial unique indexes and `ON CONFLICT` arbiters for invariants (the
  codebase leans on them for idempotency — [Ch. 3](03-events-and-explanations.md)).

## 29.4 Patterns to reuse (don't reinvent)

| Need | Use |
| --- | --- |
| emit an event | `bus.PublishTx(ctx, tx, ev)` inside the business transaction |
| explain a score | `explanation.New(subject, score).Add(label, delta)` |
| a "now" date | `world.world_date(world_id)` / `club_world_date(club_id)`, never `now()` |
| a player's overall | `squad.PositionalOverall` |
| randomness | derive a new seeded stream; never touch matchsim's canonical stream |
| tunable number | a named constant in the owning package + its numerics doc |
| AI/absent action | call the shared `…ForClub` core with a policy-bot `Actor` |
| idempotent money | dedup-keyed ledger insert |

## 29.5 The improvement workflow (current sprint)

1. Plan in `docs/tasks/improvements/IM##-<slug>.md` from the fixed template:
   **Status / Sprint / Source / Depends / What to do / Recorded decisions**.
2. Implement; run the verification sequence.
3. On completion: set `**Status:** Implemented`; add `## Delivery evidence`
   (files + test results); mirror user-visible behaviour into the relevant
   chapter of **this book** (and the how-to/numerics doc if one exists) and
   into `docs/product_manager.md` as an OPD entry when a product decision was
   made.
4. **Stop** — no commit unless asked.

Frontend work (`frontend/`, Nuxt 3 + pnpm, composables in
`frontend/app/composables/`) is out of scope unless a task explicitly includes
it.

## 29.6 Keeping this book true

- Change a number → update the constant, the chapter's table, and the numerics
  ledger together.
- Add a system → add a chapter, link it from the contents page and from every
  chapter it connects to, and add it to [Appendix A](appendix-a-source-index.md).
- Supersede a behaviour → fix the chapter; don't leave the old text "for
  history" (history lives in git and the task files).

## Connections

- Source: `AGENTS.md`, `docs/development.md`, `docs/how-to/setup-and-launch.md`, `docs/tasks/README.md`, OPD-14.

---
[← Admin console](28-admin-console.md) · [Contents](the-touchline-book.md) · [Next: Roadmap →](30-roadmap-and-open-decisions.md)
