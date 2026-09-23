# IM02 — Single daily cadence: day-derived weekly/monthly seasons and monthly board reviews

**Status:** Not started
**Sprint:** Improvements (world clock)
**Source:** Product decision (manual session)
**Depends on:** S02-03 (configurable world clock); S04-01 (daily kickoff);
S06-02 (board review); S08-01 (academy maintenance)

## What to do

Simplify the world clock to a **single daily cadence** and derive every other
periodic system from the day counter. Today the clock runs five cron
granularities (`hourly`, `daily`, `weekly`, `monthly`, `seasonal`);
weekly/monthly systems are wired to separate crons. After this task:

- Only `tick.daily_cadence` drives the world clock (plus `tick.match_cadence`,
  which is the live-match pacing, untouched and excluded from the clock).
- Days make up weeks and months: weekly work runs when
  `current_day % days_per_week == 0`, monthly work when
  `current_day % days_per_month == 0` — so the admin, "from the daily cadence",
  can tell how long a week/month/year is.
- **Board confidence reviews move from weekly to monthly** (once per configured
  month). The board *formulas* do not change (they grade by
  `played/totalMatches` progress, `evaluate.go`).

Decisions locked (manual session): daily default is **1 game-day per real day**
(`0 0 * * *`); periodic steps key off **`current_day % 7` / `current_day % 30`**
with new configurable keys.

## Behaviour

### Clock surface after the change

| Key | Default | Role |
| --- | --- | --- |
| `tick.daily_cadence` | `0 0 * * *` (was `0 */8 * * *`) | the only WORLD_TICK granularity the scheduler registers; each emission advances `current_day` by 1 |
| `tick.match_cadence` | `20s` | live-match pacing; **not** a world-clock cadence (unchanged) |
| `calendar.days_per_week` | `7` | weekly-work step |
| `calendar.days_per_month` | `30` | monthly-work step (board review, wages, academy maintenance) |
| `season.off_season_ticks` | `30` | see IM01 (rollover gap) |

Removed keys: `tick.hourly_cadence`, `tick.weekly_cadence`,
`tick.monthly_cadence`, `tick.seasonal_cadence`.

### Day-derived dispatch (worker)

The `WORLD_TICK` handler for `granularity == "daily"` becomes the only handler.
After the existing always-on work (kickoff + live pacing, PolicyBot bid
responses, `Transfers.DailyTick`, dashboard sweep):

- **When `current_day % days_per_week == 0`** → the weekly block:
  `Policy.EnsureTraining`, `Training.ApplyWeekly`, `Players.WeeklyTick`,
  `Social.ReconcileRivalries`.
- **When `current_day % days_per_month == 0`** → the monthly block:
  `Finance.ApplyMonthlyWages`, `Academy.Maintenance`, and **`Board.WeeklyReview`
  (re-purposed as the monthly review)**.
- Daily tick ordering guarantees board review runs **after** wages/market and
  only once per month.

The seasonal cadence's only hook (leagues-less world lifecycle fallback via
`OnSeasonCompleted`) moves under the daily handler guard
`current_day % academy.DaysPerSeason == 0` (worlds with leagues already receive
`SEASON_COMPLETED`, which continues to drive it).

### Scheduler

- `scheduler.WorldClockGranularities` → `["daily"]`; `granularityFromKey`
  keeps excluding `match`.
- `FireTick` keeps advancing `current_tick` on every emission and `current_day`
  only for `daily` (unchanged). With one granularity, `current_tick` and
  `current_day` advance in lockstep (both +1 per daily tick).
- Cleanup migration deletes leftover `tick.{hourly,weekly,monthly,seasonal}_cadence`
  rows from `world.world_config` (they would otherwise be ignored — this is
  hygiene). Existing worlds unaffected behaviourally.

### Idempotency

Every moved block is already idempotent per tick (board snapshot
`(manager_id, world_tick)`; wage/academy dedup keys; training/player passes
guarded identically to today). The modulo gates make a caught-up world catch up
correctly: a world that ticks 30 days in one replay run performs its weekly work
on days 7/14/21/28 and monthly work on day 30.

## Changes

### internal/world

- `service.go` `defaultConfigKeys`: keep `tick.match_cadence` + `tick.daily_cadence`
  (new default `0 0 * * *`); add `calendar.days_per_week = 7`,
  `calendar.days_per_month = 30` (and `season.off_season_ticks = 30` per IM01);
  drop hourly/weekly/monthly/seasonal.

### internal/scheduler

- `service.go`: `WorldClockGranularities = []string{"daily"}`.

### internal/app

- `app.go`: collapse the `WORLD_TICK` handler to the daily path; read
  `world.worlds.current_day` plus the two `calendar.*` values (world config,
  fallback defaults) each pass; gate the weekly/monthly/seasonal-fallback blocks
  on `current_day % step`. Call `CompSvc.ActivateDueSeasons` before kickoff
  (IM01).

### migrations

- `0049_single_daily_cadence.up.sql` / `.down.sql`: delete the four removed
  `tick.*` cadence rows from `world.world_config`
  (`DELETE … WHERE config_key IN ('tick.hourly_cadence','tick.weekly_cadence','tick.monthly_cadence','tick.seasonal_cadence')`);
  down re-inserts them (restoring the old defaults). No schema change.

### internal/board

- Rename/re-word the public surface and comments `WeeklyReview` → `Review`
  (or keep `WeeklyReview` as an alias and document it as monthly-on-days) —
  do **not** change numerics or the `loadReview`/`evaluateAndRecord` pipeline.

## Tests

- `internal/scheduler` unit: only `daily` is registered from config; leftover
  `tick.weekly_cadence` rows are ignored; `match` still excluded.
- `internal/app`/worker integration: fire daily ticks and assert — weekly work
  on days 7/14/21/28 (e.g. training rows written, player weekly pass counters),
  monthly block (wages + academy maintenance + board snapshots) on day 30 and
  not before; the seasonal fallback fires at `days_per_season`. Assert board
  snapshots exist with gap 30 (monthly) not gap 7.
- Config variance: `calendar.days_per_month = 10` → monthly work on day 10/20/30.
- Migration `0049` up (rows gone) / down (rows restored with old defaults).
- Existing integration suites that pump cadences keep passing (they drive ticks
  directly, not crons).

## Docs

- `docs/how-to/cadences-and-time.md`: rewrite as **single daily cadence** —
  the daily tick, day-derived week/month (`calendar.days_per_week/month`),
  what runs when, and "from your daily cadence you know your month length".
- `docs/how-to/setup-and-launch.md`: Step 8 cadence-acceleration block now only
  references `tick.daily_cadence`; note board reviews are monthly.
- `docs/how-to/seasons.md`, `docs/how-to/glossary.md`: daily-tick / cadence /
  board-review entries corrected (remove hourly/weekly/monthly/seasonal
  cadence rows).
- `docs/design/board-numerics.md`: update the "weekly review" language to
  monthly-on-days (formulas untouched).

## Recorded decisions

- Week = `calendar.days_per_week` (7), month = `calendar.days_per_month` (30),
  both per-world config seeded at launch; no per-league override for these.
- `current_tick` remains the monotonic event-ordering counter; with one
  granularity it equals the day count in the common case, but code must keep
  using `current_day` for calendar maths.
- Board review is monthly by day step; its numerics are unchanged.
- Removed cadences become inert immediately (scheduler ignores them); the
  migration only cleans up the config rows.