# S05-01 — Implement MVP squad, tactics, and simple training commands

**Status:** Not started  
**Sprint:** 05 — Manager controls and finance  
**Source:** PRD §§3–4, 26, 52, 55–56, 73–74; technical plan §§10, 12, 16  
**Depends on:** S04-02

## What to do

Deliver the server-side commands and responsive management screens for squad selection, approved MVP tactics, and simple-mode training. Support deadline-based actions and future PolicyBot reuse.

## Acceptance criteria

- Authorized managers can view their squad and submit validated squad, tactic, and training-plan commands through documented APIs.
- Commands are persisted, evented, world-scoped, and validated on the server against ownership/deadline rules.
- The MVP controls conform to a product-approved definition of “simple mode”; unspecified tactical/training depth is not assumed.
- Submitted tactics can be used as match-engine inputs; training plans can be processed by their subscribed tick handler.
- The command layer can identify a human actor or PolicyBot actor without separate business logic.

## Delivery evidence

- Pending.
