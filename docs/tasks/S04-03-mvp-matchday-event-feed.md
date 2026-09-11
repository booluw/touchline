# S04-03 — Ship the MVP matchday event-feed experience

**Status:** Not started  
**Sprint:** 04 — Deterministic football competition  
**Source:** OPENCODE.md; technical plan §§5, 9, 11, 18; PRD §55  
**Depends on:** S04-02, S02-04

## What to do

Render the server-produced match event list as the approved text/live-commentary feed. Stream/replay the same events over the single socket for live viewing while retaining quick-result access to the same simulation outcome.

## Acceptance criteria

- The match screen renders ordered key events, including goals, cards, injuries, substitutions, and half/full time when produced by the engine.
- Live updates use the single multiplexed WebSocket and remain correct when replayed at controlled pacing.
- Quick result and live viewing consume the same persisted simulation event list.
- The client never calculates match outcomes or events.
- No 2D pitch/canvas implementation is included in MVP scope.

## Delivery evidence

- Pending.
