# Chapter 2 — Architecture and codebase map

## 2.1 The shape of the system

Touchline is a **modular monolith**: one Go binary, many engine packages, one
PostgreSQL database split into per-engine schemas, a River (Postgres-native)
job queue as the event bus, Redis as ephemeral WebSocket fan-out, and a Nuxt 3
PWA client.

```
   Nuxt 3 PWA (Vue 3 + Pinia)
        │  REST/JSON  + one WebSocket (/ws)
        ▼
   ┌───────────────────── backend/cmd/touchline ─────────────────────┐
   │  serve (default) = api + scheduler + worker in one process       │
   │                                                                  │
   │  api        → internal/httpapi  (Gin router, handlers, /ws)      │
   │  scheduler  → internal/scheduler (cron → WORLD_TICK daily)       │
   │  worker     → pkg/eventbus EventWorker + live match runner       │
   │                                                                  │
   │  engines: world · competition · match · matchday · squad ·       │
   │  tactics · training · development · injury · player · form ·     │
   │  finance · transfer · board · manager · social · faction ·       │
   │  personality · academy · playerpool · lifecycle · policybot ·    │
   │  dashboard · scout · admin · auth · bootstrap · club             │
   └──────────────────────────────────────────────────────────────────┘
        │                        │                     │
        ▼                        ▼                     ▼
   PostgreSQL (authoritative)   River jobs          Redis pub/sub
   schemas per engine           (in Postgres)       (ephemeral, fail-soft)
```

### Process roles

`backend/cmd/touchline` takes one argument:

| Role | What it runs | Port |
| --- | --- | --- |
| `serve` (default) | API + scheduler + worker in one process | `:8080` |
| `api` | HTTP + WebSocket only | `:8080` |
| `scheduler` | the world clock (cron → `WORLD_TICK`) | `:8081` health |
| `worker` | event consumers, cadence passes, live match pacing | `:8082` health |

The game is only playable when **all three roles** run (API for commands,
scheduler for the clock, worker for consequences). Wiring lives in
`internal/app` (`config.go`, `app.go`, `runners.go`, `worker.go`,
`worldtick.go`). One backend image is deployed (IM29).

## 2.2 Repository layout

| Path | Contents |
| --- | --- |
| `backend/cmd/touchline` | the single binary (roles above) |
| `backend/cmd/user-create`, `backend/cmd/ref-seed` | dev account creation; reference-data seeding |
| `backend/internal/<pkg>` | all application code (one package per engine) |
| `backend/internal/httpapi` | Gin handlers (`<domain>_handlers.go`) + their integration tests, `router.go`, `ws.go` |
| `backend/internal/apidocs/openapi.yaml` | the OpenAPI 3.1 spec — **must mirror the router** |
| `backend/pkg/` | cross-cutting libraries: `eventbus`, `explanation`, `jwt`, `matchsim`, `playergen`, `realtime`, `apiref` |
| `backend/migrations/` | numbered `NNNN_name.{up,down}.sql` + a `README.md` matrix |
| `backend/data/`, `data/` | curated reference data (names, club names) |
| `frontend/` | Nuxt 3 app (pnpm workspace; source in `frontend/app/`) |
| `docs/` | product, design, how-to docs, tasks — and this book |
| `infra/` | compose overrides, CI |

## 2.3 Package responsibilities (backend/internal)

| Package | Owns | Chapter |
| --- | --- | --- |
| `app` | process wiring, `WORLD_TICK` dispatch, worker loop | [4](04-world-clock-and-time.md) |
| `world` | worlds, config keys, world clock, calendar, countries | [4](04-world-clock-and-time.md), [5](05-worlds-and-setup.md) |
| `scheduler` | per-world cron registry, `FireTick` | [4](04-world-clock-and-time.md) |
| `bootstrap` | world bootstrap, AI club generation, DNA/persona seeding | [5](05-worlds-and-setup.md), [9](09-clubs-dna-supporters.md) |
| `competition` | leagues, seeding, fixtures, pacing, weekdays, cups, regions, qualification, rollover, standings | [6](06-leagues-and-scheduling.md)–[8](08-cups.md) |
| `club` | club reads | [9](09-clubs-dna-supporters.md) |
| `player` | player reads, morale, playing time, transfer requests, dossier | [10](10-players.md), [18](18-morale-and-transfer-requests.md) |
| `playerpool`, `lifecycle`, `academy` | pools, drafting, signing, intake, retirement, auto-fill | [11](11-player-lifecycle-and-academy.md) |
| `squad` | ratings recipe, overall, lineup selection, morale/motivation/performance factors, eligibility | [10](10-players.md), [12](12-squad-tactics-lineups.md), [16](16-matchday-and-live-matches.md) |
| `tactics`, `training`, `development` | styles/formations/lineups; weekly training; development multipliers | [12](12-squad-tactics-lineups.md), [13](13-training-and-development.md) |
| `injury` | deterministic injury engine | [14](14-condition-and-injuries.md) |
| `match`, `matchday` | match orchestration, persistence, live pacing, kickoff runner | [16](16-matchday-and-live-matches.md) |
| `form` | club form EWMA | [17](17-form.md) |
| `personality`, `faction` | trait reactions & reveals; dressing-room graph | [10](10-players.md), [19](19-dressing-room.md) |
| `finance` | ledger, budgets, wages, contracts, summary | [20](20-finance.md) |
| `transfer`, `transfertest` | market, valuation, AI policy, completion | [21](21-transfer-market.md) |
| `manager` | managers, offers, careers, reputation | [22](22-managers-and-job-offers.md) |
| `board` | match ratings, monthly review, mandates, sacking, fan news | [23](23-board-and-job-security.md) |
| `social` | profiles, trust, messaging, rivalries | [24](24-social-and-rivalries.md) |
| `policybot` | absence detection & delegation | [25](25-policybot-and-absence.md) |
| `dashboard`, `scout` | home dashboard aggregator; opponent dossiers | [26](26-dashboard-news-scouting-realtime.md) |
| `auth` | login, register, sessions | [27](27-accounts-and-auth.md) |
| `admin` | admin read models (country dashboards, timeline, renames) | [28](28-admin-console.md) |
| `eventoutbox` | outbox repair sweep | [3](03-events-and-explanations.md) |
| `testdb` | integration-test harness | [29](29-engineering-workflow.md) |

### pkg/ libraries

| Package | Purpose |
| --- | --- |
| `pkg/eventbus` | `RiverBus.PublishTx` — the transactional outbox ([Ch. 3](03-events-and-explanations.md)) |
| `pkg/explanation` | the `Explanation` wire contract (`subject`, `score`, `factors[{label, delta}]`) |
| `pkg/matchsim` | the pure, deterministic match engine ([Ch. 15](15-match-engine.md)) |
| `pkg/playergen` | procedural players: names, nationalities, attributes, talent classes ([Ch. 10](10-players.md)) |
| `pkg/realtime` | the WebSocket broker (Redis or in-process) ([Ch. 26](26-dashboard-news-scouting-realtime.md)) |
| `pkg/jwt` | access/refresh token helpers ([Ch. 27](27-accounts-and-auth.md)) |

## 2.4 Data model at a glance

Postgres schemas mirror engine boundaries:

| Schema | Representative tables |
| --- | --- |
| `world` | `worlds`, `world_config`, `events`, `countries`, `regions`, `news_stories` |
| `auth` | `users`, `sessions` |
| `ref` | `nationalities`, `name_pool` (world-independent reference data) |
| `club` | `clubs`, `club_dna`, `boards`, `board_mandates`, `supporter_groups`, `rivalries`, `club_lineups`, `club_tactics`, `club_training_plans`, `academies`, `facilities`, `form_state` |
| `player` | `players`, `player_attributes` (EAV), `player_hidden_traits`, `player_personality`, `player_condition`, `contracts`, `player_appearances`, `injuries`, `player_development`, `player_attribute_changes`, `player_history`, `player_preferences`, `transfer_requests`, `promises` |
| `competition` | `competitions`, `competition_rules`, `club_competitions`, `seasons`, `competition_entries`, `standings`, `cup_qualification`, `manager_cup_choices` |
| `match` | `fixtures`, `matches`, `match_events`, `match_inputs` |
| `finance` | `accounts`, `ledger_entries`, `budgets`, `wage_commitments` |
| `transfer` | `listings`, `bids`, `negotiations`, `completed_transfers`, `clauses` |
| `manager` | `managers`, `job_offers`, `manager_history`, `manager_reputation_events`, `job_security_snapshots`, `match_ratings`, policies |
| `social` | `relationships` (polymorphic graph), `relationship_events`, `trust_events`, messages |
| `playerpool` | country free-agent pools |

Key invariants:

- **Postgres is authoritative.** Redis carries only realtime fan-out and is
  fail-soft (missing Redis → in-process broker).
- **World scoping.** Almost every row is reachable from a `world_id`; APIs
  derive the world from the caller's manager row, never from the token.
- **The relationship graph** (`social.relationships`) is polymorphic: player,
  manager and club nodes; edges carry `relationship_type`, `strength`,
  `trust`, `sentiment`. It powers rivalries ([Ch. 24](24-social-and-rivalries.md)),
  player↔manager memory ([Ch. 18](18-morale-and-transfer-requests.md)) and
  the dressing room ([Ch. 19](19-dressing-room.md)).
- **Migrations are append-only**: never renumber an existing file; every
  change adds a row to `backend/migrations/README.md`.

## 2.5 Request flow example — placing a transfer bid

1. Browser `POST /api/transfers/bids` with httpOnly access cookie.
2. `requireAuth` middleware validates the JWT; `TouchActivity` records the
   heartbeat ([Ch. 25](25-policybot-and-absence.md)).
3. The handler resolves the manager's world and club, calls
   `transfer.Service`.
4. In **one transaction**: validate rules, insert the bid, `PublishTx` a
   `BID_PLACED` event (+ River job).
5. The worker consumes the job: pushes an urgent dashboard update to the
   seller's human manager over Redis → `/ws`.
6. The handler returns the bid with its `Explanation`.

## Connections

- Events and the outbox: [Chapter 3](03-events-and-explanations.md).
- Verification gates (OpenAPI parity tests, integration tags): [Chapter 29](29-engineering-workflow.md).
- Source: `docs/Touchline_Technical_Implementation_Plan.md` §§2–6, `docs/development.md`, `AGENTS.md`.

---
[← Vision](01-vision-and-principles.md) · [Contents](the-touchline-book.md) · [Next: Events →](03-events-and-explanations.md)
