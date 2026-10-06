# IM41 — Club page: founded, stadium, facilities, history

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html (club)`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S01 club detail

## What to do

1. `GET /api/clubs/:id` adds `founded_year`, `city`, `tier`, `reputation`, `primary_color`, `secondary_color`, `stadium` {`name`, `capacity`}, `facilities` [{`type`, `level`}], `history` (latest 10 `club_history` rows).

## Open questions (design needs data the engine does not model)

- Nickname ("The Millers") and prose backstory are not stored.
- Club personality (club_dna) display shape not designed beyond the card; club_dna left for a later task.

## Recorded decisions

All columns already exist in `club.clubs`, `club.facilities`, `club.club_history`.

## Delivery evidence

### Files

internal/club/service.go (profile columns, `clubFacilities`, `clubHistory`), openapi ClubDetail, bootstrap_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.
