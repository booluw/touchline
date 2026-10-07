# IM46 — Tactics: position fit per lineup slot

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html (tactics)`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S02 lineup

## What to do

1. `GET /api/clubs/:id/lineup` adds per-slot `fit` using the engine's `positionFit` (natural / same family / out of position) so the UI ring matches selection logic.

## Open questions (design needs data the engine does not model)

- Effective rating per slot with factors (fitness, morale penalties): engine weighting is internal; confirm before exposing numbers.
- "Pick best XI" suggestion endpoint.

## Recorded decisions

Expose the engine's own score; no new formula.

## Delivery evidence

### Files

internal/squad/lineup.go (`PositionFit`), internal/tactics/service.go, openapi Lineup, tactics_training_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.

### Notes

Finding (engine, unchanged): `positionFit` treats GK as defence, so a keeper in a CB/LB/RB slot scores 0.75 despite the comment saying keepers never play outfield.
