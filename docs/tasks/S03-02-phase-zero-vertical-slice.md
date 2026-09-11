# S03-02 — Verify the Phase-0 vertical slice

**Status:** Not started  
**Sprint:** 03 — Seeded playable world bootstrap  
**Source:** Technical plan §§14, 16; OPENCODE.md  
**Depends on:** S03-01, S02-04

## What to do

Demonstrate the Phase-0 exit flow end to end: authenticate, create or select a world as approved, create a club/squad, and observe a daily tick. Establish automated verification around this critical path.

## Acceptance criteria

- A repeatable end-to-end test or scripted verification proves the complete Phase-0 exit criterion.
- The event log shows the tick and its world context; no client-side simulation is involved.
- Frontend and API failures surface understandable user/developer feedback.
- Backend build, unit tests, and vet pass; frontend typecheck/lint pass using the repository’s configured commands.
- Any blocked decision required for a public onboarding flow is escalated in `docs/product_manager.md`.

## Delivery evidence

- Pending.
