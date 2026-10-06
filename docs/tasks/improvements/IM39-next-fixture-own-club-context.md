# IM39 — Next fixture: our own club's position and form

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline.dc.html (Next match card)`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM next-fixture scout report

## What to do

1. `GET /api/clubs/:id/next-fixture` adds `club` with the same `league_position`, `form`, `tier`, `reputation` block the opponent already has, for the requesting club.

## Open questions (design needs data the engine does not model)

- Win/draw/loss percentages + factors: no pre-match probability is computed anywhere.
- Opponent style text ("high press, 4-2-3-1"): tactics are stored but no style summary/"weak to crosses" stat exists.
- Referee: not modelled.

## Recorded decisions

Reuse the opponent-side queries for the own club; no new computation.

## Delivery evidence

### Files

internal/scout/scout.go (`Club *Report`), openapi NextFixtureView.club, scout_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.

### Notes

`TestNextFixtureScout` already failed before this change, at the existing `league_position before results` assertion (reproduced with the change stashed). The new IM39 assertions run before that line and pass.
