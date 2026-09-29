# IM23 — Day-stamped world ticks and a Redis-proof daily dispatch

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (world clock / worker integrity)
**Source:** Doc-vs-code audit of the backend against `docs/how-to/cadences-and-time.md`,
OPD-24 (one daily emission ⇔ one game day) and OPD-42 (catch-up in bounded
passes). Two defects:
1. A catch-up pass commits up to `maxDaysPerFire` (7) day increments in one
   transaction and emits one `WORLD_TICK{daily}` per day, but the emission did
   not say *which* day it was. The worker re-read `world.worlds.current_day` when
   it processed each emission — by then already the batch's last day — so every
   emission of the batch gated on the same day: week/month/season boundaries
   inside a batch were skipped, or ran up to 7 times when the batch ended on one.
2. The worker returned early (and marked the event handled) when the realtime
   `world_tick` push to Redis failed, silently skipping that game day's wages,
   training, market and kickoffs. The realtime push is documented best-effort.
**Depends on:** IM02 single daily cadence, IM16 fixed-scale clock + rollover
(`scheduler.rolloverDays`), the worker's `handleWorldTick`.

## What to do

- Stamp every daily emission with the calendar day it advanced to:
  `WORLD_TICK` payload `{"granularity":"daily","day":<current_day>}`.
- Gate the worker's daily/weekly/monthly/seasonal passes on the payload day.
  Emissions from before IM23 carry no `day` and fall back to the counter.
- Make the realtime push best-effort: log a build/publish failure and run the
  day's gameplay anyway.

## Delivery evidence

### Backend

- `backend/internal/scheduler/service.go` — `rolloverDays` reads
  `RETURNING current_tick, current_day` and writes `"day"` into each emission's
  payload; `FireTick` doc updated.
- `backend/internal/app/worldtick.go` — `handleWorldTick` uses `tickDay(payload)`
  (new) for every day gate, falling back to `World.Calendar`'s counter;
  `runSeasonal` + `worldSeasonAt(day)` derive the season and reference date for
  the stamped day, not the counter.
- `backend/internal/app/worker.go` — `subscribeWorldTick` logs
  `BuildWorldTick`/`Broker.Publish` failures and always continues to
  `handleWorldTick`.

### Tests

- `backend/internal/app/worldtick_test.go` — `TestTickDay` (stamped day,
  day 0, pre-IM23 payload, malformed payload).
- `backend/internal/app/worker_integration_test.go` —
  `TestDailyDispatchUsesStampedDayOnCatchUp` replays five catch-up batches with
  `current_day` already at each batch's end: weekly pass stamps day 28, wages
  post once (tick 30), one monthly board review.
- `backend/internal/scheduler/service_integration_test.go` —
  `TestFireTickRollsOverMissedDays` asserts emissions 1..30 carry `day` 1..30;
  `TestFireTickAdvancesCounterAndRecordsPayload` expects
  `{"day": 1, "granularity": "daily"}`.

### Verification

See the IM23–IM29 verification block in
[IM29](IM29-single-backend-image-deploy.md#verification) (shared run).

## Recorded decisions

- **The emission carries the day; the counter is not re-read.** The catch-up
  batch is one transaction, so the counter cannot tell emissions apart.
- **Realtime never gates gameplay.** A failed `world_tick` socket push is logged;
  the daily dispatch still runs (the REST reads stay authoritative, OPD-19).
- Legacy emissions without `day` keep the pre-IM23 behaviour (counter).
