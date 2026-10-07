# IM45 — Matchday: per-side stats from match events

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Matchday.dc.html`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S02 match engine

## What to do

1. `GET /api/matches/:id/events` adds `stats` per side (home/away): goals, chances created, yellow cards, red cards, substitutions, penalties, counted from stored events.

## Open questions (design needs data the engine does not model)

- Possession, shots, xG: not emitted as events.
- Assistant suggestions, matchday modes, press conference, full-time "consequences" summary: not modelled.
- Pre-match win %: see IM39.

## Recorded decisions

Counts only over `match.match_events` rows.

## Delivery evidence

### Files

internal/match/read.go (`Stats`, `SideStats`), match_handlers.go, openapi MatchSideStats, match_feed_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.
