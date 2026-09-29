# IM20 — Player detail page

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (manager squad surface)
**Source:** Product request — a player's name in a match feed or a squad list was
a dead end; opening a player needed a second, differently-shaped call that only
worked for your own club, so a cup opponent or a transfer target could not be
looked at
**Depends on:** the S09-02 attribute/overall recipe (`squad.PositionalOverall`,
the six EAV category means — the IM17 roster block), the attribution read model
`player.player_appearances` (written by match completion, `rating`/`goals`/
`assists` added in 0045), and `player.contracts.weekly_wage` as the club finances
read it. No schema, config, or engine change.

## What to do

Give a player a page, and the read to feed it. One endpoint,
`GET /api/players/:playerID`, that any manager can call for **any player in their
own world**, showing identity, current club, ability, career record — and the
weekly wage when the player is at the caller's own club. Reachable from the
match feed and the squad list.

## Delivery evidence

### Backend

- `backend/internal/player/profile.go` (new):
  - `PlayerDetail` — `player` ref, `club`, names, `nationality`, `date_of_birth`,
    `position`, `squad_number`, the six-category `attributes`, `overall`,
    `career`, and `weekly_wage` (**omitted**, never zero, unless the player is at
    the caller's own club).
  - `PlayerCareer` — `appearances` / `goals` / `assists` / `average_rating` from
    one aggregate over `player.player_appearances`. `AVG` ignores NULL ratings, so
    a match that predates attribution is still an appearance and does not drag the
    average — the semantics 0045 documents.
  - `GetPlayerDetail(ctx, worldID, managerID, playerID)` — **world-scoped, not
    ownership-scoped**. It reuses the existing `playerProfile` identity read, then
    rejects `prof.WorldID != worldID` with `ErrPlayerNotFound`, so a foreign id
    answers exactly like a non-existent one and the endpoint can never confirm
    another world's players exist.
  - `playerAttributeMeans` is the one-player twin of `squadAttributeMeans` over the
    same shared `categoryMean` arithmetic, and the overall comes from the canonical
    `squad.PositionalOverall` — the same two numbers the roster block reports.
  - `playerWeeklyWage` reads the active contract with the same
    `weekly_wage::bigint` cast the club finances read uses, so a player page and
    the wage bill can never disagree. Nil (omitted) when there is no active
    contract — a free agent has no wage.
  - The wage gate resolves the caller's own club through the existing
    `clubByManager`; a jobless manager simply has no wage to see
    (`ErrManagerHasNoClub` is not an error on this read), while any other database
    failure still propagates.
- `backend/internal/httpapi/player_squad.go` — `handleGetPlayerProfile`: 400 on a
  malformed id, 403 on no world context, `playerStatus` for the rest, so
  `ErrPlayerNotFound` maps to the established 404 body. No club param, because
  the read is not club-gated.
- `backend/internal/httpapi/router.go` — `GET /api/players/:playerID` next to the
  existing `POST /api/players/:playerID/release`, with a comment stating the
  world-not-club rule and the wage exception.
- `backend/internal/apidocs/openapi.yaml` — the `/api/players/{playerID}` path
  (`operationId: getPlayerDetail`, 200/400/404) and the `PlayerDetail` /
  `PlayerCareer` component schemas. The existing S09-02 `PlayerProfile` schema
  (the dressing-room dynamics profile) is a different shape under a different
  name — the new schema is `PlayerDetail`, so neither shadows the other.
  `TestDocsCoverRouter` / `TestDocsOpenAPIValid` pass.
- Tests:
  - `backend/internal/player/detail_integration_test.go` (new) —
    `TestPlayerDetailCarriesAbilityAndCareer` pins the EAV to known per-category
    values and checks the card against `ROUND(AVG(value))` read straight from the
    table (the same source-of-truth helper IM17's roster test uses), asserts the
    overall equals `squad.PositionalOverall` of those means and lands in 1..99,
    seeds a rated and an unrated appearance to prove both matches count while only
    the rated one moves `average_rating`, and pins the own-club wage to a known
    contract value. `TestPlayerDetailHidesOtherClubsWage` proves a rival's player
    is readable **and** that the rival's wage — which the fixture really has — is
    withheld.     `TestPlayerDetailIsWorldScoped` moves a player to another world and
    asserts `ErrPlayerNotFound`, identical to an unknown id. That "another world"
    is created through `world.Service.CreateWorld` rather than faked with a bare
    `uuid.New()`: `player.players.world_id` references `world.worlds(id)`, so a
    random uuid fails the foreign key and the test dies before reaching the
    isolation assertion it exists for. `setWage` also asserts it updated exactly
    one active contract, so a fixture that stopped seeding contracts reports
    "updated 0 contracts" instead of a baffling "wage is nil" three assertions
    later.
  - `backend/cmd/api/player_detail_integration_test.go` (new) — the HTTP edge: the
    full wire shape (all six attribute keys, `overall` in 1..99, all four career
    keys, `weekly_wage` present for the caller's own player and **absent** for a
    rival's), 404 for an unknown id, 400 for a malformed one, 401 unauthenticated.
- Verify (backend): `gofmt -w` on the touched files, `go build ./...`,
  `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/... ./cmd/...`,
  `go test ./...` all green. (Integration tests remain compile-gated locally — no
  `TEST_DATABASE_URL` and no usable local Postgres or Docker daemon on this arm64
  host, every installed `postgres` binary being x86_64.)

### Frontend

- `frontend/app/composables/usePlayer.ts` (new) — the typed `PlayerDetail` /
  `PlayerAttributes` / `PlayerCareer` wire shapes, the shared
  `playerAttributeCategories` label list, and `getPlayer(playerId)` in the
  `useClub` / `useCompetition` shape (`$api`, toast on failure, rethrow).
- `frontend/app/pages/play/players/[playerId].vue` (new) — the card: name,
  position, squad number, age from the date of birth, nationality, club (or "Free
  agent"), the overall, the career row, and the six attribute pairs, plus the wage
  line (or an explicit "owning club's business" note when it is withheld). Loading
  and error states match the squad page's.
- `frontend/app/pages/play/squad.vue` — the squad-list name is now a `NuxtLink` to
  the card.
- `frontend/app/pages/play/matches/[fixtureId].vue` — each feed line with a named
  player carries a "player card" link next to the commentary. The two commented-out
  imports at the top of that file (another workstream's) are left exactly as they
  were.
- Verify (frontend): `pnpm typecheck` reports **no** error in any new or touched
  file. The repo's baseline is not clean — `pnpm typecheck` and `pnpm lint` already
  fail on pre-existing issues (`squad.vue` implicit anys, `tactics.vue` /
  `training.vue` / `finances.vue` importing `~/stores*` with an extra `app/`
  prefix, `types/index.ts` duplicate re-exports, `utils/api.ts` generics) and
  `pnpm build` fails on those same three unloadable store modules, none of which
  this change touches.

- Docs: `docs/how-to/glossary.md` §3 ("Player detail card"), the OpenAPI
  `MatchEvent`/`PlayerDetail` schemas, `docs/product_manager.md` OPD-46.

## Recorded decisions

- **A page, not another roster endpoint.** The squad list only ever held the
  caller's own players, so "look at this player" was impossible for anyone else.
  The read is world-scoped and the page is a normal Nuxt route, so a name in a
  match feed and a name in a squad list are the same destination.
- **World-scoped, not ownership-scoped; 404 for another world.** Scoping to the
  manager's club would have made the page useless for a cup tie or a scouting
  decision, and answering a foreign id with 403 would confirm that the player
  exists somewhere. Identical 404s leak nothing.
- **The wage is the only private field.** Every other number on the card is
  football information the manager can already infer from a match report. A
  rival's contract is somebody else's business, so it is **omitted** rather than
  zeroed — an absent field is a fact, a zero is a lie.
- **Ability is computed, not stored.** The category means and the overall come
  from the EAV and the canonical recipe at read time, so a recalibration moves the
  player page, the squad screen, offers and the match engine together — the same
  doctrine IM17 recorded for the roster.
- **Career totals are the appearance table's totals.** Nothing is accumulated
  separately, so a player who changes clubs keeps one continuous record, and a
  match's attribution can only ever be counted once.
- **No nav entry.** The page is reached by clicking a person, which is the only
  sensible entry point; `MANAGER_ROUTES` in `frontend/app/utils/routes.ts` is a
  stale copy of the admin links and is a separate fix.
