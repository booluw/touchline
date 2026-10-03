# IM35 — Hidden attributes on player reads

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (player read models)
**Source:** Product request (2026-10-02). Player responses should also return
the hidden attributes a game like Football Manager exposes, limited to ones
that can be trained.
**Superseded in part by:** IM37 (OPD-60) widens `hidden_attributes` to every
non-potential hidden trait and adds the full `dossier`.
**Depends on:** `player.player_hidden_traits` (migration 0006), the IM17 roster
read, the IM20 player detail card, the S06-03 morale detail, the S08-02
development read.

## What to do

1. Add `hidden_attributes` `{professionalism, temperament, adaptability}` to
   the four player reads: `GET /api/players/:id`, `GET /api/clubs/:id/players`,
   `GET /api/clubs/:id/players/:pid` and `.../development`.
2. Show exact values only for the caller's own players. Another club's player
   never carries the block.
3. Mirror the change in `openapi.yaml`.

## Recorded decisions

OPD-59 in `docs/product_manager.md`. These three traits were picked because
they map to FM hidden attributes that can be coached through mentoring.
Consistency, pressure handling, injury susceptibility, learning speed and
potential stay hidden. Making training move the three traits is IM36.

## Delivery evidence

### Files

- `backend/internal/player/player.go`: `HiddenAttributes` type; `Hidden`
  field on `PlayerMoraleRow` and `PlayerMoraleDetail`.
- `backend/internal/player/profile.go`: `PlayerDetail.Hidden`, filled inside
  the existing own-club wage branch.
- `backend/internal/player/development.go`: `PlayerDevelopmentDetail.Hidden`.
- `backend/internal/player/morale.go`: morale detail fills `Hidden`.
- `backend/internal/player/store.go`: `playerHiddenAttributes`; `squadRows`
  LEFT JOINs `player_hidden_traits` (no extra roster query).
- `backend/internal/apidocs/openapi.yaml`: `HiddenAttributes` schema, referenced
  from `SquadMorale`, `PlayerDetail`, `PlayerMorale` and `PlayerDevelopment`.
- `backend/internal/player/dossier_integration_test.go` (renamed in IM37):
  `TestHiddenAttributesOwnClubOnly`.
- Docs: `docs/product_manager.md` (OPD-59), `docs/how-to/glossary.md`.

### Verification (2026-10-02, working tree on `a96e7c1`)

- `gofmt`, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...` and `go test ./...` all
  pass (including `TestDocsCoverRouter` and `TestDocsOpenAPIValid`).

### Not verified

- `TestHiddenAttributesOwnClubOnly` is compile-checked only. Docker was not
  running and `TEST_DATABASE_URL` was unset, so it has not run against Postgres.
- No manual check through the running server.
