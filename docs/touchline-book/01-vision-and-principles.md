# Chapter 1 — Vision and product principles

> *"Every club has a personality. Every player has a story. Every decision has
> consequences."* — the Touchline product thesis

## 1.1 What Touchline is

Touchline is a **browser-based, asynchronous, persistent multiplayer
football-management universe**. Many human managers share one **world** (see
[Chapter 5](05-worlds-and-setup.md)); the world never pauses for any one of
them. Matches kick off at real times, money is spent month by month, boards
lose patience, players sulk, rivals remember. A manager who logs off is covered
by an assistant ([PolicyBot, Chapter 25](25-policybot-and-absence.md)) so the
world keeps moving.

The MVP validation question (PRD §85) is:

> **Is managing a club in a world populated by other real managers more
> compelling than managing a club alone?**

The north-star metric is **Meaningful Manager Decisions per Active Manager per
Week (MMD/AMW)**. Supporting KPIs: D1/D7/D30/D90 retention; human-vs-human
fixtures, negotiations, messages and rivalries; universe depth (tenure,
seasons completed, academy graduates, retired-player careers).

## 1.2 Target users

| Segment | Wants |
| --- | --- |
| **Competitive managers** (primary) | win against other humans; transparent, fair, deterministic systems |
| **Football roleplayers** (secondary) | a club with identity, players with stories, a career with history |
| **Social communities** (tertiary) | rivalries, messaging, shared world events |

## 1.3 The core loop (PRD §3)

```
Observe → Decide → Act → Simulate → React → Adapt
```

- **Observe** — dashboard, news, standings, scouting ([Ch. 26](26-dashboard-news-scouting-realtime.md)).
- **Decide / Act** — lineups and tactics ([Ch. 12](12-squad-tactics-lineups.md)), training ([Ch. 13](13-training-and-development.md)), transfers ([Ch. 21](21-transfer-market.md)), board negotiation ([Ch. 23](23-board-and-job-security.md)).
- **Simulate** — the match engine ([Ch. 15](15-match-engine.md)) and the cadence passes ([Ch. 4](04-world-clock-and-time.md)).
- **React** — morale, sentiment, form, board ratings, rivalries.
- **Adapt** — the manager changes course; consequences persist.

Manager actions come in three flavours (PRD §4): **immediate** (e.g. respond to
a bid), **queued** (e.g. a training plan takes effect at the next week
boundary), and **deadline-based** (e.g. a lineup locks once the fixture goes
live).

## 1.4 Non-negotiable product & engineering principles

These come from the Technical Plan §1 and the PM mandate in
`docs/product_manager.md`. Every chapter of this book is an application of
them.

1. **Server authority.** The client renders state and submits commands; it
   never simulates outcomes. All game maths lives in Go on the server.
2. **Modular monolith first.** Engines are Go packages with hard boundaries
   and their own Postgres schemas, compiled into one binary
   ([Chapter 2](02-architecture.md)). Extraction to services later is a
   transport change, not a rewrite.
3. **Everything is an event.** Every meaningful state change writes a typed
   `world.events` row in the same transaction
   ([Chapter 3](03-events-and-explanations.md)).
4. **Determinism where competition or money is at stake.** Match outcomes,
   draws, intakes and AI decisions are seeded and replayable
   ([Chapter 15](15-match-engine.md)).
5. **Explainability.** Every scored outcome carries an `Explanation` whose
   factors say *why*; clients render stored explanations and never recompute
   them.
6. **Append-only money.** There is no balance column anywhere; cash is
   `SUM(ledger_entries)` ([Chapter 20](20-finance.md)).
7. **Async-first UX.** Nothing requires two managers to be online at once.
8. **Unified PolicyBot execution.** AI and absence delegation invoke the
   *same* command handlers a human calls ([Chapter 25](25-policybot-and-absence.md)).
9. **No pay-to-win.** Real money can never buy competitive or financial
   advantage (PRD §78; [Chapter 30](30-roadmap-and-open-decisions.md)).
10. **Tuning is data.** Every decision number is a named constant (often
    marked *proposal*); recalibration edits constants, never steering logic.
11. **Strict phasing.** Ship only the scope of the current task manifest;
    schema hooks for later features are not an invitation to build them.

## 1.5 The world, in one diagram

```
                ┌──────────────── World (clock, config, seed) ───────────────┐
                │                                                            │
   Regions ──► Countries ──► Leagues (tiers) ──► Seasons ──► Fixtures ──► Matches
                │                 │                                 │
                │               Cups (domestic / regional)          ▼
                │                                         ratings · form · morale
   Clubs (DNA, board, supporters, finance, academy, squad)   sentiment · rivalries
                │                                                   │
   Players (attributes, hidden traits, personality, condition)     ▼
                │                                     monthly board review
   Managers (human / PolicyBot) ◄── job offers ◄── AI clubs    wages · sackings
```

## Connections

- The *why* behind specific numbers lives in each system chapter.
- The phased roadmap that orders delivery is in [Chapter 30](30-roadmap-and-open-decisions.md).
- Source: `docs/Touchline — Persistent Multiplayer Football Manager PRD.md`,
  `docs/product_manager.md`, `docs/Touchline_Technical_Implementation_Plan.md` §1.

---
[Contents](the-touchline-book.md) · [Next: Architecture →](02-architecture.md)
