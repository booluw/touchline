# Chapter 30 — Roadmap, open decisions and known gaps

## 30.1 Delivery phases

The roadmap is 18 sprints (S00–S17) in five phases. Task files live in
`docs/tasks/`; check each file's status before assuming delivery.

| Phase | Goal | Sprints |
| --- | --- | --- |
| **0 — Foundations** | event spine, player generation, auth, scheduler, bootstrap | S00 governance · S01 world foundation & events · S02 auth, lifecycle, clock, WebSocket · S03 seeded world, vertical slice |
| **1 — MVP core loop** | competition, matches, controls, finance, market, board, social, absence, dashboard | S04 fixtures, engine, matchday feed · S05 tactics/training, ledger · S06 transfers, board, morale, social, PolicyBot · S07 dashboard, email, PWA, E2E/load |
| **2 — V1 depth & integrity** | academy, development, injuries, personality, dressing room, news, agents, DNA, anti-abuse | S08 · S09 · S10 · S11 golden replay & production launch |
| **3 — V2 ecosystem & scale** | club creation, ownership, national teams, Person entity, stadiums, gRPC, multi-world | S12 · S13 · S14 |
| **4 — V3 inhabitable universe** | multi-role actors (scout, journalist…), global economy, fair monetisation | S15 · S16 · S17 |

Alongside the sprints, the **improvement track** (`docs/tasks/improvements/`)
refines delivered systems. Implemented: IM01–IM30, IM33, IM35, IM37.
Not started: **IM31** (real-time world clock — retire `tick.day_length`),
**IM32** (integration-suite repair), **IM36** (trainable hidden attributes).
**IM34** (match pitch simulation) is planned on branch `feat/simulation`.
**Prize pools & fair play** is an approved specification (OPD-34), not built.

## 30.2 Open product decisions

| ID | Topic | Blocks |
| --- | --- | --- |
| OPD-02 | registration & account policy (age/privacy, verification, password reset) | public launch |
| OPD-06 | messaging moderation (reporting, blocking, bans) | social at scale |
| OPD-07 | email provider & consent | S07-02 |
| OPD-08 | SLA targets (P95, failover, autoscaling); rate limiting | S07-04, S11-02 |
| OPD-09 | ownership rules / reputation thresholds; real hiring market | S10, S12, S13 |
| OPD-10 | monetisation packaging (0% pay-to-win) | S16-02 |
| OPD-54 | password rules — **deferred; do not add rules** | — |

Resolved decisions (OPD-11 … OPD-60) are recorded in
`docs/product_manager.md` and cited throughout this book.

## 30.3 Known gaps and risks worth knowing

| Area | Gap | Chapter |
| --- | --- | --- |
| Board | opening the board page runs a full review that can **grade mandates and sack** mid-month; display is path-dependent | [23](23-board-and-job-security.md) |
| Board | DNA alignment shallow; supporters single bloc; alternatives ignore a candidate pool; PRD sack triggers beyond confidence absent | [23](23-board-and-job-security.md) |
| Clock | `tick.day_length` still configurable despite OPD-57 (IM31) | [4](04-world-clock-and-time.md) |
| Events | outbox repair keys on River job existence; River deletes completed jobs after retention → risk of re-enqueuing delivered history | [3](03-events-and-explanations.md) |
| Finance | no revenue, debt, instalments or statements | [20](20-finance.md) |
| Transfers | no agents, loans, release-clause triggers, anti-abuse monitoring | [21](21-transfer-market.md) |
| Cups | no automatic campaign per season; single-legged only; no prize money | [8](08-cups.md) |
| Dressing room | most management-action triggers not wired | [19](19-dressing-room.md) |
| Personality | many reaction actions have no live call sites | [10](10-players.md) |
| Development | hidden attributes not trainable (IM36); no individual training | [13](13-training-and-development.md) |
| Injuries | pitch condition neutral (no stadium state) | [14](14-condition-and-injuries.md) |
| Auth | no CSRF/rate limiting, no email verification/reset | [27](27-accounts-and-auth.md) |
| Tests | integration suite repair pending (IM32) | [29](29-engineering-workflow.md) |

## 30.4 Product guardrails for builders

- Ship only the current task's scope; schema hooks are not a mandate.
- Every state change → event; every score → explanation; every number →
  named constant.
- Never let money buy competitive advantage.
- Prefer deterministic, seeded behaviour wherever competition or money is
  involved.

## Connections

- Source: `docs/product_manager.md`, `docs/tasks/README.md`, `docs/tasks/improvements/*`, `docs/em/*`, Tech Plan §§16–18.

---
[← Engineering workflow](29-engineering-workflow.md) · [Contents](the-touchline-book.md) · [Next: Appendix A →](appendix-a-source-index.md)
