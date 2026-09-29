# IM30 — Schema-drift query fixes found by running the integration suites

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (integration suite health)
**Source:** Running the `//go:build integration` suites against a real
Postgres 16 while verifying IM23–IM29 (the repo's docs say they only
compile-check outside CI). Several queries referenced columns that the schema
does not have, so the features behind them failed at runtime:
1. **World seeding** — `seeding.go` listed leagues without the `reputation`
   column that IM06 added to the shared `scanLeagues` (16 destinations, 15
   columns): `SeedWorld` failed for every world with leagues.
2. **Weekly player pass** — `player.expectationRole` ordered
   `player.player_preferences` by a `created_at` column that table does not
   have. `Players.WeeklyTick` failed, so the worker's daily dispatch errored on
   every week boundary (day 7, 14, …) and river retried the whole day.
3. **Home dashboard** — four reads (incoming bids, expiring contracts, unhappy
   players, market events) selected `p.display_name` from `player.players`;
   display names live on `person.people`. The dashboard read and the worker's
   per-tick dashboard sweep failed.
4. **Transfer listings/bids** — the same `display_name` drift (fixed under IM26).
5. **Injury evaluation hung** — `injury.playerContextQuery` selected five
   columns while `loadPlayerContexts` scanned six (the player id first). The
   scan failed with the transaction left open, and the match/training injury
   paths (`TestPersistMatchRoundTrip`, `TestHTTPInjuryReadAndRushReturn`) hung
   until the test timeout.
**Depends on:** migrations `0004_person`, `0006_player`, IM06.

## What to do

Point each query at the real columns; change nothing else.

## Delivery evidence

- `backend/internal/competition/seeding.go` — league listing selects
  `c.reputation`.
- `backend/internal/player/store.go` — `expectationRole` orders by
  `strength DESC NULLS LAST, id` (the table has no timestamp; the strongest
  expectation wins deterministically).
- `backend/internal/dashboard/store.go` — the four reads join
  `person.people pp` and select `pp.display_name`.
- `backend/internal/injury/store.go` — `playerContextQuery` selects `p.id`
  first, matching the scan.
- `backend/internal/competition/weekdays.go` — `nextAllowedWeekday` treats an
  empty weekday set as unrestricted; season materialization called it for
  competitions with no weekday rule and looped forever
  (`TestNextAllowedWeekdayEmptySetIsUnrestricted`).

### Verification

See [IM29](IM29-single-backend-image-deploy.md#verification).

## Recorded decisions

- **The strongest playing-time expectation wins** when a player has several
  (the old "latest" ordering never ran, as the column does not exist).
- Integration suites should run against a real Postgres, not only compile —
  this class of defect is invisible to `go vet -tags integration`.
