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
| Dev infra | Docker Compose (Redis + api + scheduler + worker + frontend). **Postgres is NOT in compose — it's Neon.** |
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
- **One manager, one club at a time** platform-wide. To take another job: resign/by sack. `manager_reputation` = global append-only *history log* (display only); any reputation *score* used in hiring logic is computed **world_id-scoped**.
- Match viewing = event-feed (see above). Email notifications (SES/Postmark/SendGrid behind interface), user-configurable prefs (`notification_preferences` table), Web Push later.

---

## Repo layout (current state)

```
backend/
  cmd/{api,scheduler,worker}/main.go     — three binaries (all build ✓)
  internal/{club,player,transfer,finance,match,social,world}/  — domain packages + Service interfaces (stubs + structs, no DB impl yet)
  pkg/eventbus/    — EventBus interface + river.go stub publish (inserts into world.events)
  pkg/playergen/   — PlayerNameGenerator iface, file-backed gen, NationalityPool, PlayerFactory, tests ✓
  pkg/auth/        — JWTConfig, GenerateTokenPair, ValidateToken, tests ✓ (uses golang-jwt/v5)
  data/names/      — README + format example. NOTE: no real name data curated yet.
  go.mod           — module github.com/touchline/backend, go 1.22
frontend/
  nuxt.config.ts   — Nuxt 3 + @vite-pwa/nuxt + @pinia/nuxt + Tailwind
  app.vue, pages/{index, auth/login}.vue
  composables/{useDashboard,useSocket}.ts
  stores/{club,squad,finance,transfers,social}.ts   — Pinia
  server/index.ts  — BFF health/status only (no game logic)
  assets/css/main.css, tailwind.config.js, tsconfig.json, package.json, .env.example
infra/
  docker/          — Dockerfile.{api,scheduler,worker,frontend} (multi-stage; go 1.22-alpine builder)
  helm/{api,scheduler,worker,frontend}/  — Chart.yaml + values.yaml + templates (deployment/service/ingress; worker has HPA on event_bus_queue_depth)
docker-compose.yml  — redis + api + scheduler + worker + frontend; requires Neon DATABASE_URL
.github/workflows/ci.yml — backend lint/vet/test, frontend lint/typecheck, multi-arch GHCR build on main
README.md, .gitignore, OPENCODE.md
```

---

## Build/test status (as of this handoff)

- `cd backend && go build ./...` ✓
- `cd backend && go test ./...` ✓ (auth + playergen smoke tests)
- `cd backend && go vet ./...` ✓
- Frontend `npm install` + `npm run dev` NOT yet exercised (no package-lock committed; `npm install` will generate it).
- `docker compose up` NOT yet exercised (needs Neon `DATABASE_URL`).

---

## What is NOT built yet (Phase 0 backlog — next steps for an agent)

1. **DB migrations** (golang-migrate, one set per schema). Done in `backend/migrations/0000–0014` (see its README). The weighted nationality pool is `ref.nationalities.generation_weight` + `ref.name_pool` (global, non-world-scoped — see resolved decision OPD-11 in `docs/product_manager.md`); `world.events`, `world.world_config` (tick cadences), and `world.news_stories` are defined under `world`. Migrations execute via a Helm pre-install/pre-upgrade hook (`infra/helm/migrations`), a `docker compose` migration service, and a CI gate.
2. **Wire river properly**: `pkg/eventbus` currently inserts into `world.events` directly; still needs real river round-trip (publish→queue→worker). Add river dep back to go.mod when implemented (tidy currently strips unused deps — expected).
3. **Gin API wiring** (`cmd/api`): real router, auth middleware, `/api/auth/login|refresh`, `/api/dashboard`, health.
4. **Scheduler**: `robfig/cron`, read cadences from `world_config`, emit `WORLD_TICK` events.
5. **Worker**: subscribe to ticks, dispatch to engine handlers (granularity-scoped).
6. **playergen data**: curate real per-nationality name files (content/data task).
7. **Neon `DATABASE_URL`** + env docs for local dev.
8. **Redis wiring**: session store, rate limiting, WS pub/sub.

## Phase roadmap summary (plan §16)

- **Phase 0 (pre-MVP)**: scaffolding, event bus + `world.events` working end-to-end, auth E2E, playergen, core schemas, scheduler publishing WORLD_TICK. Exit: create world → generate club + squad → see daily tick.
- **Phase 1 (MVP)**: match engine + fixtures/tables, squad/tactics/training (Simple mode), transfers + human-human negotiation, contracts, ledger finance, board + sackings, player morale, social (messaging/rivalries), absence mode/PoliticsBot, dashboard, PWA + email notifications.
- **Phase 2 (V1)**: academies, full personalities + relationship graph, agents, injuries, dynamic potential, manager-created competitions, news read-model, club DNA driving AI, supporters, anti-abuse.
- **Phase 3 (V2)**: club creation, ownership path, national football, retired-player careers (Person entity), stadiums, extraction candidate.
- **Phase 4 (V3)**: multiple inhabitable roles against same event log; global economy; premium analytics last.

## Conventions & gotchas

- **Do NOT add `balance` columns** to finance tables. Ledger only.
- **`world_id` on every schema** from the first migration — do not retrofit.
- `go mod tidy` removes unused deps (river/redis/cron are currently declared only in plan — will vanish until imported; that's fine).
- Match engine stays **pure** (`Simulate(seed, teamA, teamB, tacticsA, tacticsB) MatchResult`), zero DB — testability + determinism.
- Comments: project code intentionally has few comments; treat docs as the spec.
- Keep frontend server routes BFF-only; game logic never in Nuxt.
- Match tick cadence is config at runtime (`world_config`), not code.

## Files you must read before touching related code

- `docs/Touchline_Technical_Implementation_Plan.md` — §3 arch diagram, §4 event core, §6 schemas, §9 match engine, §11 frontend, §12 API surface, §15 infra, §16 phases.
- `internal/*/` structs mirror plan §6 schema table — keep in sync when writing migrations.
- `docs/Touchline — Persistent Multiplayer Football Manager PRD.md` — sections 52–73 for UX/features; 74–77 for phase scope.