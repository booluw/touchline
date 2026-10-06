# IM40 — Squad table: nationality, age, contract and wage

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Squad.dc.html`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM17 roster, IM37 dossier

## What to do

1. `GET /api/clubs/:id/players` rows add `nationality`, `date_of_birth`, `age`.
2. Own-club rows add `contract` {`weekly_wage`, `end_date`, `squad_role`} from the active contract. (Value, status, condition and personality already ship in `dossier`.)

## Open questions (design needs data the engine does not model)

- Potential (Advanced mode column): blocked by OPD-60 (potential never exposed).
- Morale labels (Furious/Unsettled…): client maps from `morale` number.
- Predicted consequences of selling (fan sentiment −8 to rival): not computed.

## Recorded decisions

Contract only for the caller's own club, matching the IM37 public/private split.

## Delivery evidence

### Files

internal/player/{player,store}.go (`RosterContract`, roster SQL), openapi SquadMorale, player_squad_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.

### Notes

Roster lists `status = 'active'` only, so injured/loaned players (the design's Injured/On loan flags) are absent. Open question.
