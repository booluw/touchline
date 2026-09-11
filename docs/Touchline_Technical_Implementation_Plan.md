# Touchline — Technical Implementation Plan
### Go backend · Nuxt 3 (Vue) frontend · Cloud-agnostic, container-first infrastructure

This plan turns the Touchline PRD into a buildable system. It's phased to match the PRD's own MVP → V1 → V2 → V3 structure (sections 74–77), so each phase ships something playable and the architecture never has to be thrown away to reach the next one.

---

## 1. Guiding technical principles

These map directly to the PRD's product principles (section 81) but restated as engineering constraints:

1. **Server is the only source of truth.** The client never simulates outcomes — it renders state and submits commands. This is non-negotiable for a competitive multiplayer game.
2. **Modular monolith first, not microservices first.** The PRD's diagram in section 69 shows five "engines." Running those as five separately deployed services on day one is a distributed-systems tax you don't need at MVP scale. Build them as **separate Go modules/packages with hard boundaries and their own DB schemas**, communicating only through an internal event bus interface. This lets you extract any engine into its own service later (V2/V3) without a rewrite — you're just changing the transport the interface uses.
3. **Everything is an event.** Section 63's event list becomes the backbone of persistence, notifications, news generation, and eventually replay/audit. Build the event bus in phase 1, not as a retrofit.
4. **Determinism where money or competition is at stake.** Match simulation and any RNG-driven outcome that affects standings must be seeded and replayable (section 68). This is a testing and anti-cheat requirement, not a nice-to-have.
5. **Async-first UX.** Nothing requires two managers to be online simultaneously (section 4). This shapes the whole backend: actions are commands that get queued/validated against deadlines, not real-time turns.
6. **PWA, not native.** Per your direction, mobile = responsive web app installed as a PWA. This avoids maintaining separate iOS/Android codebases while still getting home-screen install, offline shell, and push notifications (via Web Push).

---

## 2. Tech stack

| Layer | Choice | Why |
|---|---|---|
| Backend language | **Go** | Strong concurrency primitives (goroutines/channels) map naturally onto tick-based simulation workers; static typing and fast compile times suit a large domain model; excellent for long-running services. |
| Backend framework | **Chi or Echo** for HTTP, no heavy framework | Touchline's complexity is in the domain model, not routing. Keep the HTTP layer thin. |
| API style | **REST (JSON) + WebSockets**, gRPC internally between engines once split out | REST/JSON is simplest for a Nuxt SPA to consume; WebSockets carry live match ticks and notifications; gRPC is reserved for later inter-service calls if/when engines become real services. |
| Database | **PostgreSQL** | Relational integrity matters enormously here (contracts, transfers, finances are all ledger-like). Use Postgres schemas per engine to keep the modular-monolith boundaries real at the DB level too. |
| Cache / ephemeral state | **Redis** | Session storage, rate limiting, live-match transient state, pub/sub for WebSocket fan-out across multiple API pods. |
| Event bus / job queue | **NATS JetStream** (or Postgres-backed queue via `river` as a simpler fallback) | Cloud-agnostic, lightweight, single binary, has persistence + replay which matches the event-sourcing needs in section 63/68. `river` (Postgres-native Go job queue) is a good "one less moving part" option for MVP if you want to defer running NATS at all. |
| Simulation scheduling | **Custom Go tick scheduler** + cron-style jobs (`robfig/cron`) | World ticks (15-min match ticks, hourly, daily, weekly, seasonal — section 4) are just scheduled jobs that emit events. |
| Frontend framework | **Nuxt 3 (Vue 3, Composition API)** | SSR for fast first load and SEO on public pages (club pages, league tables), file-based routing, built-in PWA module, good TypeScript support. |
| Frontend state | **Pinia** | Standard for Vue 3/Nuxt; stores map cleanly onto domain areas (squad, finances, transfers). |
| Realtime client | **WebSocket client wrapped in a Pinia store/composable** | Single socket connection per session; server pushes typed events, store mutates state, components react automatically. |
| PWA | **`@vite-pwa/nuxt`** | Manifest, service worker, offline shell caching, installability, Web Push. |
| Styling/UI | **Tailwind CSS** + a small internal component library | Fast iteration across the large number of screens in section 52. |
| Auth | **Custom JWT (access + refresh) via httpOnly cookies**, or **Ory Kratos / Clerk** if you want to outsource it | Given social trust (section 49) and anti-multi-accounting (section 50) matter a lot here, owning auth early gives you the hooks you'll need (device fingerprinting, session history) — but don't over-invest before MVP validates the core loop.
| Infra | **Docker + Docker Compose (dev)**, **Kubernetes (prod)**, Helm charts | Matches your "cloud-agnostic" preference — same manifests run on any managed K8s (EKS/GKE/AKS) or bare-metal K8s (k3s) later. |
| CI/CD | **GitHub Actions** → build/test/lint → push image → deploy via Helm/Argo CD | Argo CD gives you GitOps deployment, which pairs well with "cloud-agnostic." |
| Observability | **OpenTelemetry** → Prometheus (metrics) + Grafana (dashboards) + Loki (logs) + Tempo/Jaeger (traces) | All open-source, all portable across clouds. |
| Object storage | **S3-compatible (MinIO self-hosted or any cloud's S3 API)** | Club crests, generated media, exports. |

---

## 3. High-level architecture

```
                         ┌────────────────────┐
                         │   Nuxt 3 SPA/PWA    │
                         │  (Vue 3 + Pinia)    │
                         └─────────┬───────────┘
                        REST (JSON) │ WebSocket
                                    │
                         ┌──────────▼───────────┐
                         │     API Gateway       │  (Go, Chi/Echo)
                         │  authn/z · rate limit │
                         │  · request validation │
                         └──────────┬───────────┘
                                    │ in-process calls (MVP)
                                    │ or gRPC (post-split)
        ┌───────────┬───────────┬──┴────────┬───────────┬────────────┐
        ▼           ▼           ▼            ▼           ▼            ▼
   ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌─────────┐ ┌──────────┐
   │  Club/  │ │ Transfer│ │ Finance │ │  Match/   │ │ Social  │ │  World/  │
   │ Player  │ │ Engine  │ │ Engine  │ │Simulation │ │ Engine  │ │  News    │
   │ Engine  │ │         │ │         │ │  Engine   │ │         │ │  Engine  │
   └────┬────┘ └────┬────┘ └────┬────┘ └─────┬─────┘ └────┬────┘ └────┬─────┘
        │           │           │            │            │           │
        └───────────┴─────┬─────┴────────────┴────────────┴───────────┘
                           ▼
                 ┌───────────────────┐
                 │   Event Bus        │   (NATS JetStream / river)
                 │  WORLD_TICK, GOAL,  │
                 │  TRANSFER_BID, …    │
                 └─────────┬───────────┘
                           ▼
                 ┌───────────────────┐
                 │  Simulation Workers│  (Go goroutine pools,
                 │  (tick processors) │   horizontally scalable)
                 └─────────┬───────────┘
                           ▼
              ┌────────────────────────────┐
              │  PostgreSQL (schema/engine)  │◄──── Redis (cache, pub/sub, sessions)
              └────────────────────────────┘
```

Each "Engine" box in phase 1 is a **Go package** behind an interface (`ClubService`, `TransferService`, etc.), all compiled into one binary, all writing to the same Postgres instance but in **separate schemas** (`club`, `transfer`, `finance`, `match`, `social`, `world`). This is what lets you split any one of them into its own deployable service in V2/V3 by swapping the in-process call for a gRPC client, with no domain-logic rewrite.

---

## 4. Event-driven core

Every meaningful state change is an event, per PRD section 63. Concretely:

```go
type WorldEvent struct {
    ID          uuid.UUID
    Type        string          // "PLAYER_SOLD", "PLAYER_INJURED", ...
    OccurredAt  time.Time
    WorldTick   int64           // monotonic tick counter for ordering/replay
    ActorID     *uuid.UUID      // manager who triggered it, if any
    Payload     json.RawMessage // typed per event Type
    CausedBy    *uuid.UUID      // parent event ID, for causal chains (sec. 63 example)
}
```

- Events are appended to an **event log table** (`world.events`) and simultaneously published to JetStream/river.
- Subscribers (Finance engine, Social engine, News engine, Notification service) react asynchronously and independently — this is exactly the `PLAYER_SOLD → relationship disrupted → morale drop → meeting requested → news story` chain in section 63.
- The event log doubles as your **audit trail** for anti-abuse (section 50) and **club/player history** (sections 29, 61) — "history" isn't a separate feature to build, it's a read model (materialized view) over the event log.
- Determinism (section 68): every event that involves randomness stores the **seed** used, keyed to `(WorldEvent.ID, WorldTick)`. Replaying a match = re-running the match engine with the same seed and the same input events.

---

## 5. World clock & tick scheduler

```
Tick type          Frequency        Driven by
------------------  ---------------  -----------------------------
Match tick          15 min (live)    goroutine per live match, ends on full time
Economic/social      hourly           cron job → emits WORLD_TICK(hourly)
Daily                daily            training resolution, injury checks, news digest
Weekly               weekly           league table snapshot, board review
Monthly              monthly          financial statements, wage payments
Seasonal             per season       promotion/relegation, contract expiry, awards
```

Implementation: a single **Scheduler service** (`robfig/cron` inside a Go worker) publishes `WORLD_TICK` events with a `granularity` field onto the event bus. Every engine subscribes only to the granularities it cares about. This keeps cadence **configurable at runtime** (PRD explicitly asks for this in section 4) without redeploying — cadence lives in a config table, not in code.

Live matches are the one place that needs a dedicated real-time process: each live match gets its own goroutine running a deterministic tick loop (see section 9 below), pushing state diffs to subscribed WebSocket clients via Redis pub/sub (so it works across multiple API pods).

---

## 6. Data model (core schemas)

Rather than one giant schema, split along the engine boundaries. Abbreviated (full DDL is a phase-1 deliverable, not reproduced here):

**`club` schema** — `clubs`, `club_dna`, `boards`, `board_mandates`, `facilities`, `academies`, `supporter_groups`, `club_history`
**`player` schema** — `players`, `player_attributes`, `player_personality`, `player_hidden_traits`, `player_history`, `contracts`
**`manager` schema** — `managers`, `manager_reputation`, `manager_history`, `job_security_snapshots`
**`transfer` schema** — `transfer_listings`, `bids`, `negotiations`, `clauses` (sell-on/buy-back/release), `loans`
**`finance` schema** — `finance_accounts`, `ledger_entries` (never a bare `balance` column — see section 36/67 of PRD), `budgets`, `wage_commitments`
**`social` schema** — `relationships` (graph edges), `messages`, `manager_trust_scores`, `promises`
**`match` schema** — `fixtures`, `matches`, `match_events`, `match_seeds`
**`competition` schema** — `competitions`, `competition_rules`, `standings`
**`world` schema** — `events` (the event log), `news_stories`, `world_config` (tick cadences, feature flags)

Key modeling decisions:

- **Ledger, not balance.** `finance.ledger_entries` is append-only (section 67). `balance` is always a derived SUM(), never a stored/updated column, or you will eventually get it out of sync with reality and lose the "explain where the money went" feature (section 54) that's core to the product.
- **Relationships as a graph table**, not columns on `players`:
  ```sql
  CREATE TABLE social.relationships (
    id UUID PRIMARY KEY,
    entity_a_id UUID NOT NULL,
    entity_a_type TEXT NOT NULL, -- 'player' | 'manager' | 'club'
    entity_b_id UUID NOT NULL,
    entity_b_type TEXT NOT NULL,
    relationship_type TEXT NOT NULL, -- 'friendship' | 'rivalry' | ...
    strength INT NOT NULL,
    trust INT NOT NULL,
    sentiment INT NOT NULL,
    last_interaction_at TIMESTAMPTZ,
    UNIQUE (entity_a_id, entity_b_id, relationship_type)
  );
  ```
  This directly implements PRD section 66 and is queryable for squad-dynamics calculations (section 15) without an actual graph database — Postgres recursive CTEs are sufficient at this scale (thousands of clubs, not billions of edges).
- **`board_mandates` as structured, negotiable rows**, not free text — primary/secondary/strategic/financial targets each get their own row with a `status` (pending/agreed/met/broken) so job-security calculations (section 9) can query them directly.

---

## 7. Procedural player generation (names + nationality)

Since the PRD explicitly avoids real-player licensing, this needs its own subsystem, built early because almost everything else depends on having a believable player pool.

**Approach:**

1. **Nationality distribution table** (`world.nationality_pool`): weight each nationality by realistic football-producing populations (e.g., heavier weighting toward Brazil, Argentina, France, Nigeria, England, Spain, Germany, etc., matching real-world football talent distribution), so a generated world "feels" like football rather than being uniformly random.
2. **Name generation, per nationality**: maintain curated first/last name lists per nationality/culture (not AI-generated at runtime — precomputed, versioned data files, e.g. `data/names/nigeria.json`, `data/names/brazil.json`), then combine `random(firstNames) + random(lastNames)` with light rules (e.g., some cultures use a single common name / patronymic style, Brazilian players commonly get single-name "shirt names" derived from their full name).
   - This is a **data engineering task, not a code task** — budget time to source/curate open name-frequency datasets per country rather than inventing names.
   - Store generation as `PlayerNameGenerator` interface so you can swap/extend per-nationality generators independently.
3. **Nationality → attribute-distribution bias** (optional but cheap and adds flavor consistent with PRD section 30's pyramid/culture framing): nationality can lightly bias starting attribute archetypes (e.g., technical vs physical emphasis) — configurable data, not hardcoded stereotypes baked into logic, so it's tunable/removable.
4. **Uniqueness**: enforce a `(first_name, last_name)` collision check at generation time, regenerate on collision within a nationality pool, to avoid two players with identical names in the same world (cosmetic annoyance, not a system risk, but cheap to prevent).
5. **Growth over time**: academy output (section 23) and general world player generation both call the same `PlayerNameGenerator` + `PlayerFactory`, parameterized by region (so an academy in a specific country generates nationally-plausible players).

This whole subsystem should be a **standalone Go package (`pkg/playergen`)** with no dependencies on the rest of the domain, unit-testable in isolation, and shippable in the first sprint.

---

## 8. Explainability layer (PRD section 54)

This is a cross-cutting requirement, not a single feature, so it needs a dedicated pattern:

- Every system that produces a "decision" (board confidence change, transfer-request trigger, AI bid, job-security change) writes a **reasoning breakdown**, not just a resulting number:
  ```go
  type Explanation struct {
      Subject   string            // "board_confidence", "transfer_desire", ...
      Score     int
      Factors   []ExplanationFactor
  }
  type ExplanationFactor struct {
      Label string // "Wage bill 18% above structure"
      Delta int    // -12
  }
  ```
- Store these alongside the events that caused them (`world.events.payload` includes the `Explanation`), so both the UI and the news generator can render "why" without recomputing anything.
- This single pattern implements PRD sections 9, 16, 18, 54 all at once — build it once, reuse everywhere.

---

## 9. Match simulation engine

- **Deterministic, seeded, tick-based.** A match is simulated in discrete ticks (not necessarily 15 real-world minutes — that's the *live-match-in-world-time* cadence; internally a match can resolve in a few seconds of compute using its own micro-ticks, e.g. 1 tick per simulated minute).
- Inputs: both squads' attributes, tactics, personality/fatigue/confidence modifiers, a **random seed**.
- Output: a `MatchResult` plus an ordered `[]MatchEvent` (goals, cards, injuries, substitutions) — the event list is what both the live viewer and the "quick result" mode consume; they're the same simulation, just rendered differently (section 55: full tactical control / assisted / pre-match / quick result are **viewing modes**, not different simulations).
- Live viewing mode streams `MatchEvent`s over WebSocket as they're generated (or replayed at a controlled pace even if computed instantly server-side, to preserve the "watching a match" feel — this is a deliberate UX pacing choice, cheap to implement, worth doing).
- Keep the match engine in its own package with **zero DB dependency** — pure function `Simulate(seed, teamA, teamB, tacticsA, tacticsB) MatchResult`. This makes it trivially unit-testable and safe to run in parallel workers.

---

## 10. Absence mode / delegation (sections 71–73)

Technically, this is just: every mutable decision point (squad selection, training focus, transfer response, contract renewal threshold) has an optional `Policy` row owned by the manager. When a tick/event fires and no manager input has been received by the deadline, the **same command handlers a human would trigger are invoked by a "PolicyBot" actor** using the stored policy. This means:

- You do **not** build a separate "AI plays for you" system.
- You build one command layer (`SelectSquad`, `RespondToBid`, `SetTrainingFocus`, …) that both humans and PolicyBot call.
- `PolicyBot` is just another `ActorID` in the event log, clearly distinguishable in history/audit.

This also directly reuses itself as the base for **AI-controlled clubs** (section 45–47) — an AI club is a club whose every decision point is driven by PolicyBot with a richer, personality-weighted policy instead of a simple threshold. Building absence-mode delegation and AI clubs as *the same underlying system* is one of the highest-leverage architectural decisions in this plan.

---

## 11. Frontend architecture (Nuxt 3)

```
/app
  /pages           -- file-based routing: /club/[id], /squad, /transfers, /world, ...
  /components
  /composables      -- useMatchSocket(), useExplanation(), useTicker()
  /stores           -- Pinia: club.ts, squad.ts, finance.ts, transfers.ts, social.ts
  /server           -- Nuxt server routes ONLY for BFF concerns (auth cookie handling,
                        request proxying with SSR-safe headers) — NOT game logic
  nuxt.config.ts    -- @vite-pwa/nuxt, runtime config for API base URL
```

- **SSR** for the dashboard/home (section 53) and public pages (club profiles, league tables, competition pages) — fast first paint, good for sharing links into a specific club/competition.
- **CSR-only** for deeply interactive screens (live match viewer, tactics board) — no benefit from SSR there.
- **One WebSocket connection**, opened after auth, multiplexed by event `type` (`match_tick`, `notification`, `board_update`, …), fanned out to whichever Pinia stores care — implemented as a `useSocket()` composable + a small internal pub/sub so components don't each open their own connection.
- **Home dashboard (section 53)** is a dedicated aggregation API endpoint (`GET /api/dashboard`) that server-side joins "urgent / important / interesting" across engines — don't make the client fan out to six APIs to build one screen.
- **PWA specifics**: app-shell caching for offline resilience (read-only view of last-known state when offline). Notifications (transfer bids, financial warnings, match reports) are delivered by **email with user-configurable preferences** at MVP (see section 18, item 4); Web Push can be layered in later as an additional opt-in channel on the same preferences model, so the "check back when something happens" loop works without a native app.

---

## 12. API surface (representative, not exhaustive)

```
POST   /api/auth/login
POST   /api/auth/refresh

GET    /api/dashboard
GET    /api/clubs/:id
GET    /api/clubs/:id/squad
POST   /api/clubs/:id/tactics
POST   /api/clubs/:id/training-plan

GET    /api/players/:id
POST   /api/players/:id/interact          # talk to player (sec. 16)
POST   /api/players/:id/promise           # make a promise (sec. 17)

GET    /api/transfers/listings
POST   /api/transfers/bids
POST   /api/transfers/bids/:id/respond    # accept/reject/counter (sec. 21)

GET    /api/finance/ledger
GET    /api/finance/summary               # cash vs budget breakdown (sec. 36)

GET    /api/competitions
POST   /api/competitions                  # manager-created competitions (sec. 33)

WS     /ws                                # single multiplexed socket
```

Every endpoint that changes state returns not just the new resource but the **`Explanation`** object where relevant (see section 8 of this plan), so the frontend never has to guess why something happened.

---

## 13. Anti-abuse & competitive integrity (sections 49–51)

- **Deterministic replay** (section 8/68 above) is your primary anti-cheat tool for match outcomes.
- **Trade/collusion monitoring** runs as an **async analyzer subscribed to `TRANSFER_COMPLETED` events**, not inline in the transfer flow — flag-for-review, don't block, at MVP. Heuristics: fee far below/above market valuation, repeated trades between the same two managers, newly-created accounts trading with established ones.
- **Multi-account detection**: device fingerprint + IP clustering + behavioral similarity, stored separately from gameplay data, reviewed by commissioners/admin tooling, never auto-punitive without review at MVP.
- **Social trust score** (section 49) is a derived read-model over the event log (broken promises, honored trades, dispute outcomes) — same pattern as club/player history.
- Build a minimal **admin/commissioner tool** (internal Nuxt app or just a protected route set) early — you will need to manually adjudicate disputes long before you need automated ML-based detection.

---

## 14. Testing strategy

- **Unit tests**: pure engines (match simulation, player generation, job-security scoring) — highest ROI, no DB needed.
- **Property-based tests** for the finance ledger (sum of entries must always reconcile to reported balance; no operation should be able to create/destroy money) — this matters a lot given section 36/67's emphasis on financial trust.
- **Golden replay tests**: store a fixed seed + inputs + expected `MatchResult`, assert byte-for-byte reproducibility — protects the determinism guarantee (section 68) from regressions.
- **Integration tests** against a real Postgres via `testcontainers-go` for each engine's schema.
- **Load testing** (k6 or similar) focused on the event bus and WebSocket fan-out before scaling tests of "thousands of clubs" (section 69) — that's where a persistent multiplayer world actually breaks first.

---

## 15. Infrastructure & deployment

```
Repo layout (monorepo):
/backend
  /cmd/api            -- main API binary
  /cmd/scheduler       -- tick scheduler binary
  /cmd/worker          -- simulation worker pool binary
  /internal/club
  /internal/player
  /internal/transfer
  /internal/finance
  /internal/match
  /internal/social
  /internal/world
  /pkg/playergen
  /pkg/eventbus
/frontend              -- Nuxt 3 app
/infra
  /docker              -- Dockerfiles
  /helm                -- Helm charts per deployable (api, scheduler, worker, frontend)
  /terraform (optional) -- only if/when you pick a specific cloud, kept minimal & swappable
```

- **Dev**: `docker-compose.yml` on a local laptop spins up Redis, NATS, API, worker, scheduler, frontend with hot reload — Postgres itself runs on **Neon** (serverless, branchable) from day one rather than in the compose stack, so dev environments match prod-shape data more easily and there's no local Postgres to manage. Plan to migrate to a self-hosted Postgres instance once infrastructure is owned directly.
- **Prod**: initial target is **Contabo VPS(s) running k3s** (a lightweight, standard Kubernetes distribution) rather than a managed cloud K8s offering — one Helm chart per deployable unit (`api`, `scheduler`, `worker`, `frontend`) as below, HPA on `worker` pods keyed on event-bus queue depth. Because the manifests are standard Kubernetes, this can move to managed K8s (EKS/GKE/AKS) later without rewriting deployment config if hosting needs outgrow Contabo.
- **Database migrations**: `golang-migrate` or `atlas`, versioned per schema, run as a Helm pre-install/pre-upgrade hook.
- **Secrets**: External Secrets Operator or Sealed Secrets — cloud-agnostic, avoids locking to one cloud's secrets manager.
- **CI**: GitHub Actions — lint (`golangci-lint`, `eslint`), test, build multi-arch images, push to registry (GHCR — portable), Argo CD picks up new image tags per environment.

---

## 16. Phased roadmap

### Phase 0 — Foundations (pre-MVP, ~4–6 weeks)
- Repo scaffolding, CI/CD skeleton, Docker Compose dev environment
- Event bus + event log table (`world.events`) working end-to-end with a trivial event
- Auth (login/session) end-to-end, Nuxt ↔ Go
- `pkg/playergen`: nationality pool + name generation, unit-tested, seeded worlds of fake players
- Core schemas migrated (club, player, manager — minimal columns)
- Tick scheduler running, publishing `WORLD_TICK` events on a configurable cadence

**Exit criterion:** you can create a world, generate a club with a squad of realistically-named, realistically-nationalized players, and see a daily tick fire.

### Phase 1 — MVP (PRD section 74, ~3–4 months)
- Match engine (deterministic, seeded) + fixtures/league tables + promotion/relegation
- Squad management, tactics, training (simple mode only — section 73's "Simple" tier)
- Transfers: listings, bids, human-to-human negotiation, AI-club counterpart behavior (basic)
- Contracts, wages
- Finance: ledger-based accounts, cash vs budget distinction (sections 36/67) from day one — do not retrofit this
- Board: confidence scoring with explanation breakdowns, sackings
- Player dynamics: morale, playing-time tracking, basic transfer requests
- Social: manager profiles, messaging, human-to-human transfers, basic rivalry tracking
- Absence mode: policy-driven delegation for squad selection and transfer responses (this also gives you your first AI-club behavior for free, per section 10 of this plan)
- Home dashboard (urgent/important/interesting)
- PWA shell; email notifications for key events (bids, finances, match reports) with user-configurable preferences

**Exit criterion:** the core question from PRD section 85 is testable — "is managing a club among real managers more compelling than managing alone?"

### Phase 2 — V1 (PRD section 75, ~4–6 months)
- Academies: investment, output generation (reuses `playergen`, regionally parameterized), shutdown/reopen mechanics
- Full player personality + hidden variables + relationship graph (section 11/12/14) live
- Squad dynamics/dressing-room factions computed from the relationship graph (section 15)
- Agents as semi-autonomous actors influencing negotiations
- Injuries (types, severity, medical staff quality)
- Dynamic potential / player development model
- Manager-created competitions + competition reputation growth
- Media/news generation as a read-model over the event log
- Club DNA + archetypes fully modeled and driving AI-club decisions
- Supporters, ownership changes
- Anti-abuse: trade/collusion monitoring, commissioner tooling

### Phase 3 — V2 (PRD section 76, ~6+ months)
- Club creation flow (financing, league placement, constraints per section 43)
- Manager ownership path (buy club / become chairman)
- Custom competitions at scale, with abuse-prevention rules (min reputation, prize caps, approval flow)
- National team football, deeper international transfer markets
- Advanced relationship graph (national-team relationships, agent relationships as first-class graph edges)
- Retired player second careers (coach/scout/agent/pundit) — this is where `manager` schema and `player` schema start sharing a "person" identity concept worth formalizing (a `Person` entity that can hold multiple roles over time)
- Stadium development, advanced sponsorship
- **Architecture shift point**: this is the natural moment to evaluate splitting the highest-load engines (Match, Transfer) into independently deployed/scaled services via gRPC, since the interface boundaries were built in from Phase 0.

### Phase 4 — V3: Football Universe (PRD section 77, ongoing)
- Multiple inhabitable roles (sporting director, agent, journalist, scout, player) as additional "actor types" against the same event log and command-handler pattern already built for managers/PolicyBot
- Global football economy simulation
- Full analytics/premium tooling layer for monetization (section 78) — built last, deliberately, since it's additive and never gates competitive fairness

---

## 17. Team & sequencing notes

- Phase 0/1 can realistically be built by a small team (2–4 backend-leaning engineers + 1–2 frontend) if scope is held to exactly what's listed above — the biggest risk to timeline is scope creep into V1 features (agents, deep personalities) before the core loop is validated, which the PRD itself warns against in section 85.
- The **finance ledger and event bus are the two systems worth over-investing in early** relative to their apparent phase-1 scope, because every later phase (news, history, trust scores, replay, anti-cheat) is a read-model built on top of them. Getting the event schema right in Phase 0 avoids painful migrations later.
- The **player generation package** is small in scope but should be treated as a content task with its own timeline (sourcing/curating per-nationality name data), not just a code task — plan it in parallel with Phase 0 engineering, not sequentially after.

---

## 18. Open questions worth resolving before Phase 0 starts

1. ~~World sizing at MVP~~ — **Resolved: multiple parallel worlds from day one.** Every schema in section 6 carries a `world_id` from the first migration (not retrofitted), and all engines, the event log, and the tick scheduler are `world_id`-scoped — the scheduler runs cadences per world rather than globally, since worlds may be created/launched at different times. This also means the club-creation flow (Phase 3/V2, section 43) and world matchmaking/selection at signup need to be designed together in Phase 0, even though club creation itself ships later.
2. ~~Session model~~ — **Resolved: one manager, one club, at a time.** A manager holds at most one active club assignment across the whole platform. To take a second job, they must resign (or be sacked) and then either apply for a new job in the same world or move to a different world entirely — there's no simultaneous multi-club management. Reputation and history travel with the manager as a visible career record (section 42/61) regardless of which world they move to, but a manager's reputation earned in one world is **not factored into job-offer eligibility or board hiring decisions in a different world** — each world's job market evaluates a manager fresh, using only what that world's clubs/boards have actually seen them do. Practically: `manager_reputation` becomes a global, append-only history log for display, while any reputation *score* used in hiring logic (section 9's Job Security inputs, AI-club hiring decisions) is computed `world_id`-scoped.
3. ~~Match viewing fidelity~~ — **Resolved: text/event-feed match viewer for MVP**, in the style of a live commentary feed (Google's match-commentary UI is the reference point) — a scrolling, timestamped stream of key moments (goals, cards, chances, substitutions, half/full time) driven directly off the `MatchEvent` list from section 9, with no pitch graphics required. This keeps Phase 1 frontend scope to a WebSocket-driven list component rather than a canvas/SVG renderer. **A 2D pitch visualization is deferred to a later version** (candidate for V1/V2) — when it's built, it consumes the exact same `MatchEvent` stream, just rendered differently, so no backend or match-engine change is needed to add it later.
4. ~~Notification delivery~~ — **Resolved: email notifications for important events**, covering transfer bids/offers, club financial warnings, and match reports, with **user-configurable preferences** (per-category opt-in/opt-out and frequency, e.g. instant vs. daily digest) rather than a fixed notification set. This needs a `notification_preferences` table (per manager, per event category) and a dedicated **Notification service/worker** subscribed to the event bus, which checks preferences before dispatching — email via a transactional provider (e.g. SES/Postmark/SendGrid, kept swappable behind an interface for the cloud-agnostic requirement). Web Push (originally proposed alongside the PWA) can be added later as an additional delivery channel using the same preferences model, rather than being the sole channel at MVP.
5. ~~Hosting target~~ — **Resolved.** Initial development target is **local (laptop) via Docker Compose** — no Kubernetes needed yet for day-to-day work. **Postgres runs on Neon** from the start (serverless, zero ops, easy branching for dev/test), with a planned migration to a self-hosted Postgres instance once the team owns its own infrastructure. **Production hosting target is Contabo** (VPS-based) rather than a managed cloud provider, which points the Phase-1 infra toward **self-hosted k3s on a small number of Contabo VMs** instead of managed K8s (EKS/GKE/AKS) — the Helm charts and manifests in section 15 stay identical either way, since k3s is a standard K8s distribution; only the underlying VM provisioning changes. This should be reflected in section 15 as the concrete Phase-1 infra choice, with the broader cloud-agnostic setup (Terraform, managed K8s) remaining available as a later option if hosting needs outgrow Contabo.
