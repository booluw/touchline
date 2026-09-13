# S04-04 — Close the engineering-review findings (event atomicity, calendar semantics, competition event spine)

**Status:** Done  
**Sprint:** 04 — Deterministic football competition  
**Source:** `docs/em/review-context.md` + `docs/em/{event-delivery-atomicity,world-calendar-tick-semantics,competition-event-spine}.md`  
**Resolves:** OPD-23 (event publication contract), OPD-24 (world calendar semantics)  
**Depends on:** S04-02, S04-03

## What to do

Close the three findings from the 2026-09-13 engineering review:

1. **P0 `event-delivery-atomicity.md`** — event recording and dispatch must be atomic with the business state they describe; no committed event may be permanently undispatched; publish errors never silently swallowed.
2. **P0 `world-calendar-tick-semantics.md`** — the match calendar must advance only on the daily cadence, decoupled from the monotonic tick counter; target real-life pacing ≈ 3 in-game days per day.
3. **P1 `competition-event-spine.md`** — competition lifecycle events must conform to the same durable publication contract (persisted IDs, dispatch, explicit failure policy).

## Acceptance criteria

- **Calendar (OPD-24):** with all five default granularities enabled, one `daily` step advances fixture eligibility by exactly one in-game day; hourly/weekly/monthly/seasonal emissions never change matches-due; pause/resume and daily cadence changes preserve the contract (rate-only). `world.worlds.current_day` is the canonical day counter; `current_tick` remains monotonic ordering.
- **Atomicity (OPD-23):** every producer writes state + `world.events` row + River job in one transaction via `PublishTx`; fault-injection proves a failed enqueue rolls back the whole state change; the repair sweep re-dispatches a committed-but-undispatched event by its original ID exactly once (idempotent); the `event_repair_sweep` log is the operational signal.
- **Competition spine (P1):** seed + rollover events create expected rows AND dispatch the same persisted IDs to a subscriber; a forced event-write failure prevents (aborts) the associated competition state change; replay/audit covers `SEASON_COMPLETED`, promotion/relegation movement, and the next `SEASON_CREATED` sequence; competition events stamp the real `current_tick`.

## Technical plan

### A. Calendar semantics

- Migrations `0032`: `ADD COLUMN world.worlds.current_day BIGINT NOT NULL DEFAULT 0`; backfill from `world.events` daily `WORLD_TICK` count; down drops the column.
- `internal/world/service.go` `defaultConfigKeys`: `tick.daily_cadence` default → `0 */8 * * *`.
- `internal/scheduler/service.go` `FireTick`: when `granularity == "daily"`, `current_day = current_day + 1` in the same tx as `current_tick`.
- `internal/matchday/runner.go` `worldDate`: use `current_day`, not `current_tick`.
- Update tests: scheduler counter test, matchday `advance` helpers; add "one daily step = one matchday" + granularity-neutrality + pause/cadence-change coverage.

### B. Event publication contract

- `pkg/eventbus`: add `PublishTx(ctx, tx, *Event)` to the interface; implement in `RiverBus` (event row INSERT `RETURNING id, occurred_at` + River `InsertTx` in caller's tx); refactor `Publish` to `Begin → PublishTx → Commit`.
- Migrate all producers to `PublishTx` inside their state tx and delete bespoke record-then-publish code: scheduler, world (`SetStatus`/`CreateWorld`), bootstrap, manager (remove `_ = publish` swallow), match (`PlayFixture`, `kickoffFixture`, `Finalize`), competition (item C).
- New `internal/eventoutbox` repair sweep: worker goroutine re-enqueues committed-but-undispatched rows by original ID; logs `event_repair_sweep`.
- Tests: fault-injection rollback per producer; sweep recovery of the identical original ID; idempotent second sweep; delivered-event coverage.

### C. Competition event spine

- Replace `recordSeedEvent` with the shared publish path: `RETURNING id, occurred_at` captured, real `current_tick` stamped, dispatch via `PublishTx` in the same tx.
- Rollover `_ = recordSeedEvent` → propagate errors (abort the `ApplyResult` tx with a clear message).
- Seeding returns/captures event IDs for replay assertions.
- Tests: delivered IDs == persisted IDs; ordered rollover sequence; forced event-write failure rolls back standings/rollover state.

## Delivery evidence

### A. Calendar semantics (OPD-24) — implemented + automated
- `migrations/0032_world_calendar_day.up.sql`: `world.worlds.current_day BIGINT NOT NULL DEFAULT 0`, backfilled from the daily `WORLD_TICK` count; `down.sql` drops it. `internal/world/service.go` default `tick.daily_cadence` → `0 */8 * * *` (≈3 in-game days/real day).
- `internal/scheduler/service.go` `FireTick`: increments `current_day` only for `granularity == "daily"`, in the same tx as `current_tick`; no post-commit publish (event+job via `eventbus.WriteTx` in-tx).
- `internal/matchday/runner.go` drives fixtures from `current_day`; matchday `advance` helpers + `asOf` updated.
- Tests: `internal/scheduler/service_integration_test.go` asserts `current_day=1` after daily+weekly and `current_day` unchanged for hourly/weekly/monthly/seasonal (`TestFireTickAdvancesCalendarOnlyOnDaily`); `internal/matchday/runner_integration_test.go` (one daily step = exactly one matchday, 4 fixtures) + `runner_feed_integration_test.go` re-pass. Pause/cadence-change rate-only already covered by `TestFireTickSkipsNonPlayableWorlds` + `TestConfiguredDailyTickArrivesAtWorker` ✓.

### B. Event↔dispatch atomicity (OPD-23) — implemented + automated
- `pkg/eventbus` contract: `Publisher.PublishTx(ctx, tx, *Event)`; `EventBus` interface includes `PublishTx`. `RiverBus.PublishTx` = `RecordTx` (INSERT `world.events` `ON CONFLICT (id) DO NOTHING`, `RETURNING id, occurred_at`, `normalizeEvent` fills zero ids/timestamps) + `client.InsertTx` (`EventJobArgs`, unique-by-args) in the caller's tx. `PublishTx` external wrapper = `Begin → PublishTx → Commit`.
- All producers migrated to in-tx `PublishTx` (scheduler, world, bootstrap, manager, match service+live, competition) — no post-commit publish, no swallowed errors (manager's `_ = publish` removed; rollover `_ = recordSeedEvent` now propagates and aborts). API-side `bootstrap.GenerateAIClub` signature now takes `pub eventbus.Publisher`.
- `internal/eventoutbox` repair sweep: `Sweep` re-enqueues `world.events` rows lacking a `river.river_job` dispatch (`args->>'event_id'`) by their **original id**, cooperative batch `Limit`, `OldestLagSeconds` signal. `cmd/worker/main.go` runs it on a 60s ticker (`EVENT_REPAIR_SWEEP_INTERVAL`) and logs `event_repair_sweep scanned=… repaired=… oldest_lag_s=…`. `RiverBus.EnqueueRepair` verifies the event exists (rejects missing) and is idempotent by construction.
- Tests: `pkg/eventbus/outbox_integration_test.go` (poisoned tx → no event row *and* no job; clean commit → both; repair replays original id once, second repair no-op; missing event errors); `internal/eventoutbox/outbox_integration_test.go` (re-enqueue once, batch limit 2/2/1, oldest-lag signal); fault-injection per producer: `internal/manager/service_integration_test.go::TestAcceptJobOfferEventFailureRollsBackState` (manager stays inactive, club stays AI, offer `proposed`, no history, no events) and `internal/competition/outbox_integration_test.go::TestSeedCompetitionEventFailureAbortsState` / `TestRolloverEventFailureAbortsSeasonCompletion` (last fixture unapplied, season open, no `SEASON_COMPLETED`).

### C. Competition event spine (P1) — implemented + automated
- `recordSeedEvent` → shared publish path (`eventbus.WriteTx`), captures `RETURNING id`, stamps the **real** `world.worlds.current_tick` read in-tx, defaults actor to `system`; seeding and rollover call sites use it.
- Rollover emits (`emitSeasonCompleted`/`emitClubMoved`/`emitNextSeason`) return and propagate errors — a failed write aborts the `ApplyResult` tx.
- Tests: `TestCompetitionEventsDispatchPersistedIDs` drives seed + full rollover on a real river bus and asserts delivered `node` ids equal the persisted set with exact per-type counts (`COMPETITION_SEEDED` 1, `SEASON_CREATED` 4, `SEASON_COMPLETED` 2, `CLUB_PROMOTED` 1, `CLUB_RELEGATED` 1); fault-injection tests above prove abort-on-failure.

### Verification
- `go build ./...` ✓, `go vet ./...` ✓.
- `go test -p 1 -tags integration -count=1 -timeout 600s ./...` ✓ (whole suite green on local Postgres `:55432`).
- Local docker/CI: unavailable on the dev machine (OPD-14); full-stack compose + CI gate re-run pending CI. Open follow-up: wire a real bus into `cmd/api` (API-originated calls are log-only today via the `WriteTx` nil-publisher fallback).
- No commits made.