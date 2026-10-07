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

---

## Reference Specs & Ledgers

- **Design Numerics Ledger**: [`docs/design/storyline-numerics.md`](../../design/storyline-numerics.md)
- **Touchline Book Reference**: [`docs/touchline-book/31-storylines-and-narratives.md`](../../touchline-book/31-storylines-and-narratives.md)
