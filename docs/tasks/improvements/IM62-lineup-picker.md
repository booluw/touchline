# IM62 — Lineup picker: select and swap players (tactics page)

**Status:** Implemented (browser check pending)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Product-owner request 2026-10-10; design `Touchline Screens.dc.html` (tactics pitch, Substitutes/Replace panel, mobile bottom sheet). See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM46 (position fit), IM61 (tactics page)

## What to do

1. Pick and rearrange the XI on the tactics page, desktop and mobile, as in the design.
2. Roster rows carry what the picker needs: fitness and the lineup availability gate.

## Recorded decisions (product owner, 2026-10-10)

- Roster (`GET /api/clubs/:id/players`) adds `fitness` (0–1), `available` (the exact `SetLineup` gate via `squad.LoadSquad`, today's date) and `unavailable_reason` (`injured` | `ineligible`).
- Lives on the tactics page; token number = `round(overall × fit)` (engine fit 1 / 0.75 / 0.3 / 0.05); ring = fit.
- One Save button: POST tactics, then PUT lineup. Not atomic: if the lineup PUT fails, tactics are already saved and the error toast shows.
- Desktop: click pitch player → candidate list (best first) → click to replace (an XI player swaps); click two pitch players to swap; click a substitute then a slot; HTML5 drag (pitch↔pitch, row→pitch); Esc cancels; "Pick best XI" (greedy, skips unavailable).
- Mobile: compact pitch, tap opens a bottom sheet of candidates; picking an "IN XI" player swaps. No drag on touch.
- Formation change keeps players by slot index (same as the backend's slot-relative lineup).

## Delivery evidence

Backend: `internal/squad/store.go` (`LoadedPlayer.Injured`), `internal/player/{player,store,morale}.go`, `internal/player/availability_test.go`, `internal/player/roster_attributes_integration_test.go` (`TestRosterAvailabilityAndFitness`), `openapi.yaml`. `go build`/`vet`/unit pass; integration `internal/player` + `internal/httpapi` (Roster/PlayerSquad/Docs) pass on embedded Postgres 16.
Frontend: `utils/lineup.ts`, `components/tactics/LineupEditor.vue`, `components/ui/Pitch.vue` (names, roles, empty slots, drag), `types/ui/design.ts`, `types/manager/{squad,tactics}.ts`, `composables/manager/tactics.ts`, `pages/play/tactics.vue`. `vue-tsc` + eslint pass; lineup maths checked with a node assert script (no overlapping tokens in all 8 formations, greedy XI skips injured, swap/assign). **Not yet exercised in a browser.**
