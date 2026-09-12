# S03-02 — Verify the Phase-0 vertical slice

**Status:** Done ✅  
**Sprint:** 03 — Seeded playable world bootstrap  
**Source:** Technical plan §§14, 16; OPENCODE.md  
**Depends on:** S03-01, S02-04

## What to do

Demonstrate the Phase-0 exit flow end to end: authenticate, create or select a world as approved, create a club/squad, and observe a daily tick. Establish automated verification around this critical path.

## Acceptance criteria

- A repeatable end-to-end test or scripted verification proves the complete Phase-0 exit criterion.
- The event log shows the tick and its world context; no client-side simulation is involved.
- Frontend and API failures surface understandable user/developer feedback.
- Backend build, unit tests, and vet pass; frontend typecheck/lint pass using the repository’s configured commands.
- Any blocked decision required for a public onboarding flow is escalated in `docs/product_manager.md`.

## Delivery evidence

- **AC1 — repeatable end-to-end proof:** `backend/cmd/api/phase0_slice_integration_test.go` (`TestPhase0VerticalSlice`) walks the entire exit path over real HTTP + the realtime seam, deterministically, in the CI integration run (`-p 1 -tags integration -race ./cmd/api/...`): admin login → `POST /api/admin/worlds` (provisioning) → bootstrap `Slice FC` (201, 24-player squad + policy-bot manager + seed) → candidate reads the club/squad via `GET /api/clubs{,/:id}` → launch → `tick.daily_cadence=* * * * *` → `scheduler.NewService.FireTick` (daily) → assertions below → failure-feedback assertions.
- **AC2 — tick context lives in the server's event log, no simulation:** after FireTick the test asserts `world.events` holds exactly one `WORLD_TICK` for the booted `world_id` with payload `granularity=daily`, `world_tick=1`, and `world.worlds.current_tick` advanced to 1. The `/ws` envelope a cookie-authenticated browser stand-in receives is built from that persisted row via `realtime.BuildWorldTick` — the same helper `cmd/worker` uses after consuming `WORLD_TICK` off the bus — and its `event_id`/`tick`/`world_id` are asserted to match, proving the browser data is server-sourced.
- **AC3 — understandable failure feedback:** asserted on the API (401 wrong password with a readable error; authenticated-without-manager-row → 403 `"no world context"`) and surfaced in the UI: `frontend/components/PhaseZeroStatus.vue` renders a live connection badge (Connected/Reconnecting/Offline), the latest `world_tick` (granularity, tick #, time), the bootstrapped club/squad, a clear empty-state ("an AI club will offer you the job"), and readable 401 (→ Sign in), 403/`no world context`, 5xx, and network-error states with a Retry control. `pages/index.vue` mounts it; login world-picker now lands on `/` after picking.
- **AC4 — repository-configured checks green:** `go build ./...`, `go vet ./...`, `go test -race ./...`, and the full integration suite pass; frontend `pnpm run lint` and `pnpm run typecheck` pass. `npm run lint` previously errored with "no configuration file" — S03-02 adds a minimal flat ESLint config (`frontend/eslint.config.mjs`: JS + TS + Vue essential, `no-undef` off for Nuxt auto-imports) and dev deps. This task also **completes the frontend pnpm migration**: `packageManager` pinned in `package.json`, CI `frontend-lint` job switched to `pnpm/action-setup` + `pnpm install --frozen-lockfile` + `pnpm run lint`/`typecheck` (it was broken before — `cache: npm` referenced the never-committed `package-lock.json`), `Dockerfile.frontend` builds via `corepack enable && pnpm install --frozen-lockfile`, and `docs/development.md`/`OPENCODE.md` document pnpm.
- **AC5 — onboarding blocker escalated:** `OPD-02` (Registration & Account Policy) in `docs/product_manager.md` is marked **ESCALATED (S03-02)**: the slice is verified only via admin bootstrap + dev-only `cmd/user-create` (OPD-15(6)); a public, self-service onboarding flow (registration, age/privacy consent, password reset) is blocked on it.
