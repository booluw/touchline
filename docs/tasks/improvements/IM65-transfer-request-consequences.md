# IM65 — Transfer request: consequence preview, asking price, reassure route

**Status:** Implemented (browser check pending)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Design `Touchline Squad.dc.html` (step 2 "Predicted consequences", asking-price options); product owner 2026-10-10 (OPD-65).
**Depends on:** IM63, IM64

## What to do

1. Read-only preview of each answer to an open request.
2. Approve takes an asking-price preset.
3. Fix: the panel's "promise playing time" answer must pause the request. `promise-playing-time` alone left it pending, so the player was still auto-listed after 21 days.

## Recorded decisions (product owner, 2026-10-10)

- `GET /api/clubs/:id/players/:playerID/transfer-request/preview` lists the constants each action applies, with nothing simulated:
  - **approve:** relationship +15; the player is listed at the chosen price.
  - **reassure:** relationship 0, then +10 if the promise is kept or −30 if broken; the request pauses for 28 days; the promise is judged after 4 weeks.
  - **deny:** morale −min(10, current) points; relationship −25; a 28-day cooldown.
  Squad, faction and fan effects (including the design's derby-rival penalty) are not modelled, so they are not shown.
- Approve body `{"price_preset": "quick_sale"|"valuation"|"hold_out"}` = 0.8 / 1.0 / 1.25 × market value. The default is valuation; an unknown preset returns 400 `invalid_price_preset`. The effect of price on buyer interest is not modelled.
- New `POST .../transfer-request/reassure` (engine `ReassurePlayer`, keyed by player). The panel's promise option now uses it.

## Delivery evidence

Backend: `internal/player/request_preview.go`, `internal/player/transfer_requests.go` (multiplier), `internal/httpapi/{player_squad_handlers,router}.go`, `openapi.yaml`. Tests: unit `TestBuildRequestPreview`, `TestMoraleTargetExplanationSums`. The integration test `TestHTTPTransferRequestPreviewReassureApprove` covers a preview with 3 options, reassure → `reassured` then preview 404, a bad preset → 400, and hold_out → listing asking_price = round(1.25 × value). gofmt clean; `go build`, `go vet`, `go vet -tags integration`, `go test ./...` pass. Integration on embedded Postgres 16: httpapi PlayerSquad / Preview / Docs and player Roster tests pass.
Frontend: `composables/manager/squad.ts` (`getPreview`, `respond` with preset, `reassure`), `types/manager/squad.ts`, `components/squad/PlayerPanel.vue` (step 2: price presets, consequences WhyBreakdown, notes), both pages pass the preset through. `utils/helpers.ts`: `toWhy` export reformatted (the `export /** */ const` form was not auto-imported). `nuxi typecheck` + eslint pass. **Not yet exercised in a browser.**
