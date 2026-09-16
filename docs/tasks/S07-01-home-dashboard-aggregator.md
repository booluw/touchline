# S07-01 — Implement home dashboard backend aggregator and Nuxt 3 frontend view

**Status:** Implemented  
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

- `backend/internal/dashboard/` — dedicated read-only aggregator package
  (`model.go` item/action/snapshot types + numerics, `store.go` queries over
  transfer/finance/board/player/competition/social/match tables,
  `service.go` `GetDashboard` + realtime `PushCategory`/`PushWorldDelta`).
- `GET /api/dashboard` wired to the aggregator via
  `backend/internal/httpapi/dashboard_handlers.go` (replaces the session stub);
  world+manager resolved at request time (OPD-15); club-less managers get empty
  sections.
- Realtime: `pkg/realtime/event.go` gains `EventDashboardUpdate =
  "dashboard_update"`; `internal/app/app.go` runs `PushWorldDelta` after each
  daily/weekly/monthly world-tick pass and subscribes to
  `BID_PLACED`, `BID_COUNTERED`, `BID_ACCEPTED`, `BID_REJECTED` to push the
  selling club's manager an urgent refresh.
- Docs coverage green: `DashboardItem`, `DashboardAction`, `DashboardSnapshot`
  schemas in `backend/internal/apidocs/openapi.yaml`.
- Frontend minimal hook: `frontend/app/composables/useDashboard.ts` (Action
  type, reactive data, `dashboard_update` merge by stable ID) and
  `frontend/app/pages/index.vue` (categorized urgent/important/interesting
  cards).
- Tests: `dashboard_test.go` (builders, dedupe push), compile-checked
  `dashboard_integration_test.go` (empty feed for club-less manager, board +
  expiry surfacing, realtime push over a live DB).
- Design doc: `docs/design/dashboard-numerics.md`.
