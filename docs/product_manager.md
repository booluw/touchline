# Touchline Product Management Strategy & Handoff Document

## Executive Product Mandate

Touchline is a browser-based, asynchronous, persistent multiplayer football-management universe. The product prioritizes social competition, persistent consequences, explainable systems, meaningful financial trade-offs, and enduring world history.

The primary product thesis is:
> **Every club has a personality. Every player has a story. Every decision has consequences.**

The MVP core validation question (PRD §85) is:
> **"Is managing a club in a world populated by other real managers more compelling than managing a club alone?"**

The PRD (`docs/Touchline — Persistent Multiplayer Football Manager PRD.md`) and Technical Plan (`docs/Touchline_Technical_Implementation_Plan.md`) serve as the authoritative sources of truth. `OPENCODE.md` records resolved architectural decisions and domain invariants. This document acts as the product strategy manifest, sprint breakdown directory, open-decision registry, and team handoff guide.

---

## Delivery Posture & Technical Invariants

- **Strict Phasing:** Ship scope strictly according to the task manifests in `docs/tasks/`. Do not pull V1+ features into Phase 1 MVP merely because schema hooks exist.
- **Server Authority:** The server is the sole source of truth. The Nuxt 3 client renders state and submits commands; it never simulates outcomes (OPENCODE.md, Tech Plan §1).
- **Append-Only Ledger:** Financial balance is strictly `SUM(ledger_entries)`. No stored `balance` column exists in `finance` tables (OPENCODE.md, Tech Plan §6).
- **Event Spine:** All state changes emit typed `world.events` carrying `world_id`, tick counters, actor IDs, causal chains (`CausedBy`), and typed JSON payloads (Tech Plan §4).
- **Explainability:** All scored outcomes (board confidence, transfer desire, job security) return `Explanation` objects with explicit factor breakdowns (Tech Plan §8).
- **Relationship Graph:** Modeling entity relationships as polymorphic graph edges in `social.relationships` (`player`, `manager`, `club`) (Tech Plan §6).
- **Unified PolicyBot Execution:** Absence delegation and AI-controlled clubs invoke the exact same command handlers a human manager calls (Tech Plan §10).
- **Competitive Fairness:** Strict non-pay-to-win policy. Real-money purchases cannot grant competitive or financial simulation advantages (PRD §78).

---

## Complete Application Sprint & Task Breakdown

The delivery roadmap translates the PRD and Technical Implementation Plan into 18 structured sprints (`S00` through `S17`) across 5 major development phases:

### Phase 0 — Foundations & Control Plane (Pre-MVP)
*Goal: Establish core engine scaffolding, event bus, player generation, auth, and world tick scheduler.*

- **Sprint 00 — Product control plane**
  - [`S00-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S00-01-product-delivery-governance.md): Establish product delivery governance.
- **Sprint 01 — World foundation and event spine**
  - [`S01-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S01-01-core-migrations-and-world-scoping.md): Create core schemas and world-scoped migrations.
  - [`S01-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S01-02-event-log-and-river-round-trip.md): Wire `world.events` and river event-bus queue.
  - [`S01-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S01-03-explanation-contract.md): Implement standard `Explanation` object contracts.
  - [`S01-04`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S01-04-player-generation-data-and-seeding.md): Curate `pkg/playergen` name datasets and weighted nationality pool.
  - [`S01-05`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S01-05-local-development-and-environment-contract.md): Establish Neon Postgres and local dev environment contracts.
- **Sprint 02 — Authenticated, schedulable worlds**
  - [`S02-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S02-01-gin-api-auth-and-session-flow.md): Build Gin REST router, JWT authentication, and session flows.
  - [`S02-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S02-02-world-lifecycle-and-club-assignment-contract.md): Implement world lifecycle and single-club manager assignment rules.
  - [`S02-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S02-03-configurable-world-clock.md): Build configurable world tick scheduler emitting `WORLD_TICK` events.
  - [`S02-04`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S02-04-redis-and-single-websocket-foundation.md): Implement Redis session store and single multiplexed WebSocket foundation (`useSocket`).
- **Sprint 03 — Seeded playable world bootstrap**
  - [`S03-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S03-01-world-bootstrap-and-first-squad.md): Produce seeded world, club, and procedurally generated first squad.
  - [`S03-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S03-02-phase-zero-vertical-slice.md): Validate Phase-0 vertical slice end-to-end.

---

### Phase 1 — MVP Core Loop
*Goal: Deliver playable persistent multiplayer competition, transfer market, board consequences, and dashboard.*

- **Sprint 04 — Deterministic football competition**
  - [`S04-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S04-01-competition-fixtures-and-standings.md): Implement competition scheduling, fixtures, and standings.
  - [`S04-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S04-02-deterministic-match-engine.md): Build pure deterministic match simulation engine (`Simulate`).
  - [`S04-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S04-03-mvp-matchday-event-feed.md): Build text/event-feed matchday commentary viewer over WebSockets.
- **Sprint 05 — Manager controls and finance**
  - [`S05-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S05-01-squad-tactics-and-simple-training.md): Implement squad selection, tactics management, and Simple training mode.
  - [`S05-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S05-02-ledger-finance-and-contract-wages.md): Implement append-only ledger finance, cash vs budget, and wage commitments.
- **Sprint 06 — Multiplayer market and board consequences**
  - [`S06-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S06-01-transfers-listings-bids-and-negotiation.md): Build transfer engine, listings, cash/clause bids, and human negotiation workflow.
  - [`S06-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S06-02-board-confidence-mandates-and-sackings.md): Implement structured board mandates, confidence scoring, and firing events.
  - [`S06-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S06-03-player-morale-playing-time-and-transfer-requests.md): Model player morale, playing time promises, and transfer requests.
  - [`S06-04`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S06-04-social-messaging-manager-profiles-and-rivalries.md): Build manager profiles, direct messaging, and relationship graph foundations.
  - [`S06-05`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S06-05-absence-mode-and-policybot.md): Implement policy-driven absence delegation and PolicyBot fallback execution.
- **Sprint 07 — MVP experience, operations, and validation**
  - [`S07-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S07-01-home-dashboard-aggregator.md): Build `GET /api/dashboard` aggregator and Nuxt 3 dashboard UI.
  - [`S07-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S07-02-email-notifications-and-preferences.md): Build notification preferences and transactional email worker.
  - [`S07-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S07-03-pwa-shell-and-offline-resilience.md): Configure `@vite-pwa/nuxt` module, service worker, and offline shell caching.
  - [`S07-04`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S07-04-mvp-e2e-validation-and-load-testing.md): Execute E2E flow validation and k6 load testing suite.

---

### Phase 2 — V1 Depth & Integrity
*Goal: Introduce youth academies, rich player personalities, squad dynamics, news generation, agents, and anti-abuse.*

- **Sprint 08 — Academies and player development**
  - [`S08-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S08-01-academy-investment-and-procedural-intake.md): Implement academy investment tiers and procedural youth intake.
  - [`S08-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S08-02-dynamic-potential-and-player-development.md): Implement dynamic potential growth and age-curve player development.
  - [`S08-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S08-03-injuries-medical-staff-and-recovery.md): Implement injury simulation, medical staff quality, and rehabilitation.
- **Sprint 09 — Relationship-driven football world**
  - [`S09-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S09-01-full-player-personality-and-hidden-traits.md): Implement full player personality archetype system and hidden traits.
  - [`S09-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S09-02-dressing-room-factions-and-squad-dynamics.md): Implement graph-derived squad dynamics and dressing room factions.
  - [`S09-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S09-03-news-generator-and-media-read-model.md): Build automated news generator and media read-model over event log.
- **Sprint 10 — Agents, competitions, identity, and ownership**
  - [`S10-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S10-01-agents-and-semi-autonomous-negotiations.md): Implement player agents and semi-autonomous contract negotiations.
  - [`S10-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S10-02-manager-created-competitions-and-reputation.md): Implement manager-created competitions and reputation growth.
  - [`S10-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S10-03-club-dna-and-supporters.md): Implement club DNA archetypes and supporter group mechanics.
  - [`S10-04`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S10-04-anti-abuse-trade-monitoring-and-commissioner-tooling.md): Implement anti-abuse trade monitoring and commissioner adjudication tooling.
- **Sprint 11 — V1 integrity and launch review**
  - [`S11-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S11-01-v1-golden-replay-and-integrity-verification.md): Build V1 golden replay test suites and financial ledger property tests.
  - [`S11-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S11-02-v1-production-deployment-and-launch.md): Finalize production Helm charts, k3s Contabo cluster, and V1 launch.

---

### Phase 3 — V2 Ecosystem & Scale
*Goal: Enable club creation, manager ownership path, national teams, retired player careers, and gRPC service extraction.*

- **Sprint 12 — Club creation and ownership path**
  - [`S12-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S12-01-club-creation-flow-and-league-placement.md): Implement club creation flow, financial backing validation, and lower-tier placement.
  - [`S12-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S12-02-manager-ownership-and-chairman-transition.md): Implement manager-to-owner transition and chairman authority mechanics.
- **Sprint 13 — International world and careers**
  - [`S13-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S13-01-national-team-football-and-international-competitions.md): Implement national team football, international call-ups, and tournaments.
  - [`S13-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S13-02-person-entity-and-retired-player-careers.md): Refactor player/manager identity into Person entity for retired player careers.
  - [`S13-03`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S13-03-stadium-development-and-advanced-sponsorship.md): Implement stadium expansion projects and commercial sponsorship deals.
- **Sprint 14 — V2 infrastructure and world expansion**
  - [`S14-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S14-01-grpc-inter-engine-extraction.md): Extract Match and Transfer engines into standalone gRPC microservices.
  - [`S14-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S14-02-multi-world-scaling-and-world-matchmaking.md): Implement multi-world matchmaking, cross-world reputation, and world scaling.

---

### Phase 4 — V3 Inhabitable Universe
*Goal: Unlock multi-role gameplay (scouts, journalists, agents, directors), global economy, and fair monetization.*

- **Sprint 15 — Multi-role universe foundation**
  - [`S15-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S15-01-multi-role-actor-framework.md): Implement multi-role actor framework for inhabitable universe roles.
  - [`S15-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S15-02-scout-and-journalist-inhabitable-roles.md): Implement specialized gameplay interfaces for Scout and Journalist roles.
- **Sprint 16 — Global economy and fair monetization**
  - [`S16-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S16-01-global-football-economy-simulation.md): Implement global macro-economic simulation and TV rights distribution.
  - [`S16-02`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S16-02-cosmetic-and-analytics-monetization.md): Implement premium analytics suite, cosmetic customizations, and fairness verification.
- **Sprint 17 — V3 launch and ongoing governance**
  - [`S17-01`](file:///Users/bfree/Desktop/booluw/touchline/docs/tasks/S17-01-v3-universe-launch-and-ongoing-governance.md): Execute V3 universe launch and establish ongoing community governance.

---

## Open Product Decisions Registry

These decisions are deliberately open in the source specifications. Engineering teams must consume configuration parameters rather than hardcoding arbitrary assumptions. The PM must resolve these with stakeholders as sprint dependencies approach:

| Decision ID | Open Topic | Impacted Sprints | Governing Resolution Standard |
|---|---|---|---|
| **OPD-01** | World Bootstrap & Regional Formats | S02, S03, S04 | Define initial league size (e.g. 10/12/18 teams), promotion/relegation numbers, and tier structures. |
| **OPD-02** | Registration & Account Policy | S02-01 | Define age/privacy requirements, identity verification thresholds, and password reset flows. |
| **OPD-03** | Simulation Formulae & Tuning | S04, S05, S06 | Standardize mathematical weights for board job security, injury probability curves, and morale decay rates. |
| **OPD-04** | MVP Tactical Boundaries | S05-01 | Specify exact initial tactical input controls for Simple mode (e.g. formation, mentality, pressing intensity). |
| **OPD-05** | Transfer Workflow Rules | S06-01 | Set bid expiration windows (e.g. 24h / 48h world ticks) and valid clause structures. |
| **OPD-06** | Messaging Moderation Policy | S06-04 | Establish reporting/blocking mechanics, text moderation filters, and commissioner ban policies. |
| **OPD-07** | Email Provider & Consent | S07-02 | Select transactional provider (SES/Postmark/SendGrid), sender domain identity, and GDPR consent policy. |
| **OPD-08** | Operational SLA Targets | S07-04, S11-02 | Set P95 API response thresholds (<200ms), DB failover objectives, and cluster autoscaling parameters. |
| **OPD-09** | V1/V2 Ownership Rules | S10, S12, S13 | Define minimum manager reputation required to create custom competitions or purchase club equity. |
| **OPD-10** | Monetization Packaging | S16-02 | Pricing tiers for premium analytical dashboards and cosmetic cosmetics, enforcing 0% pay-to-win. |

### Resolved OPD entries

| Decision ID | Resolution | Resolved by |
|---|---|---|
| **OPD-11** (topics doc `world.nationality_pool`) | The weighted nationality distribution pool lives in the `ref` schema — `ref.nationalities.generation_weight` plus `ref.name_pool` — not `world.nationality_pool`. Nationality/name data is identical across parallel worlds, so it is non-world-scoped by design (migrations `0002_ref`, `backend/migrations/README.md`). Tech plan §7 and the Phase-0 handoff notes used the name `world.nationality_pool`; that wording is superseded. `pkg/playergen` consumes the same weights regardless of table name (S01-04). | S01-01 |
| **OPD-12** (technical plan §8 / PRD §54 explainability contract) | The explanation contract lives in `pkg/explanation` (cross-cutting, not `internal/world`). The wire shape is pinned as `{"subject","score","factors":[{"label","delta"}]}` with all fields always emitted; additive-only changes. `Factors` are the authoritative reason and are **not** required to sum to `Score` (narrative "why" cases such as an AI club's bid carry no numeric total); an opt-in `Validate()` reports a summed-deltas mismatch only when a producer calls it. Explanations persist with their causing event in `world.events.explanation` (JSONB); consumers render stored reasons (reference `Render()` in the package) and never recalculate. State-changing API endpoints (S02+) must return the `Explanation` directly. | S01-03 |
| **OPD-13** (S01-04 data home, coverage, and player-persistence timing) | (**1) Data home + ingestion:** curated `data/names/<code>.json` files are the versioned, license-attributed record of truth; `cmd/ref-seed` ingests them idempotently into `ref.nationalities` + `ref.name_pool` (upsert nationalities, delete+reinsert name pool per code, one transaction; compose `ref-seed` service after migrations; CI runs it twice asserting identical output). Runtime squad generation reads the DB pools; `pkg/playergen` stays DB-free. (**2) Coverage:** 21 nationalities (README's 15 + colombia, uruguay, ghana, croatia, scotland, ivory_coast) with `generation_weight` values per the README table — tuning is data, not code. **Deferred:** `attribute_bias` and single-name/`shirt_name` rules (no `shirt_name` column yet). (**3) Codes:** lowercase 2–3 letter slugs; `eng`/`sco` are project-reserved non-ISO codes for the football home nations (documented in `ref.nationalities.code` guidance and `data/names/README.md`). (**4) Player-persistence timing — product decision:** players are **not** persisted by S01-04. Generated players (`GeneratedPlayer`) are written as `person.people` + `player.players` rows only when a team is created or the first season starts (fresh-slate worlds; S02-02/S03-01), so S01-04 touches reference data only and never game entities. | S01-04 |
| **OPD-14** (developer-environment contract) | **Postgres is external by default:** the base `docker-compose.yml` defines no Postgres service — it runs on Neon and the connection string is brought by the developer (`DATABASE_URL`). A hermetic Postgres is an **opt-in override** (`infra/ci/docker-compose.postgres.yml`, postgres:16) used by CI's `compose` job and available locally. **Env contract:** a repo-root `.env` (template `.env.example`) drives compose; the merged values (`DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`, TTLs, ports, `ENV`, `NUXT_PUBLIC_API_BASE`) are mirrored in `backend/.env.example` for bare `go run`/migrate runs — the two templates are kept in sync. Neon gotchas codified: unpooled endpoint (strip `-pooler`), scheme `postgres://` (golang-migrate/river reject `postgresql://`), strip surrounding quotes. **Health contract:** every backend binary fails fast with an actionable error when `DATABASE_URL` is missing/unreachable and exposes an HTTP liveness check — api `:8080/health` + `:8080/health/db` (pings Postgres, 503 when down), scheduler `:8081/health`, worker `:8082/health` — used by compose healthchecks and the CI smoke test. **Bootstrap order:** migrations → ref-seed → api/scheduler/worker → frontend (compose `depends_on`, one-shots use `service_completed_successfully`). **CI verification:** the `compose` job validates `docker compose config -q` with placeholder env and boots the full stack against the Postgres override, polling `/health/db` (up to ~10 min) then asserting frontend on :3000. Walkthrough: `docs/development.md`. | S01-05 |
| **OPD-15** (accounts, worlds, jobs, and sessions) | **(1) One account ⇒ many worlds:** `auth.users` is account-level; membership is one `manager.managers` row per (user, world). `uq_managers_user_world` enforces at most one row per user per world. **(2) One job per user, not per world:** a user holds at most one global job (`uq_manager_one_job_per_user` — partial unique index where `status='active' AND current_club_id IS NOT NULL`). No multi-job tie-break scenario exists. **(3) JWTs carry no `world_id`:** access token claims are `sub`=manager, `user_id`, `jti`, `exp`, `iat`, `type=access`; refresh is `sub`=manager, `jti`, `exp`, `iat`, `type=refresh`. The active world is *derived from the manager row* by manager id, never minted into a token (a `jti` also guarantees every token is unique so rotation never collides on `auth.sessions.refresh_token_hash`). **(4) Login world resolution:** (a) the world where the user has a job always wins; (b) a jobless user may pick explicitly via `world_id` — rejected with 403 if not a member; (c) jobless with exactly one active world → that world; (d) jobless across *multiple* active worlds → `{"status":"worlds","worlds":[...]}` (no session yet) and the client re-posts with `world_id`; (e) else a single manager row → its world; (f) else a world list; (g) no manager rows → 403 `ErrNoManager` (account exists, no world joined). **(5) Sessions rotate + revoke:** refresh stores only a SHA-256 hash in `auth.sessions` (with `ip inet`, device fingerprint, `expires_at`, `revoked_at`); every refresh revokes the old row and issues a new session pinned to the same manager. **(6) Registration/recovery/email verification remain OPEN (OPD-02):** `cmd/user-create` is a developer bootstrap only (one admin/importer account), never the product signup. **(7) Non-goals this sprint:** no auth events, no rate limiting (S02-04/S11), no formal CSRF policy (httpOnly + SameSite=Lax covers dev/localhost; revisit when cross-site deployments land), "last world visited" not persisted — world resolution defaults as in (4). | S02-01 |

---

## Product Metrics & Stage Gates

### North Star Metric
- **Meaningful Manager Decisions per Active Manager per Week (MMD/AMW)**

### Key Performance Indicators (KPIs)
- **Retention:** D1, D7, D30, and D90 active manager retention cohorts.
- **Engagement:** Active human-vs-human fixtures completed, transfer negotiations conducted, social messages exchanged, and rivalries forged.
- **Universe Depth:** Average manager tenure per club, seasons completed, academy prospects graduated, and retired player career transitions.

---

## PM Handoff Checklist for Next Product Owner

1. **Review Context:** Read `OPENCODE.md`, `docs/Touchline — Persistent Multiplayer Football Manager PRD.md`, `docs/Touchline_Technical_Implementation_Plan.md`, and this file (`docs/product_manager.md`).
2. **Backlog Integrity:** Ensure every task executed in `docs/tasks/` maps to a sprint, has clear acceptance criteria, and records implementation evidence upon completion.
3. **Resolve Open Decisions:** Coordinate stakeholder sign-off on Open Decisions (OPD-01 to OPD-10) before work starts on dependent sprints.
4. **Scope Control:** Reject feature creep into Phase 1 MVP that belongs in V1 (Phase 2) or V2 (Phase 3).
5. **Verify Non-Negotiables:** Validate that all state-changing features write to `world.events`, maintain ledger integrity, return `Explanation` objects, and run server-authoritatively.
