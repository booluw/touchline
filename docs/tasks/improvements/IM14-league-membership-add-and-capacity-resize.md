# IM14 — League membership: add a club to a league, and resize league capacity

**Status:** Implemented
**Sprint:** Improvements (league administration)
**Source:** Product decision (manual session)
**Depends on:** seed-status `club_competitions` membership model (0025/0037);
the last-completed-season rollover path (`rollover.go`); admin pyramid controls;
`recordSeedEvent` audit plumbing; the apidocs route↔openapi gate
(`TestDocsCoverRouter`).

## What to do

Give the admin two league membership controls, both applying to the **season
after** the current one, so a running world never has its live schedule edited
underneath teams:

1. **`AddClubToLeague`** — add an existing, league-less club into a league.
   The club is recorded as a *declared member* (a `role='league'` row in
   `club_competitions`), its `club.clubs.country` is normalised to the league's
   country name, and it joins in at the next season rollover. If the league's
   next season would fall short of `team_count`, missing places are auto-filled
   from the country's league-less club pool (generating fresh AI clubs if the
   pool is exhausted), so the fixture schedule is never holey.
2. **`SetLeagueCapacity`** — raise a league's `team_count` and set the
   **promotions/relegations** that will apply at the next rollover. Raising is
   the only direction allowed (see recorded decision *Upward-only capacity*);
   the reciprocal counts on the league's two neighbours are auto-adjusted in
   the same transaction so the country's promotion/relegation graph stays
   coherent — per-league edits alone can never satisfy the adjacency symmetry
   that `validateAdjacency` (util.go:16) enforces.

Both operations are pure **declarations** (no standings, no fixture writes,
`IsArchived`-gated), fully audited through `recordSeedEvent`
(`CLUB_JOINED_LEAGUE`, `LEAGUE_CAPACITY_CHANGED`), and never touch the live
season.

## Behaviour

### `AddClubToLeague(ctx, worldID, clubID, leagueID)`

- Serialize on the world (`FOR UPDATE`), reject archived worlds.
- The club must exist in the same world (`ErrClubNotFound` /
  `ErrClubWorldMismatch`) and be **league-less**: adding a club that already
  has a `role='league'` membership is rejected (`ErrClubAlreadyInLeague`).
  "League-less" is schema-consistent with the seed-status "full" check at
  competition_handlers.go:272 — every club in a league has exactly one
  `role='league'` row (partial-unique `uq_club_one_league`).
- The league must exist in the same world (`ErrCompetitionNotFound` /
  `ErrCompetitionWorldMismatch`) and be a **league**, not a cup.
- **Capacity check**: the club is admitted only while
  `realSize(live season entries) + pendingDeclaredMembers + 1 <= team_count`
  (`ErrLeagueFull`). The `+1` reserves a seat for the next composition; because
  rollover keeps every incumbent plus incoming streams, this makes structural
  over-subscription impossible (see *Add-cap arithmetic* below).
- On success: `UPDATE club.clubs SET country = <league country name>` (the
  auto-normalisation locked in an earlier session), `INSERT` the
  `role='league'` membership row (racing duplicate → `409`
  `ErrCompetitionAlreadyMember`), and emit a `CLUB_JOINED_LEAGUE` audit event.
- Result model: the club's next-season projection (live season size, whether
  they are declared, projected rank/entry) so the admin sees *when* the change
  lands, not just that it was stored.

### `SetLeagueCapacity(ctx, leagueID, CapacityParams{TeamCount, Promotions, Relegations})`

- `TeamCount` must be even and >= 4 (`ErrInvalidTeamCount`). `Promotions` and
  `Relegations` must each be < `TeamCount` (`ErrInvalidCounts`).
- **Upward-only**: `new TeamCount < current` is rejected
  (`ErrLeagueShrink`) — shrinking an already-composed pyramid is never coherent.
- Promotion/relegation links must be coherent (`ErrAdjacencyMismatch`): a
  league in the top tier must not set `Promotions > 0` (nothing above it;
  `PromotesTo` required to be set, and once set it may not be cleared),
  `Relegations` must equal the league below's `Promotions`, and the below
  league's `Relegations` must equal our `Promotions`.
- **Auto-adjust reciprocal neighbours** (recorded decision): rather than
  refusing when the neighbours disagree, the engine computes each neighbour's
  missing reciprocal count and updates it in the **same transaction**, then
  reloads and re-validates the country's whole ladder with `validateAdjacency`.
  A per-league change therefore lands as a coherent ladder or errors; a single
  call that would leave an island (`PromoteTo` set with a gap, or a neighbour
  count that would deadlock a reciprocal change) fails with
  `ErrAdjacencyMismatch`.
- Emits `LEAGUE_CAPACITY_CHANGED` and returns the freshly-read league.

### Composition at rollover (membership-aware)

The classic rollover composes the next season from **standings only**
(`stayers + promoted-in + relegated-in`) and hard-errors when
`len(entries) != TeamCount` (rollover.go:110–113). That is now the base layer;
the rollover adds two declared layers on top, in order:

1. **Declared members** (`role='league'` rows that are NOT in the live season's
   `competition_entries` and NOT playing another league's latest season) are
   merged into the league's next-season entry set.
2. **Auto-fill**: if the merged set is still short of `team_count`, the pool of
   that country's remaining league-less clubs fills places; if the pool is
   exhausted, fresh AI clubs are generated (the `SeedWorld`
   membership-write loop: `hashMix(worldSeed, league)` rng + name pools +
   `GenerateAIClub` + `role='league'` row) until the league composes exactly.

With coherent adjacency, `stayers + promotions + relegations == oldN`, so the
only free degrees are the declared adds and the top-up — the engine can always
reach exactly `team_count`. A residual mismatch becomes `errInternalRollover`
(membership over-stream can no longer happen by construction; see *Add-cap
arithmetic*).

### `ErrOddMemberCount` (StartSeason hardening)

`startSeason` now rejects an odd member count up front
(`ErrOddMemberCount`) instead of reaching `roundRobin`'s odd-count padding
panic (clubnames.go:69) — there is existing IM10 hardening in this exact area,
so this file's odd/even audit is deliberate.

## Architecture / recorded notes

Root cause that motivates auto-adjust: adjacency is a **constraint on pairs**
(`above.Relegations == l.Promotions` and `below.Promotions == l.Relegations`).
A strict-refusal policy means a promoted/relegated league can only change if
its neighbour changes *first*, and a two-neighbour loop can never make progress
— so the admin interface must be able to move both sides of an edge in one
call. Auto-adjust is the minimal shape of that "one call".

### Add-cap arithmetic

In a coherent ladder the rollover satisfies `stayers + streams == oldN`. During
a season, a league's next composition is at most:

```
oldN - (streamed out) + (streamed in) + declared adds   <=  oldN + declared adds
```

declared adds are capped by `team_count - realSize - pendingPendingAdds`, and
`oldN <= team_count` for an existing league, so
`realSize + pending + 1 <= team_count` makes over-subscription structurally
impossible; the rollover's `len > TeamCount` branch remains a belt-and-braces
internal error, never an admin-visible 4xx.

### "Applies next season" is the unifying rule

- No-season league (never rolled over): composed at `StartSeason` from declared
  members only; capacity changes bind at first rollover.
- Live-season league: adds and capacity changes bind at rollover.
- Documented everywhere as: **"capacity/membership changes apply from the
  season AFTER the current one."**

## Changes

### internal/competition

- `membership.go` (new): `AddClubToLeague`, `SetLeagueCapacity`,
  `pendingDeclaredMembers` / `clubLeagueLessCandidates` queries,
  `addClubToLeague` shared writer, `ErrLeagueFull`, `ErrLeagueShrink`,
  `ErrAdjacencyMismatch`, `ErrClubAlreadyInLeague`, `ErrInvalidCounts`,
  `ErrInvalidTeamCount` (sentinels live in `service.go`).
- `rollover.go`: second composition loop — merge declared members, top-up from
  pool candidates, then generate AI clubs to exact `team_count`.
- `seeding.go`: `StartSeason`/`startSeason` odd-count rejection.
- `service.go`: new error sentinels; `AddClubToLeague` / `SetLeagueCapacity`
  methods.

### internal/httpapi + openapi

- `membership_handlers.go` (new): `handleAdminAddClubToLeague`,
  `handleSetLeagueCapacity` (inline request structs, PATCH/POST bodies like
  `leagueReputationRequest`).
- `router.go` (admin group near the club-rename route):
  `POST /api/admin/worlds/:id/countries/:countryID/clubs/:clubID/league`,
  `PATCH /api/admin/leagues/:id/capacity`.
- `openapi.yaml`: both routes + request/response schemas + `AdminCountryID`
  param usage; must satisfy `TestDocsCoverRouter`/`TestDocsOpenAPIValid`.

### Integration tests (`membership_integration_test.go`, `//go:build integration`)

- No-season add → `StartSeason` composes the declared member in.
- Mid-season: resize and add clubs, play the full existing season via
  `GetFixtures` + `ApplyResult` → next season is exactly the new sizes; assert
  the `competition_entries` (declared + promoted-in), the ladder's
  promotions/relegations rows, and both audit events.
- Auto-fill: a league running short tops up from a generated league-less club
  pool, then generates AI clubs when the pool is exhausted.
- Refusals: `ErrOddMemberCount`, `ErrLeagueShrink`, `ErrInvalidCounts`,
  promote-without-link, add-over-capacity, already-leagued, cross-world.

### Docs (this sprint's emphasis — user directive "heavily documented")

- `docs/tasks/improvements/IM14-league-membership-add-and-capacity-resize.md`
  (this file).
- `docs/how-to/setup-and-launch.md`: admin flows for both operations with the
  "applies next season" framing and auto-fill/auto-adjust behaviour.
- `docs/how-to/seasons.md`: composition correction — it previously implied
  rollover = standings only; now standings + declared members + auto-fill.
- `docs/product_manager.md`: recorded decisions (upward-only capacity;
  reciprocal auto-adjust; declared-members-first then pool then AI creation;
  applies-next-season).

## Recorded decisions

- **Declared members first.** The admin's explicit adds sort ahead of any
  automated fill; auto-fill (pool, then AI creation) only tops up to
  `team_count`.
- **Upward-only capacity.** `SetLeagueCapacity` never lowers `team_count`;
  downward movement is out of scope (shrinking a composed pyramid is never
  coherent). The add-cap rule makes over-subscription structurally impossible
  anyway.
- **Auto-adjust reciprocal neighbours.** Instead of strict refusal (which
  deadlocks under single-league edits), the neighbour's missing reciprocal
  count is written in the same transaction and the whole ladder re-validated —
  then all-or-nothing.
- **Auto-normalise country.** On add, the club's country text is set to the
  league's country name (previous session's locked decision), keeping the
  seed-status `country`-based checks coherent.
- **Every change is an audit event** (`CLUB_JOINED_LEAGUE`,
  `LEAGUE_CAPACITY_CHANGED`) through `recordSeedEvent`, same channel as
  seeding/rollover writes.
- **Odd member count is a 4xx, not a panic.** `startSeason` rejects it up front
  (`ErrOddMemberCount`).

## Known limitations (pre-existing, documented — not fixed here)

- `startSeason` lacked an even-count check and panicked through `roundRobin`
  padding (clubnames.go:69); IM14 hardens it.
- `bootstrap/service.go:179` hardcodes the "england" seed pool; fine for the
  single-country seed shape today.
- `club_competitions` `role='league'` rows are one-time per club for ever
  (`uq_club_one_league`); live placement is therefore derived from
  latest-season `competition_entries`, and "league-less" means "no
  `role='league'` row". This diverges from the "per-season membership" reading
  in glossary.md — the glossary is corrected as part of this task.

## Tests

- Integration (CI-only, live Postgres at `TEST_DATABASE_URL`): scenarios in
  *Integration tests* above, reusing the `seedWorld` + `GetFixtures` +
  `ApplyResult` loop harness (a 4-team two-tier country = 24 fixtures: 6 rounds
  × 2 leagues × 2 fixtures).
- `TestDocsCoverRouter`, `TestDocsOpenAPIValid` keep the two routes and schemas
  gated.

## Verification (run from `backend/`)

- `gofmt -w` on every touched Go file.
- `go build ./...`, `go vet ./...`, `go vet -tags integration
  ./internal/... ./pkg/...`, `go test ./...`.

## Delivery evidence

### Files

- `backend/internal/competition/membership.go` (new) — `AddClubToLeague`,
  `SetLeagueCapacity`, `pendingDeclaredMembers`, `clubLeagueLessCandidates`,
  the shared add writer, and ladder dry-run/re-validation helpers.
- `backend/internal/competition/rollover.go` — membership-merge + pool/AI
  top-up in the second composition loop.
- `backend/internal/competition/seeding.go` — odd-count guard in `startSeason`.
- `backend/internal/competition/service.go` — new sentinels and service methods.
- `backend/internal/httpapi/membership_handlers.go` (new) + `backend/internal/httpapi/router.go` —
  the two admin routes.
- `backend/internal/apidocs/openapi.yaml` — route + schema mirror.
- `backend/internal/competition/membership_integration_test.go` (new,
  `//go:build integration`).
- Docs: this file, `docs/how-to/setup-and-launch.md`, `docs/how-to/seasons.md`,
  `docs/product_manager.md`, `docs/how-to/glossary.md`.

### Verification

- `gofmt -w` clean; `go build ./...` clean; `go vet ./...` clean;
  `go vet -tags integration ./internal/... ./pkg/...` clean; `go test ./...`
  green (incl. apidocs gates and prior suites).
- Integration suite compiles under the `integration` tag; the three scenarios
  exercise add/resize/refuse in one country ladder.