# Sprint 31 — Storyline Engine & Emergent Narrative Backlog

This directory contains the implementation tasks for **Sprint 31 (Storyline & Emergent Narrative Engine)**.

---

## Working Rules & Invariants

- **Server Authority**: All storyline pattern matching, dilemma generation, and consequence scoring run on the backend Go server (`internal/storyline`). The frontend renders state and submits choices.
- **Event Spine Integration**: Story arc state transitions emit typed `world.events` carrying JSON `Explanation` payloads.
- **Async-First Execution**: Dilemmas appear as urgent dashboard items (`GET /api/dashboard`). If un-responded before tick expiration, PolicyBot executes default staff stance.
- **No Pay-To-Win**: Storyline choices carry purely tactical/emotional/financial trade-offs; real money cannot influence dilemma generation or outcomes.
- **Strict Verification**: Every task requires unit tests, `go build ./...`, `go vet ./...`, compile-gate integration tests, and schema test compliance (`TestDocsCoverRouter`, `TestDocsOpenAPIValid`).

---

## Task Map

| Task ID | Component | Description | Depends On | Status |
| --- | --- | --- | --- | --- |
| [`STORY-01`](STORY-01-event-pattern-evaluator-and-schema.md) | Schema & Engine Scaffolding | Postgres migration (`story.*`), event spine listener, evaluator runner | S01 event spine | Not started |
| [`STORY-02`](STORY-02-narrative-arc-archetypes.md) | Arc State Machines | Implement 5 initial arc archetypes & trigger pattern matchers | STORY-01 | Not started |
| [`STORY-03`](STORY-03-crossroad-dilemmas-and-dashboard.md) | Dilemma & Dashboard | Dilemma generator, option resolution, dashboard items, PolicyBot fallback | STORY-02, S07-01 | Not started |
| [`STORY-04`](STORY-04-pre-match-mind-games-and-press.md) | Mind Games & Press | Tactical pre-match stances, rivalry press banter, referee/card modifiers | STORY-02, S06-04 | Not started |
| [`STORY-05`](STORY-05-season-storybook-and-career-chronicle.md) | Season Chronicles | Season storybook recap synthesis, manager career chronicles & profile API | STORY-01, S07-01 | Not started |
| [`STORY-06`](STORY-06-financial-audits-and-115-charges.md) | Financial Audits & Charges | 3-season PSR audits, creative accounting dilemmas, independent commission trials, points deductions | STORY-01, S05-02 | Not started |
| [`STORY-07`](STORY-07-player-actor-career-mode.md) | User-as-Player Career Mode | Player actor profile creation, position-locked match ratings, human manager vs human player hooks | STORY-01, S15-01 | Not started |
| [`STORY-08`](STORY-08-player-off-pitch-actions-and-agent.md) | Player Off-Pitch Actions | Hiring/firing agents, contract demands, transfer requests, social media statements, personal lifestyle | STORY-07, S06-04 | Not started |
| [`STORY-09`](STORY-09-player-matchday-actions-and-tactics.md) | Player Matchday Actions | Pre-match goals, tactical role requests, in-match call for ball, ref arguments, goal celebrations, press quotes | STORY-07, STORY-04 | Not started |
| [`STORY-10`](STORY-10-player-career-progression-and-awards.md) | Player Career Progression | International call-ups & caps, Golden Boot/Ballon d'Or awards, aging curve, post-retirement transition to Manager/Scout | STORY-07, S13-01 | Not started |
| [`STORY-11`](STORY-11-retired-player-to-manager-archetype.md) | Retired Player Manager Arc | Former-club hiring affinity, tactical DNA inheritance, prodigal_son_manager_return arc (Guardiola/Alonso/Fàbregas path) | STORY-01, STORY-10 | Not started |

---

## Reference Specs & Ledgers

- **Design Numerics Ledger**: [`docs/design/storyline-numerics.md`](../../design/storyline-numerics.md)
- **Financial Sustainability & FFP Ledger**: [`docs/design/financial-sustainability-and-ffp.md`](../../design/financial-sustainability-and-ffp.md)
- **User-as-Player Career Mode Ledger**: [`docs/design/player-career-mode-numerics.md`](../../design/player-career-mode-numerics.md)
- **Retired Player Manager Archetype Ledger**: [`docs/design/retired-player-manager-archetype.md`](../../design/retired-player-manager-archetype.md)
- **Touchline Book Reference**: [`docs/touchline-book/31-storylines-and-narratives.md`](../../touchline-book/31-storylines-and-narratives.md)
