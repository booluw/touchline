# IM01 — Season lifecycle: admin start endpoint + configurable off-season + automatic next-season start

**Status:** Not started
**Sprint:** Improvements (season lifecycle)
**Source:** Product decision (manual session)
**Depends on:** S04-01 (StartSeason seam + rollover); S05-02 (ledger/wages); S08-01 (academy)

## What to do

Make the season lifecycle admin-controllable and automatic:

1. **First season starts via an HTTP endpoint** (today there is only the
   `competition.Service.StartSeason` service seam — no route).
2. **Later seasons auto-start after an admin-set off-season measured in daily
   world ticks.** The admin sets how long the break between seasons lasts;
   wages, expenses, and the (always-open) transfer market keep running through
   the break so managers can prepare their team.
3. Make the `upcoming` season status honest: a season created at rollover
   flips to `in_progress` (and emits `SEASON_STARTED`) on its first fixture
   day.

Decisions locked (manual session): the off-season gap is **read at season end**
(the rollover anchors the next season's fixtures at `lastFixtureDay + gap`);
the gap is a **world-wide config with a per-league override**.

## Behaviour

### New endpoint — start the first season

`POST /api/admin/worlds/:id/leagues/:leagueID/season` (admin-scoped)

Wraps the existing `competition.Service.StartSeason(ctx, worldID, leagueID)`.

- `201` → `{id, competition_id, season_label, season_number, status}` with
  `status: "in_progress"` for season #1 (existing `createSeason` behaviour).
- Error → HTTP map:
  - `404` `ErrWorldNotFound`, `ErrCompetitionNotFound`
  - `409` `ErrCompetitionWorldMismatch`, `ErrLeagueAlreadySeeded`,
    `ErrCompetitionNotSeeded`
- A season-1 start requires the league to be **seeded** (clubs present);
  seeding an unseeded league is refused (`ErrCompetitionNotSeeded`).

### Off-season gap (config)

- **World default:** `season.off_season_ticks` in `world.world_config`
  (`defaultConfigKeys` seed, default **30**). Admin changes it any time via the
  existing `POST /api/admin/worlds/:id/config`.
- **Per-league override:** `competition.competition_rules.scheduling_rules ->> 'off_season_ticks'`
  wins when present.
- Read precedence: per-league override → `world_config` row → compiled default
  (30). Read **inside the rollover transaction** so the value is stable with
  the season-end cascade.

### Rollover change

`internal/competition/rollover.go` `rolloverCountry` currently anchors the
next season at `lastScheduledDay` (the day after the final fixture,
`rollover.go:114-123`). Change to:

- `gap := offSeasonTicks(ctx, tx, leagueID, worldID)` (per-league override then
  world config then default).
- `anchor = lastScheduledDay + gap` days.
- Pass `anchor` as the `bootRef` to `createSeason` (serves as the next
  season's `start_date`) and `createFixtures`, so its first fixture lands on
  `anchor + 1` — i.e. **no fixtures due during the off-season**.

Keeps `SEASON_CREATED`/`SEASON_COMPLETED` event cadence unchanged (they fire at
rollover as today; `SEASON_COMPLETED` still drives academy intake).

### Season status flip

New `competition` service step, called from the worker's **daily** tick handler
before `KickoffDue`:

- For every league whose `upcoming` season has a scheduled fixture with
  `scheduled_at::date <= world_date` (the world's `current_day`), set the
  season `status = 'in_progress'` and emit `SEASON_STARTED`
  (`{competition_id, season_id, season_number, season_label}`), all inside the
  same transaction as the `KickoffDue` pass (idempotent: guard on
  `status = 'upcoming'`).
- Season #1 is created `in_progress` directly, so the step only touches
  rollover-created seasons.

### Off-season continuity (already true, now asserted)

- `Finance.ApplyMonthlyWages` and `Academy.Maintenance` have no season gating —
  they keep posting through the off-season (see IM02 for the trigger change).
- The transfer market is not gated by season (`transfer-market.md`) — it stays
  open. Add regression tests documenting this.

## Changes

### internal/competition

- `service.go`: add `offSeasonTicks(ctx, tx, leagueID, worldID) (int, error)`
  (override → world config → default) and `ActivateDueSeasons(ctx, worldID)
  (int, error)` (upcoming→in_progress + `SEASON_STARTED`).
- `rollover.go`: use the gap when computing the next-season anchor.
- `seeding.go`: leave `StartSeason`/`createSeason`/`createFixtures` signatures
  intact (they already accept an anchor `time.Time`).

### internal/httpapi

- `router.go` admin group: `admin.POST("/worlds/:id/leagues/:leagueID/season", …)`.
- New `handleStartSeason` mapping the sentinel errors above.

### internal/world

- `service.go` `defaultConfigKeys`: add `"season.off_season_ticks": 30`
  (seeded at launch, `ON CONFLICT DO NOTHING` so existing worlds keep theirs).

### internal/app

- `app.go` daily `WORLD_TICK` handler: call `CompSvc.ActivateDueSeasons` before
  `Runner.KickoffDue` (order matters so the season reads `in_progress` before
  its matches simulate).

## Tests

- `internal/competition` integration: start season #1 via service (endpoint
  covered at `cmd/api`) → `in_progress`; duplicate → `ErrLeagueAlreadySeeded`;
  unseeded league → `ErrCompetitionNotSeeded`.
- Rollover with `season.off_season_ticks = 20` → next season `start_date` and
  first fixture day are `lastFixtureDay + 20 + offset`; **no fixture has
  `scheduled_at` inside the gap**; `ActivateDueSeasons` flips it exactly on the
  first fixture day and not before.
- Per-league override beats world config.
- Off-season continuity: during a gap window, `ApplyMonthlyWages` and
  `Academy.Maintenance` still post (dedup-keyed) and listings/bids still work.
- Replay determinism: the same world seed yields identical fixture days with
  the gap applied.
- `cmd/api` integration: `POST /api/admin/worlds/:id/leagues/:leagueID/season`
  happy path + 404/409 branches.
- `internal/world`: `defaultConfigKeys` seeds `season.off_season_ticks`.

## Docs

- `docs/tasks/README.md`: add sprint-map row 19 "Improvements
  (IM01–IM03)" pointing at this folder.
- `docs/how-to/seasons.md`: off-season, `season.off_season_ticks` + override,
  `SEASON_STARTED`, automatic subsequent-season start, admin start endpoint.
- `docs/how-to/setup-and-launch.md`: Step 7 rewrites "start a season" as the
  admin endpoint (keep the seam reference); step 9 note that the off-season
  keeps wages/market running; appendix adds the endpoint.
- `docs/how-to/glossary.md`: "off-season", "season status (upcoming)",
  "season.off_season_ticks" terms.
- `internal/apidocs/openapi.yaml`: document the start-season endpoint.

## Recorded decisions

- Off-season gap is read **at rollover** (not mid-off-season): the admin sets
  `season.off_season_ticks` before the final matchday; the value is stable and
  replay-deterministic.
- Default gap is **30 daily ticks**; per-league override lives in the existing
  `competition_rules.scheduling_rules` JSON (no migration).
- Season #1 stays `in_progress` on creation (existing behaviour); the flip
  step only affects rollover-created `upcoming` seasons.