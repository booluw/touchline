# IM63 — Squad page (table, filters, player panel, transfer-request flow)

**Status:** Implemented (browser check pending)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Product-owner request 2026-10-10; design `Touchline Squad.dc.html`. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM40 (roster columns), IM62 (fitness/availability)

## What to do

1. Build `/play/squad` from the design, desktop and mobile, using the endpoints that already exist.
2. Add the one missing roster field: recent form.

## Recorded decisions (product owner, 2026-10-10) — OPD-65

- Roster adds `recent_ratings` (last 5 rated appearances, 1–10, oldest first; never null).
- Morale panel: show existing data now (morale, expectation, latest emotional state, condition). Signed factors → IM64 (written only).
- Transfer request: approve / promise / deny with confirm step; consequence preview + asking price → IM65 (written only).
- Relationships: manager history + faction from `/dynamics`.
- Detail: desktop side panel (`?player=` query); mobile → `/play/players/:id` with tabs (own players only; others keep the existing view).
- Attributes/personality shown 1–20 = ceil(v/5). Density Simple/Standard only (potential hidden).

## Delivery evidence

Backend: `internal/player/{player,store,morale}.go` (`squadRecentRatings`), `openapi.yaml` (`SquadMorale.recent_ratings`), `roster_attributes_integration_test.go` (`TestRosterRecentRatings`). Also fixed `seedCompletedMatch` (test helper) to use a different away club: `fixtures_check` rejects home = away. gofmt clean; `go build`, `go vet`, `go vet -tags integration`, `go test ./...` pass. Integration on embedded Postgres 16: `TestRoster*` 4/4 pass; `internal/httpapi` Squad/Roster pass. 6 other `internal/player` integration tests (`TestPlayerDetailCarriesAbilityAndCareer`, `TestRecordMatchAppearancesInsideTx`, `TestWeeklyTickRaises…`, `TestDeny…`, `TestApprove…`, `TestPromise…`) fail identically with this change stashed — existing, not caused here.
Frontend: `pages/play/squad.vue`, `components/squad/PlayerPanel.vue`, `composables/manager/squad.ts`, `types/manager/squad.ts`, `utils/helpers.ts` (`moodLabel`), `pages/play/players/[playerId].vue`. `nuxi typecheck` + eslint pass. **Not yet exercised in a browser.**
