# IM24 — Player lifecycle runs once per season

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (player lifecycle integrity)
**Source:** Doc-vs-code audit against `backend/docs/design/player-lifecycle.md`
(retirement once per season) and `docs/how-to/cadences-and-time.md` (the day-364
fallback is for **league-less** worlds only). Two defects:
1. `lifecycle.OnSeasonCompleted` runs the **world-wide** retirement pass, but
   its idempotency guard is keyed per country. Each country's `SEASON_COMPLETED`
   therefore re-ran retirement over the whole world: a world with N countries
   gave every veteran N retirement rolls per season.
2. The worker's seasonal fallback (`day % 364 == 0`) ran for every world, so a
   world with leagues got a second world-wide intake + retirement pass on top of
   its countries' own rollovers.
**Depends on:** A06 aging/retirement, S08-01 academy intake, IM23 (stamped day).

## What to do

- Run retirement only on the first lifecycle rollover of a `(world, season)`,
  whatever its scope; later country rollovers keep their intake, pool
  replenishment and AI auto-fill but skip retirement.
- Serialize concurrent rollovers of the same season with a transaction-scoped
  advisory lock so two countries completing at once cannot both retire.
- Skip the day-364 fallback when the world has any league.

## Delivery evidence

### Backend

- `backend/internal/lifecycle/service.go` — new `retirementDone(ctx, tx, world,
  season)`: takes `pg_advisory_xact_lock(hashtext('touchline:lifecycle:<world>:<season>'))`,
  then checks for any `WORLD_LIFECYCLE_SEASON_COMPLETED` of that season.
  `OnSeasonCompleted` retires only when none exists (`Result.Retired` is 0
  otherwise).
- `backend/internal/app/worldtick.go` — `runSeasonal` returns early when
  `competition.competitions` has a `league` row for the world.

### Tests

- `backend/internal/lifecycle/service_integration_test.go` —
  `TestLifecycleRetiresOncePerSeasonAcrossCountries`: the second country's
  rollover in the same season retires nobody and leaves fresh veterans active.
- `backend/internal/app/worker_integration_test.go` —
  `TestSeasonalFallbackSkipsLeagueWorlds` (league world: no world-wide lifecycle
  event on day 364) and `TestSingleDailyCadenceDispatchSeasonalFallback`
  (league-less world: exactly one).

### Verification

See [IM29](IM29-single-backend-image-deploy.md#verification).

## Recorded decisions

- **Retirement is a world-wide, once-per-season pass.** It keys on the season
  alone; intake/replenish/auto-fill stay per country.
- **Back-compatible guard.** Any existing lifecycle event of the season counts
  as "retirement ran" (every pre-IM24 rollover did run it).
- **League worlds never use the fallback.** Their `SEASON_COMPLETED` events are
  the only lifecycle driver, as the cadence doc already stated.
