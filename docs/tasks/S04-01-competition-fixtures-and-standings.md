# S04-01 — Implement MVP competition scheduling and standings

**Status:** Not started  
**Sprint:** 04 — Deterministic football competition  
**Source:** PRD §§30–32, 58, 74; technical plan §§6, 16  
**Depends on:** S03-02

## What to do

Build the MVP competition data and processing needed for persistent league fixtures, tables, promotion, and relegation. Use only approved initial league formats and rules.

## Acceptance criteria

- Fixtures, competitions/rules, matches, and standings are persisted world-scoped in the match/competition schemas.
- Fixture and standings processing is event-driven and auditable.
- Match results update a league table deterministically from the recorded result data.
- Seasonal promotion/relegation is supported by explicit competition rules; unspecified format details are not invented.
- Authorized API/UI surfaces can retrieve a competition, fixtures, and standings.

## Delivery evidence

- Pending.
