# The Touchline Book

*The single, consolidated reference for everyone who builds, extends, tunes or
operates Touchline — a persistent, asynchronous, multiplayer football-management
universe.*

This book replaces "read twenty scattered docs" with one linked narrative. Every
game concept has its own chapter; each chapter explains **what the concept is,
why it exists, how it is computed, where the code lives, and how it connects to
the rest of the world.** Cross-references are links — follow them.

> **Authority rule.** Code is the final truth. Where this book quotes a number,
> the constant it comes from is named so you can verify it. The per-slice design
> docs (`docs/design/*-numerics.md`, `backend/docs/design/*.md`) remain the
> detailed tuning ledgers; [Appendix A](appendix-a-source-index.md) maps every
> older document to the chapter that now covers it. When you change behaviour,
> update the chapter here **and** the ledger.

---

## How to read this book

- **New to the project?** Read Part I (chapters 1–3), then Chapter 4 (time) —
  almost every system is driven by the world clock.
- **Working on a feature?** Jump to its chapter; the "Connections" section at
  the end of each chapter lists every system it touches.
- **Tuning numbers?** Every chapter has a "Numbers" table naming the constants.
  Recalibration in Touchline is always a *data* edit (constants), never a change
  to steering logic.

## Conventions used throughout

| Convention | Meaning |
| --- | --- |
| `clamp(x, lo, hi)` | bound `x` to `[lo, hi]` |
| `round()` | mid-point rounding |
| Money | integer **pence** (`int64`) at service boundaries; `NUMERIC(14,2)` in `finance.*` columns; displayed as USD |
| "proposal" | an implemented number awaiting PM tuning sign-off |
| "approved" | a PM-signed number (mostly in the match engine) |
| `OPD-nn` | an entry in the Open/Resolved Product Decisions registry (`docs/product_manager.md`) |
| `IMnn` / `Snn-nn` | improvement tasks (`docs/tasks/improvements/`) / sprint tasks (`docs/tasks/`) |
| World day | one in-game calendar day; at the default clock scale, one real day |

---

## Contents

### Part I — Foundations
1. [Vision and product principles](01-vision-and-principles.md)
2. [Architecture and codebase map](02-architecture.md)
3. [The event spine, outbox and explanations](03-events-and-explanations.md)

### Part II — The world and its calendar
4. [The world clock, time and cadences](04-world-clock-and-time.md)
5. [Worlds: lifecycle, geography, setup and seeding](05-worlds-and-setup.md)
6. [Leagues, fixtures and scheduling](06-leagues-and-scheduling.md)
7. [Seasons, rollover and promotion/relegation](07-seasons-and-rollover.md)
8. [Cup competitions: domestic, regional and qualification](08-cups.md)

### Part III — Clubs and people
9. [Clubs: DNA, archetypes, reputation and supporters](09-clubs-dna-supporters.md)
10. [Players: attributes, overall, hidden traits and personality](10-players.md)
11. [Player lifecycle: pools, academy, intake and retirement](11-player-lifecycle-and-academy.md)

### Part IV — On the pitch
12. [Squad, tactics and lineups](12-squad-tactics-lineups.md)
13. [Training and player development](13-training-and-development.md)
14. [Condition and injuries](14-condition-and-injuries.md)
15. [The match engine (matchsim)](15-match-engine.md)
16. [Matchday orchestration and live matches](16-matchday-and-live-matches.md)
17. [Club form](17-form.md)

### Part V — The dressing room
18. [Morale, playing time and transfer requests](18-morale-and-transfer-requests.md)
19. [Dressing-room dynamics and factions](19-dressing-room.md)

### Part VI — The business of football
20. [Finance: ledger, budgets, wages and contracts](20-finance.md)
21. [The transfer market](21-transfer-market.md)

### Part VII — The manager's career
22. [Managers, careers and job offers](22-managers-and-job-offers.md)
23. [The board: match ratings, confidence, mandates and sacking](23-board-and-job-security.md)
24. [Social: trust, messaging and rivalries](24-social-and-rivalries.md)
25. [PolicyBot and absence mode](25-policybot-and-absence.md)

### Part VIII — Surfaces
26. [Dashboard, news, scouting and realtime](26-dashboard-news-scouting-realtime.md)
27. [Accounts, authentication and sessions](27-accounts-and-auth.md)
28. [The admin console](28-admin-console.md)

### Part IX — Building Touchline
29. [Engineering workflow: environment, verification and the improvement process](29-engineering-workflow.md)
30. [Roadmap, open decisions and known gaps](30-roadmap-and-open-decisions.md)

### Appendices
- [Appendix A — Source document index](appendix-a-source-index.md)
- Glossary: [`docs/how-to/glossary.md`](../how-to/glossary.md) remains the
  one-line-per-term index; this book is the long-form explanation behind it.

---

## The one-paragraph summary

A **world** is a self-contained football universe with its own **clock**. Every
real day the clock advances one **world day**; weekly and monthly boundaries are
derived from it. Admins declare **countries, leagues and cups**, seed **clubs**
(each with a **DNA** and a **board**) and **players** (each with visible
attributes, hidden traits and a personality). Human **managers** join a world by
accepting a **job offer** from an AI-run club. Fixtures kick off at real
wall-clock times and are simulated by a **deterministic match engine**, live,
minute by minute. Every result rates the manager, moves **supporter sentiment**,
**form**, **morale** and **rivalries**. Each month wages are posted from an
**append-only ledger** and the **board** reviews confidence — and may sack.
Absent managers are covered by **PolicyBot**. Everything that changes state is
an **event** carrying an **explanation**, so the game can always say *why*.
