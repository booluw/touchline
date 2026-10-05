# Appendix A — Source document index

Where every pre-existing document's content now lives in this book. The
originals remain as detailed ledgers and history; when they disagree with this
book, check the code and fix whichever is wrong.

## Product & architecture

| Document | Covered in |
| --- | --- |
| `docs/Touchline — Persistent Multiplayer Football Manager PRD.md` | Ch. 1 (vision), and the "vision" notes in each system chapter |
| `docs/Touchline_Technical_Implementation_Plan.md` | Ch. 2, 3, 4, 15, 25, 30 |
| `docs/product_manager.md` (OPD registry, sprints, metrics) | Ch. 1, 30; OPDs cited per chapter |
| `docs/development.md` | Ch. 27, 29 |
| `AGENTS.md` | Ch. 2, 29 |

## How-to guides

| Document | Covered in |
| --- | --- |
| `docs/how-to/setup-and-launch.md` | Ch. 5, 29 |
| `docs/how-to/cadences-and-time.md` | Ch. 4, 6, 16 |
| `docs/how-to/seasons.md` | Ch. 6, 7 |
| `docs/how-to/cup-competitions.md` | Ch. 8 |
| `docs/how-to/job-offers-and-decisions.md` | Ch. 22 |
| `docs/how-to/transfer-market.md` | Ch. 21 |
| `docs/how-to/glossary.md` | all chapters (kept as the one-line index) |

## Design / numerics ledgers

| Document | Covered in |
| --- | --- |
| `docs/design/board-numerics.md` | Ch. 23 |
| `docs/design/finance-numerics.md` | Ch. 20 |
| `docs/design/transfer-numerics.md` | Ch. 21 |
| `docs/design/morale-numerics.md` | Ch. 18 |
| `docs/design/tactics-training-numerics.md` | Ch. 12, 13, 14 |
| `docs/design/academy-numerics.md` | Ch. 11 |
| `docs/design/policybot-numerics.md` | Ch. 25 |
| `docs/design/social-numerics.md` | Ch. 24 |
| `docs/design/dashboard-numerics.md` | Ch. 26 |
| `docs/design/derby-rivalry-determination.md` | Ch. 24, 16 |
| `backend/docs/design/development-numerics.md` | Ch. 13 |
| `backend/docs/design/injury-numerics.md` | Ch. 14 |
| `backend/docs/design/player-lifecycle.md` | Ch. 11 |
| `backend/docs/design/squad-dynamics-numerics.md` | Ch. 19 |
| `backend/docs/admin/how-to.md` | Ch. 28 |
| `pkg/matchsim/README.md` + addenda v1.1–v1.6 | Ch. 15 |

## Engineering reviews

| Document | Covered in |
| --- | --- |
| `docs/em/event-delivery-atomicity.md` | Ch. 3 (resolved by OPD-23) |
| `docs/em/api-event-publisher-wiring.md` | Ch. 3 |
| `docs/em/competition-event-spine.md` | Ch. 3, 7 |
| `docs/em/world-calendar-tick-semantics.md` | Ch. 4 (resolved by OPD-24) |
| `docs/em/outbox-repair-redelivery-retention.md` | Ch. 3, 30 (open risk) |
| `docs/em/review-context.md` | Ch. 3 |

## Tasks

| Document | Covered in |
| --- | --- |
| `docs/tasks/S*.md` | Ch. 30 (roadmap); delivered behaviour in each system chapter |
| `docs/tasks/improvements/IM01–IM37` | cited per chapter; status in Ch. 30 |
| `docs/tasks/improvements/competition-prize-pools-and-fairplay.md` | Ch. 8, 20, 30 (spec only) |

## Corrections made while consolidating

These older statements were found to be stale against the code and are stated
correctly in the book:

- **Supporter sentiment** no longer blends at the board review with α = 0.20
  (glossary §6); since IM33 it moves after every match with α = 0.08 (0.16 in
  a rivalry) and the review only reads it — Ch. 9, 23.
- **Board review** is monthly (IM02), not weekly as in OPD-03's original
  wording — Ch. 23.
- **Opening the board page** runs the same review function as the monthly pass,
  including mandate grading and the sacking guard — Ch. 23 §23.4.
- `expired` job offers **are** produced (accept cascade and the 7-day daily
  expiry), contrary to the "no producer today" notes in the glossary and the
  job-offers FAQ — Ch. 22.

---
[← Roadmap](30-roadmap-and-open-decisions.md) · [Contents](the-touchline-book.md)
