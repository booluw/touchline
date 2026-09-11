# S01-03 — Establish the explanation-object contract

**Status:** Not started  
**Sprint:** 01 — World foundation and event spine  
**Source:** PRD §§9, 16–18, 54, 81; technical plan §8  
**Depends on:** S01-02

## What to do

Make the existing explanation model the shared contract for scored or consequential decisions. Define its event/API serialization and the rendering boundary so UI and news consume stored reasons instead of recreating them.

## Acceptance criteria

- The common Explanation and factor types are stable, documented, and serializable in event payloads and state-changing API responses.
- A decision can identify its subject, score, and contributing labeled deltas.
- Explanations are persisted with their causing event; a consumer can render them without recalculating the decision.
- Tests demonstrate round-trip persistence/serialization for an explanation.
- No endpoint or client contract requires the client to derive why a server decision occurred.

## Delivery evidence

- Pending.
