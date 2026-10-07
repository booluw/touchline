# IM43 — Board: members, confidence history, negotiation preview

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Finances Board.dc.html (Board)`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S04 board, job security

## What to do

1. `GET /api/managers/me/board` adds `persona`, `members` [{`name`, `agenda`, `influence`}] and `confidence_history` (snapshots, oldest first: `world_tick`, `total_score`, `created_at`, `explanation`).
2. New `POST /api/managers/me/board/mandates/:id/negotiate/preview` runs the same checks as negotiate without writing, returning `accepted`, `reason` and `tolerance`.

## Open questions (design needs data the engine does not model)

- Chairman traits 1–20, ownership %, bio: not stored (board has a persona + members only).
- Mandate weights ("35% of judgement") and current-vs-target progress: not stored per mandate.
- "1 request left this season", confidence cost of asking, multiple proposed options with agree %: negotiation is a deterministic tolerance check.

## Recorded decisions

Preview shares the validation path with negotiate so they cannot drift.

## Delivery evidence

### Files

internal/board/{model,store,service}.go (`checkNegotiation` shared by negotiate + `PreviewNegotiation`, `boardMembers`, `confidenceHistory`), board_handlers.go, router.go new route, openapi BoardView/NegotiationPreview + path, board_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.

### Notes

No code writes `club.board_members`, so `members` is empty until seeding exists.
