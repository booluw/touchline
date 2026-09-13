# P0 — Separate calendar advancement from aggregate world ticks

## Context

OPD-17 defines five independently scheduled world tick granularities: `hourly`, `daily`, `weekly`, `monthly`, and `seasonal`. The matchday runner is intended to consume daily ticks and determine which fixture dates are due.

## Evidence

- `backend/internal/world/service.go` seeds all five schedules. The defaults include hourly (`0 * * * *`), daily, weekly, monthly, and seasonal schedules.
- `backend/internal/scheduler/service.go`, `FireTick`, increments the single `world.worlds.current_tick` for **every** granularity.
- `backend/internal/matchday/runner.go`, `worldDate`, reads that single value and returns `launch_date + current_tick days`.
- `backend/internal/matchday/runner.go`, `KickoffDue`, calls `worldDate`; `dueMatchdays` then starts every fixture whose scheduled calendar day is no later than this calculated date.

## Failure mode

Under the default schedule, `current_tick` rises hourly, not daily. After one real day, it has increased for roughly 24 hourly ticks plus the daily tick (and may also include weekly/monthly/seasonal ticks). The match runner therefore treats one real day of world time as roughly 25 game days and can release many scheduled matchdays on a single daily handler invocation. The no-overlap gate delays their execution but does not restore the intended calendar.

Changing an hourly or other non-daily cadence changes fixture timing, even though these engines should be independent. This makes season pacing depend on unrelated scheduler configuration and undermines deterministic calendar semantics.

## Requested engineering decision

Establish a canonical world-calendar representation whose advancement is exclusively tied to the intended calendar cadence. Keep a distinct monotonic sequence for event ordering/audit if needed. Define the behavior of skipped, repeated, paused, resumed, and changed daily schedules before changing the schema or runner.

## Acceptance evidence to add

- A test with all default granularities enabled showing that one daily calendar step advances fixture eligibility by exactly one intended game day.
- A test showing hourly/weekly/monthly/seasonal events do not change match due-date calculation.
- Pause/resume and cadence-change tests that preserve the chosen calendar contract.

