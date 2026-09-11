# S04-02 — Deliver the deterministic, pure match engine

**Status:** Not started  
**Sprint:** 04 — Deterministic football competition  
**Source:** PRD §§55–57, 63–64, 68, 74; technical plan §§1, 9, 14  
**Depends on:** S04-01, S01-03

## What to do

Implement the pure, seeded simulation contract that resolves a match from team inputs and tactics into an ordered result/event list. Integrate it through worker-driven fixture processing, not client execution.

## Acceptance criteria

- Match simulation has no database dependency and accepts a seed, squads, and tactics to return `MatchResult` and ordered `MatchEvent` values.
- Repeating a simulation with identical seed and inputs produces identical output.
- Match seeds and sufficient input/event context are persisted for replay/audit.
- Results update fixtures/standings through server-side event handling.
- Unit and golden replay tests protect deterministic behavior; product-owned tuning formulae are not fabricated without approval.

## Delivery evidence

- Pending.
