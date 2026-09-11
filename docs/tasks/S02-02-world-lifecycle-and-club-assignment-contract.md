# S02-02 — Implement world lifecycle and manager-club assignment boundaries

**Status:** Not started  
**Sprint:** 02 — Authenticated, schedulable worlds  
**Source:** OPENCODE.md; technical plan §18; PRD §§5, 42–43, 74  
**Depends on:** S01-01, S02-01

## What to do

Implement the platform constraints for multiple worlds and one active club per manager. Expose only the lifecycle and assignment capabilities supported by a product-approved world-bootstrap design.

## Acceptance criteria

- A world can be created and independently identified; all subsequent operations scope data to that world.
- A manager cannot hold more than one active club assignment anywhere on the platform.
- Resignation/sacking can end an active assignment without deleting career history.
- Global manager reputation history is append-only/displayable, while any operational reputation calculation is world-scoped.
- World selection, initial club assignment, and initial league composition use an approved decision; implementation does not infer missing rules.

## Delivery evidence

- Pending.
