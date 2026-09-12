# OPENCODE — Touchline handoff notes

Working notes for an LLM agent (or human) joining this codebase. Read this first, then the two documents in `docs/`.

---

## What this project is

Touchline is a **browser-based, persistent multiplayer football management universe**. Thousands of managers coexist in the same continuously-running world (no synchronous multiplayer required). Managers build squads, negotiate transfers with each other, manage finances, get hired/fired, and eventually shape the football world itself.

- **PRD**: `docs/Touchline — Persistent Multiplayer Football Manager PRD.md` (long-term product vision; 85 sections)
- **Technical plan**: `docs/Touchline_Technical_Implementation_Plan.md` (buildable architecture; phased 0→4)

Both are the source of truth. Do not invent systems absent from these docs.

---

## Key technical decisions (resolved)

| Topic | Decision |
|---|---|
| Backend language | **Go** |
| HTTP framework | **Gin** (user choice over the plan's Chi/Echo) |
| API style | REST/JSON + WebSockets (`/ws`, single multiplexed socket) |
| Database | **PostgreSQL** (Neon serverless from day one; self-hosted later) |
| Schemas | One Postgres schema per engine: `club, player, manager, transfer, finance, social, match, competition, world` |
| Cache / ephemeral | Redis (sessions, rate limit, live-match state, pub/sub WS fan-out) |
| Event bus / job queue | **river** (Postgres-native) as Phase-0 event bus. NATS JetStream documented as a swap-in later — the `pkg/eventbus.EventBus` interface is the stable seam. |
| Auth | **Custom JWT** (access + refresh, httpOnly cookies). No Ory/Clerk. |
| Migrations | **golang-migrate**, versioned per schema, run as Helm pre-install/pre-upgrade hook |
| Frontend | Nuxt 3 (Vue 3, Composition API), Pinia, Tailwind, `@vite-pwa/nuxt` |
| Realtime client | One WebSocket, wrapped in `useSocket()` composable, fanned out to Pinia stores |
| Dev infra | Docker Compose (migrations + ref-seed + Redis + api + scheduler + worker + frontend). **Postgres is NOT in compose — it's Neon** (opt-in Postgres override in `infra/ci/docker-compose.postgres.yml`, OPD-14). |
| Prod infra | k3s on Contabo VPS(s); Helm charts; cloud-agnostic (works on EKS/GKE/AKS unchanged) |
| CI/CD | GitHub Actions → lint/test → build multi-arch images → GHCR; Argo CD deploys |
| Contract/UI | text/event-feed match viewer (live commentary feed style); 2D pitch deferred |

## Architectural principles (from plan §1, non-negotiable)

1. **Server is the only source of truth.** Client renders + submits commands; never simulates outcomes.
2. **Modular monolith first.** Six engine packages in one binary, hard boundaries, separate DB schemas, communicating via the event-bus interface. Extract to services later without rewrite.
3. **Everything is an event.** Event log = backbone of persistence, notifications, news, history, audit.
4. **Determinism** where money/competition is at stake — seeded, replayable match simulation.
5. **Async-first UX.** Commands are queued/validated against deadlines, not real-time turns.
6. **PWA, not native.**

## Domain-model invariants (critical)

- **Ledger, never a bare balance.** `finance.ledger_entries` is append-only; balance is always `SUM(entries)`. Do not add a stored `balance` column.
- **Relationships = graph table** (`social.relationships`, pair + type unique), NOT columns on players. Uses `entity_{a,b}_type` polymorphism ('player' | 'manager' | 'club').
- **Board mandates = structured rows** (category: primary/secondary/strategic/financial + status), not free text. Job-security math queries them directly.
- **Explanation objects** (`internal/world/explanation.go`) accompany every scored decision (board confidence, transfer desire, job security…). UI and news generator render "why" from these; never recompute.
- **Explainability**: every endpoint that changes state returns the `Explanation` where relevant.
- **PolicyBot = another ActorID.** Absence-mode delegation and AI clubs are one system: human + PolicyBot both call the same command handlers.

## Which world/manager model (plan §18 resolutions)

- **Multiple parallel worlds from day one.** Every schema carries `world_id` from first migration. Scheduler is world-scoped.
- **One account, many worlds.** Membership is one `manager.managers` row per (user, world); a user holds at most **one job platform-wide** (`uq_manager_one_job_per_user`), and at most one row per (user, world) (`uq_managers_user_world`, migration `0023`). JWT claims carry no `world_id` — the active world is derived from the manager row (login ladder: job world wins → explicit pick → single active world → world picker → single row → list). Full contract: OPD-15.
- **World lifecycle = admin-only (S02-02, OPD-16).** Admin provisions a world (`provisioning`), launches it (`active`/`open_beta` = playable), pauses/resumes, archives (terminal). `world.world_config` cadence keys are seeded on launch (`internal/world.defaultConfigKeys`); S02-03 reads them.
- **A first club = a job offer from an AI club — the only sanctioned path (S02-02, OPD-16).** Admin (on an AI club's behalf) creates `manager.job_offers`; the unemployed human manager accepts or declines. Accepting hands the club over (incumbent AI manager stands down, `is_ai_controlled`→FALSE, `manager.manager_history` opens). Resign/sack close the history row (never deleted). One active assignment platform-wide; historical/display rep reads are global, operational rep is world-scoped (`WorldReputationTotal`). League composition is deliberately NOT decided here (OPD-01 → S03-01).
- Match viewing = event-feed (see above). Email notifications (SES/Postmark/SendGrid behind interface), user-configurable prefs (`notification_preferences` table), Web Push later.

---

## Repo layout (current state)

```
backend/
  cmd/{api,scheduler,worker}/main.go     — three binaries (build ✓). api connects Postgres (GET /health + /health/db); scheduler runs the world clock (S02-03) and has a health listener; worker consumes the bus with a health listener
  cmd/api/{router,handlers,middleware,lifecycle_handlers}.go — Gin API: auth (S02-01) + admin world lifecycle & config (S02-02/03) + job-offer inbox/accept/decline/resign (S02-02); httpOnly cookie pair; CORS via APP_ORIGIN
  cmd/ref-seed/main.go                   — reference-data seeder (nationalities + name_pool), idempotent; wired into compose + CI
  cmd/user-create/main.go                — dev bootstrap account CLI (email/password/world; creates auth.users + unemployed manager; prints OPD-02 note)
  internal/{club,player,transfer,finance,match,social}/  — domain packages + Service interfaces (stubs + structs, no DB impl yet)
  internal/world/   — world lifecycle Service (S02-02): CreateWorld/GetWorld/ListWorlds/SetStatus (provisioning→active/paused→active/open_beta/archived), world_config seed on launch, WORLD_* events via pkg/eventbus Publishable; integration tests ✓
  internal/manager/ — job-offer + assignment Service (S02-02): CreateJobOffer/ListOffers/AcceptJobOffer/DeclineJobOffer/Resign/Sack, one-active-assignment enforcement, manager_history open/close, JOB_OFFER_ACCEPTED + MANAGER_RESIGNED/SACKED events, append-only reputation boundary (GetReputation/ListCareerHistory/WorldReputationTotal); integration tests ✓
  internal/scheduler/ — configurable world clock (S02-03): reads tick.*_cadence cron specs from world_config per playable world, re-syncs on a poll interval (runtime config change, no redeploy), fires WORLD_TICK(granularity) events advancing world.worlds.current_tick; pg_advisory_lock single leader; unit + integration tests ✓
  internal/auth/    — auth Service: Login (OPD-15 world ladder) + Refresh (rotate+revoke via auth.sessions, hashed tokens); sentinel errors; integration tests (tag `integration`)
  internal/testdb/  — shared integration harness (migrate + truncate, CreateWorld/CreateUser/Join/CreateClub/CreateClubWithAIManager/MakeAdmin); consumed by pkg/eventbus, internal/auth, internal/world, internal/manager, internal/scheduler, cmd/api tests
  pkg/eventbus/    — EventBus (river, Postgres-native). river.go: RiverBus publish (world.events + enqueue), Subscribe, Start/Stop; worker.go: touchline_event job; integration + unit tests
  pkg/playergen/   — DB-free procedural generation: LoadNameData (curated data/names JSON), PoolGenerator, NationalityPool, NameRegistry, PlayerFactory (age 17–33, 12 positions), tests ✓
  pkg/auth/        — JWTConfig, GenerateTokenPair (access sub/user_id/jti/exp/iat/type, refresh sub/jti/exp/iat/type; HS256-pinned), ValidateAccessToken/ValidateRefreshToken, HashRefreshToken, tests ✓ (golang-jwt/v5)
  migrations/      — 0000–0014 base + 0016–0022 river + 0023 manager world scoping (uq_managers_user_world, uq_manager_one_job_per_user) + 0024 manager lifecycle contract (manager.job_offers + pending-unique, world name unique, one-active-person index, status↔club CHECK, reputation 'career' category)
  data/names/      — 21 curated nationality datasets (README.md + PROVENANCE.md); eng/sco are documented non-ISO slugs
  go.mod           — module github.com/touchline/backend, go 1.25
frontend/
  nuxt.config.ts   — Nuxt 3 + @vite-pwa/nuxt + @pinia/nuxt + Tailwind
  app.vue, pages/{index, auth/login}.vue
  composables/{useDashboard,useSocket}.ts
  stores/{club,squad,finance,transfers,social}.ts   — Pinia
  server/index.ts  — BFF health/status only (no game logic)
  assets/css/main.css, tailwind.config.js, tsconfig.json, package.json, .env.example
infra/
  docker/          — Dockerfile.{api,scheduler,worker,frontend} (multi-stage; go 1.25-alpine builder)
  helm/{api,scheduler,worker,frontend}/  — Chart.yaml + values.yaml + templates (deployment/service/ingress; worker has HPA on event_bus_queue_depth)
docker-compose.yml  — redis + migrations + ref-seed + api + scheduler + worker + frontend; requires Neon DATABASE_URL (healthchecks + env guards; opt-in Postgres override infra/ci/docker-compose.postgres.yml)
.github/workflows/ci.yml — backend lint/vet/test, eventbus integration (postgres:16), migrations + ref-seed idempotency, frontend lint/typecheck, compose config + full-stack smoke (Postgres override), multi-arch GHCR build on main
README.md, .gitignore, OPENCODE.md
```

---

## Build/test status (as of this handoff)

- `cd backend && go build ./...` ✓
- `cd backend && go test ./...` ✓ (auth + playergen smoke tests)
- `cd backend && go vet ./...` ✓
- Integration tests (tag `integration`) hit a real Postgres pointed at by `TEST_DATABASE_URL` (testcontainers fallback needs Docker): `cd backend && TEST_DATABASE_URL=… go test -p 1 -tags integration -race ./pkg/eventbus/... ./internal/auth/... ./internal/world/... ./internal/manager/... ./internal/scheduler/... ./cmd/api/...` ✓ (eventbus round-trip; auth service ladder + rotation; world lifecycle transitions + event log + config seed; job-offer accept/decline/resign/sack + one-active invariant + reputation deltas; world clock — configured daily tick arriving at a worker handler, cadence change on next sync, pause unregisters, counter/payload contract; HTTP login/dashboard/refresh + admin world lifecycle/config + offer inbox/accept/resign 401/403 coverage). `-p 1` is required — packages share + truncate one DB.
- Live API smoke (S02-02) verified by hand against a local Postgres: admin login → create+launch world (archived terminal) → config seeded → AI-club offer created → candidate logged in, saw the offer, accepted (bot stood down, club handed over, history opened, +5 reputation) → resigned (history closed, club back to AI control); double resign 409.
- Frontend `npm install` + `npm run dev` NOT yet exercised (no package-lock committed; `npm install` will generate it). `vue-tsc --noEmit` typecheck passes locally once `vite` is resolvable (pnpm doesn't hoist a direct `node_modules/vite`; CI's `npm install` hoists so the plain command works there).
- `docker compose` config-checked and full-stack-boot verified in the CI `compose` job; local Docker is absent on the dev machine, so the stack boot is CI-verified only (see `docs/development.md`, OPD-14).

---

## What is NOT built yet (Phase 0 backlog — next steps for an agent)

1. **DB migrations** (golang-migrate, one set per schema). Done in `backend/migrations/0000–0014` (base schemas) + `0016–0022` (river), see its README. The weighted nationality pool is `ref.nationalities.generation_weight` + `ref.name_pool` (global, non-world-scoped — see resolved decision OPD-11 in `docs/product_manager.md`); `world.events`, `world.world_config` (tick cadences), and `world.news_stories` are defined under `world`. Migrations execute via a Helm pre-install/pre-upgrade hook (`infra/helm/migrations`), a `docker compose` migration service, and a CI gate.
2. **~~Wire river properly~~** ✅ Done (S01-02): `pkg/eventbus` runs a real river round-trip against exported migrations `0016–0022`; publish → queue → worker handler, unique-by-args enqueue, at-least-once delivery with handler-side idempotency, integration-tested.
3. **~~Gin API wiring~~** ✅ Done (S02-01): `cmd/api` real router + CORS + auth middleware; `POST /api/auth/login` (+ world picker branch), `POST /api/auth/refresh` (rotation against `auth.sessions`), protected `GET /api/dashboard` stub; `cmd/user-create` dev bootstrap; frontend `useAuth` + `login.vue` world picker (httpOnly cookies, `credentials:'include'`). Session/identity model: OPD-15.
4. **~~World lifecycle + assignment contract~~** ✅ Done (S02-02, OPD-16): admin-only world lifecycle (`internal/world` + `POST /api/admin/worlds{,/:id/status}`), `manager.job_offers` offer state machine + AI-club handover (`internal/manager` + offers/resign endpoints), one-active-assignment invariants, `WORLD_*`/`JOB_OFFER_ACCEPTED`/`MANAGER_RESIGNED|SACKED` events, append-only reputation boundary (migration 0024). League composition deliberately deferred to S03-01 (OPD-01).
5. **~~Scheduler~~** ✅ Done (S02-03, OPD-17): `internal/scheduler` reads `tick.*_cadence` cron specs from `world_config` per playable world, re-syncs every poll interval (runtime change, no redeploy), fires `WORLD_TICK(granularity)` events advancing `current_tick`; single-leader Postgres advisory lock; malformed specs skipped. Match ticks deferred to the match engine's live-match goroutines (S04).
6. **Worker**: subscribes to `WORLD_TICK` (granularity-aware log handler) + `WORLD_*`/`JOB_OFFER_*`/`MANAGER_*`; engine handlers dispatch per granularity as the engines land (S03-01 onwards).
7. **~~playergen data~~** ✅ Done (S01-04): 21 curated nationality datasets in `backend/data/names` (see its README + PROVENANCE).
8. **~~Neon `DATABASE_URL` + env docs~~** ✅ Done (S01-05): root `.env.example` + `backend/.env.example` + `docs/development.md` (OPD-14).
9. **Redis wiring**: session store, rate limiting, WS pub/sub.
10. **World bootstrap (S03-01)**: seed clubs/leagues/first squads at game start (admin world create currently leaves the world empty of leagues/clubs) — consumes OPD-01.

## Phase roadmap summary (plan §16)

- **Phase 0 (pre-MVP)**: scaffolding, event bus + `world.events` working end-to-end, auth E2E, playergen, core schemas, scheduler publishing WORLD_TICK. Exit: create world → generate club + squad → see daily tick.
- **Phase 1 (MVP)**: match engine + fixtures/tables, squad/tactics/training (Simple mode), transfers + human-human negotiation, contracts, ledger finance, board + sackings, player morale, social (messaging/rivalries), absence mode/PoliticsBot, dashboard, PWA + email notifications.
- **Phase 2 (V1)**: academies, full personalities + relationship graph, agents, injuries, dynamic potential, manager-created competitions, news read-model, club DNA driving AI, supporters, anti-abuse.
- **Phase 3 (V2)**: club creation, ownership path, national football, retired-player careers (Person entity), stadiums, extraction candidate.
- **Phase 4 (V3)**: multiple inhabitable roles against same event log; global economy; premium analytics last.

## Conventions & gotchas

- **Do NOT add `balance` columns** to finance tables. Ledger only.
- **`world_id` on every schema** from the first migration — do not retrofit.
- `go mod tidy` removes unused deps (redis/cron are currently declared only in plan — will vanish until imported; that's fine). River stays: imported by `pkg/eventbus`.
- Match engine stays **pure** (`Simulate(seed, teamA, teamB, tacticsA, tacticsB) MatchResult`), zero DB — testability + determinism.
- Comments: project code intentionally has few comments; treat docs as the spec.
- Keep frontend server routes BFF-only; game logic never in Nuxt.
- Match tick cadence is config at runtime (`world_config`), not code.

## Files you must read before touching related code

- `docs/Touchline_Technical_Implementation_Plan.md` — §3 arch diagram, §4 event core, §6 schemas, §9 match engine, §11 frontend, §12 API surface, §15 infra, §16 phases.
- `internal/*/` structs mirror plan §6 schema table — keep in sync when writing migrations.
- `docs/Touchline — Persistent Multiplayer Football Manager PRD.md` — sections 52–73 for UX/features; 74–77 for phase scope.