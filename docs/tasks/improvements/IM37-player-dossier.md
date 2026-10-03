# IM37 — Full player dossier on player reads

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (player read models)
**Source:** Product request (2026-10-02), correcting IM35. Player reads should
return every `player_attributes` value, the `player_development` row and, in
general, everything stored about the player.
**Depends on:** IM35 (`hidden_attributes`, OPD-59), the IM17 roster read, the
IM20 player detail card, the S06-03 morale detail, the S08-02 development read.

## What to do

1. Add a `dossier` object to `GET /api/players/:id`, `GET /api/clubs/:id/players`,
   `GET /api/clubs/:id/players/:pid` and `.../development`.
2. Public snapshot for any world player: players-row bio, every attribute key,
   development (minus potential), condition, personality.
3. Public history (single-player reads only, 10 newest rows each): appearances,
   attribute changes, injuries, history events.
4. Private, own club only: contracts, emotional states, preferences, transfer
   requests, transfer-request cooldown.
5. Widen `hidden_attributes` to every hidden trait except potential.
6. Never expose any potential column.

## Recorded decisions

OPD-60 in `docs/product_manager.md`. Answers from the product owner: keep the
IM35 block; cover every player table; public/private split for rivals; recent
10 rows for logs; roster rows carry only the snapshot; potential stays hidden.

## Delivery evidence

### Files

- `backend/internal/player/dossier.go` (new): `PlayerDossier` and section
  types, `loadDossiers` (batched by `player_id = ANY($1)`, one query per
  table, shared by the roster and single reads), `playerDossier`,
  `dossierHistory`, `dossierPrivate`.
- `backend/internal/player/player.go`: `HiddenAttributes` widened to nine
  traits; `Dossier` on `PlayerMoraleRow` and `PlayerMoraleDetail`.
- `backend/internal/player/profile.go`: `PlayerDetail.Dossier`, with the
  private section gated by the existing own-club check.
- `backend/internal/player/development.go`, `morale.go`: fill `Dossier`; the
  roster batches hidden traits and dossiers.
- `backend/internal/player/store.go`: `squadHiddenAttributes` (batch);
  `squadRows` no longer joins hidden traits.
- `backend/internal/apidocs/openapi.yaml`: `PlayerDossier` schema, widened
  `HiddenAttributes`, `dossier` on the four response schemas.
- `backend/internal/player/dossier_integration_test.go` (renamed from IM35's
  file): `TestHiddenAttributesOwnClubOnly` now also checks that every stored
  attribute value appears, history appears only on single reads, private
  appears only for the own player (with a contract), and the serialised
  dossier contains no `potential`.
- Docs: `docs/product_manager.md` (OPD-60), `docs/how-to/glossary.md`, IM35
  cross-reference.

### Verification (2026-10-02, working tree on `a96e7c1`)

- `gofmt`, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...` and `go test ./...` all
  pass (including `TestDocsCoverRouter` and `TestDocsOpenAPIValid`).
- `go test -p 1 -tags integration ./internal/player/...` against a throwaway
  `postgres:16` container (`TEST_DATABASE_URL`): `TestHiddenAttributesOwnClubOnly`,
  `TestRosterCarriesAttributesAndOverall`, `TestPlayerDetailHidesOtherClubsWage`
  and `TestPlayerDetailIsWorldScoped` pass.
- Six other tests in that package fail identically on untouched `a96e7c1`
  (baseline run in a separate worktree, same database), so they predate this
  change: `TestPlayerDetailCarriesAbilityAndCareer` (its seed SQL uses a
  nonexistent `key` column), `TestRecordMatchAppearancesInsideTx`,
  `TestWeeklyTickRaisesTransferRequestForDeepShortfall`,
  `TestDenyPlayerRequestDropsMoraleAndSetsCooldown`,
  `TestApprovePlayerRequestListsPlayer`, `TestPromisePlayingTimeGradedWeekly`.
- `go test -tags integration -run 'Player|Squad|Roster|Development|Morale'
  ./internal/httpapi/`: `TestHTTPGetSquadDynamics`,
  `TestHTTPGetSquadDynamicsReflectsUnrest` and `TestHTTPPlayerDevelopmentDetail`
  fail identically on the baseline; the rest pass.

### Not verified

- History and private sections are only checked for presence and contracts;
  no test seeds injuries, emotional states, preferences or history events and
  checks their row contents.
- No manual check through the running server.
