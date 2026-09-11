# S06-04 — Implement manager profiles, direct messaging, and relationship graph foundations

**Status:** Not started  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§48, 66; technical plan §§6, 12, 16; OPENCODE.md  
**Depends on:** S02-01, S04-01

## What to do

Implement the `social` engine schema (`social.relationships`, `social.messages`, `social.manager_trust_scores`, `social.promises`). Model entity relationships using the graph table structure (`entity_a_id`, `entity_a_type`, `entity_b_id`, `entity_b_type`, `relationship_type`, `strength`, `trust`, `sentiment`). Build manager profile pages, direct manager-to-manager messaging API, and automatic rivalry tracking based on head-to-head match outcomes and controversial transfer dealings.

## Acceptance criteria

- `social.relationships` table is created with unique constraints on `(entity_a_id, entity_b_id, relationship_type)` supporting polymorphic entity types (`player`, `manager`, `club`).
- `social.messages` supports manager-to-manager direct messaging within a world, with rate limiting and basic content sanitization.
- Manager profiles display career stats, active club, trophy cabinet, head-to-head history against viewing manager, and trust score.
- Head-to-head fixtures and transfer negotiations automatically create or update `rivalry` and `trust` graph edges between managers and clubs.
- WebSocket pushes incoming social messages and relationship change alerts to active sessions in realtime.

## Delivery evidence

- Pending.
