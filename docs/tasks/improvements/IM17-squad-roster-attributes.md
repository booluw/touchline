# IM17 — Squad roster attributes and overall

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (manager squad surface)
**Source:** Product request — the manager's squad list carried morale and
playing time but no ability, so a squad screen had to guess quality or make a
second call
**Depends on:** the `player.player_attributes` EAV (categories
`technical`/`physical`/`mental`/`tactical`/`positional`/`goalkeeping`,
`0006_player`), the canonical overall recipe in `internal/squad`
(`DefaultPositionWeights`, `PositionalOverall`, `OverallCap = 99`), and the
S06-03 roster read model (`PlayerMoraleRow`, `ListSquadMorale`). No in-flight
improvement changes any of these read shapes.

## What to do

Give `GET /api/clubs/:id/players` the one thing a squad screen cannot do without:
each active player's **six attribute-category means** and their
**position-weighted `overall` (1..99)**. Both must come from the existing EAV and
the existing canonical recipe — no new persisted composite, no new endpoint, no
frontend.

## Delivery evidence

- `backend/internal/player/player.go`:
  - `PlayerAttributes` is now the **six-category wire struct** (`technical`,
    `physical`, `mental`, `tactical`, `goalkeeping`, `positional`) — the same
    shape `internal/faction` publishes in its influencer profile, so the roster
    and dressing-room payloads agree field-for-field.
  - `PlayerMoraleRow` gains `Attributes PlayerAttributes` (`json:"attributes"`)
    and `Overall int` (`json:"overall"`), both always emitted (never `omitempty`)
    — one request renders rating, category profile and morale together.
  - The vestigial S05 stub that used to own the `PlayerAttributes` name
    (`{ID, PlayerID}`, returned zero-valued by the never-wired
    `GetPlayerAttributes`) is renamed `PlayerAttributesRecord`; the method
    signature moved with it, so the six-category name now means the six
    categories everywhere.
- `backend/internal/player/store.go` — `squadAttributeMeans(ctx, q, clubID)`
  rolls the club's **active** roster EAV up into `map[uuid.UUID]PlayerAttributes`
  (per-category integer mean, round-half-up via `categoryMean`, the identical
  arithmetic to `internal/squad.attachAttributes`); a player with no attribute
  rows maps to the zero category set. `math` is the only new import.
- `backend/internal/player/morale.go` — `ListSquadMorale` loads the roll-up
  alongside the existing `squadRows` + `openRequestsByClub` reads and sets, per
  row, `Attributes` and
  `Overall = squad.PositionalOverall(position, squad.AttributeSnapshot{…})`.
  `internal/squad` is a new import of the package; `squad` imports no internal
  package, so there is no cycle.
- `backend/internal/apidocs/openapi.yaml` — `SquadMorale` gains
  `attributes` (`$ref` the existing `PlayerAttributes` schema, already used by
  the dynamics profile) and `overall` (`integer`, 1..99); the
  `listSquadPlayers` summary/description now name the attribute block and the
  overall. `TestDocsCoverRouter` / `TestDocsOpenAPIValid` unaffected (no new or
  renamed route).
- Tests (integration, compile-gated locally — live Postgres required):
  - `backend/internal/player/roster_attributes_integration_test.go` (new) —
    `rosterCategoryMeans` reads the expected means straight from the EAV
    (`ROUND(AVG(value))::int`) so the roll-up is checked against the source of
    truth rather than against itself. `TestRosterCarriesAttributesAndOverall`
    pins every attribute row of a roster player to known per-category values,
    then asserts the row's `attributes` equal the EAV means and its `overall`
    equals `squad.PositionalOverall` of that player's position (and lands in
    1..99). `TestRosterOverallCapsAt99` sets every attribute to `100` and
    asserts the row still reports the 99 display ceiling.
  - `backend/cmd/api/player_squad_integration_test.go` — the roster round-trip
    now asserts the wire shape: all six `attributes` keys present and `overall`
    in 1..99 on the first roster row.
- Verify: `gofmt -w` on the touched files, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/... ./cmd/...`, `go test ./...`
  all green. (Integration tests cannot execute locally — no
  `TEST_DATABASE_URL`/Docker — so they are compile-gated here and run in CI
  against a live Postgres.)
- Docs: `docs/product_manager.md` OPD-43, `docs/how-to/glossary.md` §3
  ("Roster attributes block"), `backend/internal/apidocs/openapi.yaml`.

## Recorded decisions

- **`overall`, not `rating`; `rating` stays the match number.** The 1..10
  `rating` is the per-match attribution figure (`player.player_appearances`).
  Naming the squad's 1..99 number `overall` keeps the two impossible to confuse
  in a payload, and matches every other surface in the product (offers, academy
  read models, valuation, the dynamics profile).
- **Reuse the canonical recipe, never a second one.** `squad.PositionalOverall`
  is the one approved overall; the roster calls it rather than re-deriving
  weights, so a future PM recalibration of `DefaultPositionWeights` moves the
  squad screen, the shop and the match engine together.
- **Always present, even when unseeded.** No `omitempty`, no nullable block: a
  category a player has no keys for (goalkeeping for an outfielder, per
  playergen's role bias) reports `0`, and a fully unseeded player reports an
  all-zero block with `overall = 1` (the `PositionalOverall` floor). A field
  that vanishes is a UI branch; a zero is a number, and it matches the contract
  the S09-02 influencer profile already ships.
- **Rollup at read time, nothing denormalized.** The EAV is the single source of
  truth, consistent with the package's doctrine (and with S09-02, which
  deliberately never denormalizes hierarchy or factions onto players). A roster
  of 20 players is one extra indexed query, not a stale column to reconcile.
- **No migration, no new endpoint, no frontend.** Pure read extension over
  existing schema; frontend squad-screen work is a separate task (out of scope
  per sprint rules).
