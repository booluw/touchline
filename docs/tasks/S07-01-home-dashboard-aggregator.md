# S07-01 — Implement home dashboard backend aggregator and Nuxt 3 frontend view

**Status:** Not started  
**Sprint:** 07 — MVP experience, operations, and validation  
**Source:** PRD §53; technical plan §11, §12, §16; OPENCODE.md  
**Depends on:** S04-01, S05-02, S06-01, S06-02

## What to do

Build the dedicated backend aggregation endpoint `GET /api/dashboard` and corresponding Nuxt 3 home dashboard screen (`pages/index.vue`). The dashboard aggregates items across engines into three distinct categories: "Urgent" (pending bids, contract expiries, imminent match deadlines), "Important" (board confidence shifts, financial warnings, player unhappiness), and "Interesting" (league standings changes, rival results, transfer market news).

## Acceptance criteria

- `GET /api/dashboard` executes as a single performant query/aggregation joining active world events, club state, squad status, and social alerts for the authenticated manager.
- The dashboard UI renders clean categorized cards ("Urgent", "Important", "Interesting") without requiring multiple client-side API fetches.
- Urgent items provide direct action buttons (e.g. "Respond to Bid", "Submit Lineup", "Renew Contract").
- Realtime WebSocket updates dynamically prepend new urgent/important events to the active dashboard view without full page reload.
- Mobile and desktop layouts render responsively following Touchline design system styling.

## Delivery evidence

- Pending.
