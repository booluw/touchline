# S09-03 — Implement news generator and media read-model over event log

**Status:** Not started  
**Sprint:** 09 — Relationship-driven football world  
**Source:** PRD §63; technical plan §§4, 8, 16; OPENCODE.md  
**Depends on:** S01-02, S01-03, S07-01

## What to do

Build the automated News engine (`internal/world`) and news read-model over `world.events`. The engine listens asynchronously to significant events (blockbuster transfers, manager sackings, derby fixture results, title wins, player revolts) and renders contextual headline/article media stories stored in `world.news_stories`. Story rendering consumes event `Explanation` payloads directly to explain *why* events occurred without recomputing state.

## Acceptance criteria

- `world.news_stories` persists generated headline, body markdown, image asset tags, importance rating, and associated entity IDs.
- News engine processes incoming `world.events` and selects deterministic story templates matching event type and severity.
- Generated news stories extract `Explanation` factors (e.g. "Sold due to $40m debt requirement") to produce rich narrative explanations.
- Nuxt UI newsfeed component displays global and club-filtered news stories sorted chronologically by world tick.
- Major world events trigger breaking news banners pushed in realtime via WebSockets.

## Delivery evidence

- Pending.
