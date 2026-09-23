# How to: cadences and game time

How the game clock works: Touchline runs a **simulated calendar** driven by
real-world cron schedules called **cadences**. This document explains how the
clock works, one real day at a time (single-daily cadence, IM02), and how to
tune it.

Relates to: [setup-and-launch.md](setup-and-launch.md) (launching + accelerating
a world), [seasons.md](seasons.md) (what the matches produce),
[transfer-market.md](transfer-market.md) (daily market maintenance).

**In short:** a *daily* tick advances the in-game calendar by one game day and
kicks off matches; **weeks and months are derived from the day counter**, not
from their own schedules — a 7-day week and a 30-day month divide the day
number. Wages, academy costs and the board review run on day-boundaries;
training and player state run every 7th day; live matches pace themselves on a
separate `match` cadence.

---

## 1. The cadence model

Every playable world stores its cadence and calendar tuning as runtime config
keys (`world.world_config`). The scheduler runs **one cron entry per playable
world for the daily cadence only** (IM02 collapsed the old hourly/weekly/monthly/
seasonal entries into the single daily clock), re-reads the config every poll
(~15s), and fires a `WORLD_TICK{daily}` event on the bus. Paused and archived
worlds never tick.

Only `WORLD_TICK{daily}` exists — every tick advances the calendar by one game
day (§3). A `WORLD_TICK` event of any other granularity (a legacy event still
in flight from before IM02) is logged and ignored by the worker, so the 
day-derived passes can't double-post.

Registered config keys (from `internal/scheduler` with the defaults seeded at
world launch in `internal/world/service.go`):

| Key | Default | What it does |
| --- | --- | --- |
| `tick.match_cadence` | `20s` | live-match pacing, **not** a world tick |
| `tick.daily_cadence` | `0 0 * * *` | one game day per real day (midnight UTC) |
| `calendar.days_per_week` | `7` | days between weekly passes (training, player state, rivalries) |
| `calendar.days_per_month` | `30` | days between monthly passes (wages, academy, board review) |
| `season.off_season_ticks` | `30` | off-season gap between seasons, in game days (IM01) |

`match_cadence` is explicitly **excluded** from the scheduler (see
`internal/scheduler/service_test.go`) — it belongs to the live match engine
(`internal/match/live.go`).

## 2. What the daily tick drives on the gameplay side

The dispatch lives in the worker's `WORLD_TICK` handler
(`backend/internal/app/app.go`, extracted as `handleWorldTick`). In this order:

| When | Effect (in order) |
| --- | --- |
| **always** | (1) `Runner.KickoffDue` + live pacing of in-progress matches — but only in the worker that holds the match-runner lock; (2) `Policy.RespondToBidsForAbsent` — the PolicyBot answers bids for away-managed clubs before the market sweep; (3) `Transfers.DailyTick` — expire stale bids, recompute player valuations, and run AI buyer activity. |
| **every day where `day % days_per_week == 0`** (7/14/21/28…) | (1) `Policy.EnsureTraining` (bot writes plans before the pass); (2) `Training.ApplyWeekly` — apply the club's training archetype deltas + condition; (3) `Players.WeeklyTick` — morale recovery, playing-time shares, squad-role backfill, transfer-request assessment; (4) `Social.ReconcileRivalries` (backfill older fixtures). |
| **every day where `day % days_per_month == 0`** (30/60/90…) | (1) `Finance.ApplyMonthlyWages` — post `4 × weekly_wage` per active wage commitment (dedup-keyed, idempotent); (2) `Academy.Maintenance` — debit each club's `annual_cost / 12` facility cost; (3) `Board.Review` — score every managed club against its mandates, snapshot the seven factors, and sack human managers at/under the confidence threshold. |
| **every day where `day % 364 == 0`** (day 364, league-less worlds) | (1) seasonal fallback — drives the full player-lifecycle sweep (intake, retirement, pool replenish) once per season for worlds with no leagues; worlds with leagues ride their own `SEASON_COMPLETED` event instead. |

A day that is both a week and a month boundary runs weekly before monthly, so
the board grades post-wage books exactly once.

The home dashboard also receives a best-effort `dashboard_update` sweep once
per tick after the passes (`internal/app/app.go`, S07-01).

## 3. The calendar

- The world's clock is `world.current_day`, an integer day counter; the
  scheduler's workers also keep `world.current_tick` in lockstep with it.
- **Every `WORLD_TICK{daily}` advances it by one.**
- The season reference date is the world's `launched_at` (or `created_at`);
  matchday *n* plays on day *n*. With the default `0 0 * * *` daily cadence the
  world runs one game day per real day; a more frequent spec runs several.
- Weeks and months are *derived*: week *w* is `current_day / days_per_week`,
  month *m* is `current_day / days_per_month` (floor division of whole days).

Consequences:

- **Faster seasons** = accelerate the daily cadence (see §5). Every daily tick
  that lands on or past a scheduled matchday's date kicks that matchday off.
- The weekly/monthly passes don't have their own schedules — they fire when the
  day counter crosses the configured boundaries, so tuning `calendar.*` is a
  pure day-count change with no cron to reconcile.

## 4. Reading a `WORLD_TICK` event

Each tick writes a `world.events` row with `world_tick` (a monotonic per-world
counter) and payload `{"granularity": "daily"}`. Consumers (worker, tests,
dashboards) use the payload to confirm it is a daily tick; the worker then
reads the world's calendar config to decide which day-derived passes to run.

## 5. Tuning the cadences (admin)

Cadences and calendar tuning are runtime config, changed through the admin API:
`POST /api/admin/worlds/:id/config` with `{"key": "tick.<name>_cadence", "value":
"<cron spec>"}`.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.daily_cadence","value":"* * * * *"}'
```

Notes:

- The scheduler re-reads config on its next poll (~15s), so a change is picked
  up within one poll interval — **not** immediately.
- Setting `tick.match_cadence` does **not** change the world clock; it only
  changes how often the live-match engine advances a simulated minute
  (`internal/match/live.go`). The world clock deliberately ignores it (OPD-17).
- `calendar.days_per_week` / `calendar.days_per_month` (JSON numbers) retune the
  week/month boundaries: e.g. `{"key":"calendar.days_per_month","value":15}`
  posts wages every 15th game day.
- Paused/archived worlds don't tick regardless of cadence.
- A sub-minute or high-frequency daily cadence makes the dev loop fast, but keep
  in mind each daily tick also runs the transfer-market sweep, and each month
  boundary runs the board review for every managed club.

## 6. Common questions

**Is `hourly` still used?** No. The hourly/weekly/monthly/seasonal granularities
were retired in IM02 — the daily clock and its `calendar.*` steps replaced them.
A `WORLD_TICK` with a legacy granularity is logged and dropped.

**Which cadence starts a match?** `daily`. `KickoffDue` fires on the daily tick
for any matchday whose scheduled date has arrived. Live matches then pace
themselves on `match_cadence` (default 20s per simulated minute).

**At which point in a day do matches kick off?** Fixtures are day-gated, not
hour-gated: when the daily tick lands on the scheduled day, the matchday is
kicked. Scheduling constraints (≥2 game-days between a club's fixtures) come
from `StartSeason`/fixture generation — see [seasons.md](seasons.md).

**Why is the board review monthly now?** IM02 re-purposed it from a weekly cron
job to the day-30 pass so wage posting and the review share one day boundary,
and evaluation cadence becomes a live `calendar.days_per_month` knob. The scores
themselves are unchanged (graded by season progress; see
[board-numerics.md](../design/board-numerics.md)).

**Do the daily passes need a playable world?** Yes; the scheduler only registers
a cadence for playable worlds, and the worker dispatch filters on the same. This
is why the launch guide's Step 8 launches the world *before* the ticks matter.