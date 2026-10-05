# AGENTS.md

Repo-specific guidance for AI coding agents. Keep high-signal only; this file is read on every session.

## Layout
- Monorepo: `backend/` (Go), `frontend/` (Nuxt 3, pnpm), `docs/` (product/how-to docs), `infra/`, `data/`.
- App code lives in `backend/internal/<pkg>`, not `backend/docs/` (docs live at repo-root `docs/`).
- One backend binary: `backend/cmd/touchline` (`serve` | `api` | `scheduler` | `worker`). Wiring lives in `internal/app` (config.go, app.go, runners.go, worker.go, worldtick.go); HTTP handlers + their integration tests live in `internal/httpapi` (`<domain>_handlers.go`). JWT helpers are `pkg/jwt`; `internal/auth` is the login/session service.
- DB migrations: `backend/migrations/` as numbered `NNNN_name.{up,down}.sql`; add a row to the `migrations/README.md` matrix with each change. Never renumber existing files.
- API schema: `backend/internal/apidocs/openapi.yaml`. Two Go tests in `backend/internal/httpapi` (`TestDocsCoverRouter`, `TestDocsOpenAPIValid`) gate it: any new/renamed route must be mirrored in openapi.yaml or `go test ./...` fails.
- Engine invariants are documented in `docs/how-to/` (cups, seasons, cadences) and `docs/product_manager.md` (recorded decisions).
- **The Touchline Book** (`docs/touchline-book/the-touchline-book.md`) is the consolidated, chapter-per-concept reference for every system; start there and keep its chapters in sync when behaviour changes.

## Verification (run in this order from `backend/`)
- `gofmt -w` on every touched Go file
- `go build ./...`
- `go vet ./...`
- `go vet -tags integration ./internal/... ./pkg/...` (compile-gate for integration tests)
- `go test ./...`

Integration tests (`//go:build integration`) need a live Postgres (`TEST_DATABASE_URL`, `make db-up`/Docker). Without one they only compile-check; when a Postgres is available (even a local `postgresql-16` cluster) run the suites you touched — IM30 found runtime SQL bugs that `go vet -tags integration` cannot see. Never drop the `integration` build tag.

## Improvement workflow (the ongoing sprint)
- Each improvement is planned in `docs/tasks/improvements/IM##-<slug>.md` from a fixed template (Status / Sprint / Source / Depends / What to do / Recorded decisions).
- When implemented: set `**Status:** Implemented`, add a `## Delivery evidence` section (files + test/verify results), mirror user-visible behavior into `docs/how-to/*.md` and `docs/product_manager.md`, and STOP (no commit unless asked).
- `qualify.go` (IM07) is the cup-qualification engine: pure (no writes), last-completed-season only, `Service.ComputeCupField(ctx, cupID)` returns `Field{Entrants, Conflicts, Unavailable, Cup, ClubCount}`. Unavailable leagues are returned as a list, never an error.

## Frontend
- `frontend/` is a pnpm workspace; `pnpm install`, `pnpm dev`. Nuxt source under `frontend/app/`, `~/` alias, `.vue` SFCs.
- API composables in `frontend/app/composables/` (e.g. `useCompetition.ts` holds Cup/CupCampaign types).
- Frontend work is out of scope unless a task explicitly includes it.