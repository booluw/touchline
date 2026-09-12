# S02-03 — Implement the configurable world clock

**Status:** Done  
**Sprint:** 02 — Authenticated, schedulable worlds  
**Source:** PRD §4; technical plan §§5, 16, 18  
**Depends on:** S01-02

## What to do

Implement the scheduler service using runtime `world_config` cadence values. Publish world-scoped `WORLD_TICK` events, including granularity, without embedding world cadence in code.

## Acceptance criteria

- Scheduler reads tick cadence configuration per world and emits world-scoped `WORLD_TICK` events through the event bus.
- Tick payload identifies granularity sufficient for engine subscriptions.
- A cadence change takes effect without an application redeploy.
- Engines can subscribe/dispatch only the tick granularities they own.
- Integration coverage demonstrates a configured daily tick arriving at a worker handler for the intended world.

## Delivery evidence

- **AC1 — scheduler service + events:** `internal/scheduler` (`service.go`) reads each playable world's `tick.*_cadence` values from `world.world_config` and registers them on `robfig/cron`; `FireTick` emits `WORLD_TICK` via a `Publishable` bus in the record-then-publish pattern, advancing `world.worlds.current_tick` in the same transaction. Single-leader `pg_advisory_lock(hashtext('touchline:scheduler'))`; malformed specs skipped + logged. `cmd/scheduler` rewritten to run `Service.Run(ctx, poll)` with river publish + health listener.
- **AC2 — granularity payload:** payload JSON `{"granularity": "<hourly|daily|weekly|monthly|seasonal>"}`; `WORLD_TICK` written to `world.events` (actor `system`); worker handler unpacks it. `TestFireTickAdvancesCounterAndRecordsPayload` asserts rows + payload per granularity.
- **AC3 — runtime cadence change, no redeploy:** cadence values live in `world_config`, re-read every `SCHEDULER_POLL_INTERVAL` (default 15s); admin `POST /api/admin/worlds/:id/config` upserts a value (`world.Service.SetConfig`). `TestConfiguredDailyTickArrivesAtWorker` re-syncs a changed spec; HTTP test covers 200/403/404 + stored JSONB.
- **AC4 — engines subscribe per granularity:** `granularityFromKey` maps only `hourly|daily|weekly|monthly|seasonal` (excludes `tick.match_cadence` — owned by the match engine's per-match goroutines, S04, per OPD-17); subscribers (worker handler) filter on payload granularity.
- **AC5 — integration proof:** `TestConfiguredDailyTickArrivesAtWorker` seeds `tick.daily_cadence`, runs the real sync loop, fires the registered daily spec, asserts a `WORLD_TICK` delivered on the real river bus to a fake worker handler for the intended world, then pauses the world and asserts unregistration. Unit coverage: `TestGranularityFromKey`, `TestReconcileRegistersUpdatesAndRemoves`, `TestReconcileSkipsInvalidSpec`. Full suite green under `-p 1 -tags integration -race`.

Recorded as **OPD-17** (configurable world clock) in `docs/product_manager.md`.
