# How to: cadences and game time

How the game clock works: Touchline runs a **simulated calendar** on a
fixed-scale continuous clock (IM16), driven by real-world cron **cadences**.
This document explains how the clock works and how to tune it.

Relates to: [setup-and-launch.md](setup-and-launch.md) (launching + accelerating
a world), [seasons.md](seasons.md) (what the matches produce),
[transfer-market.md](transfer-market.md) (daily market maintenance).

**In short:** every playable world has a **world clock** — a continuous time
line that runs at a fixed scale: at the default `tick.day_length` (one game day
per real day) world time is simply real UTC time, so a fixture at "Sep 27
20:00" really kicks off at 20:00. A *daily* tick rolls the world's **day
counter** (`current_day`) toward that clock's target — the weekly/monthly passes
and season activation read the counter. Live matches pace themselves on a
separate `match` cadence.

---

## 1. The cadence model

Every playable world stores its cadence and calendar tuning as runtime config
keys (`world.world_config`). The scheduler runs **one cron entry per playable
world for the daily cadence only** (IM02 collapsed the old hourly/weekly/monthly/
seasonal entries into the single daily clock), re-reads the config every poll
(~15s), and fires a `WORLD_TICK{daily}` event on the bus. Paused and archived
worlds never tick.

The daily emission **rolls the day counter toward the world clock's scale
target** (§3) — it no longer forces exactly one game day, it catches the
counter up. A `WORLD_TICK` event of any other granularity (a legacy event still
in flight from before IM02) is logged and ignored by the worker, so the
day-derived passes can't double-post.

Registered config keys (from `internal/scheduler` with the defaults seeded at
world launch in `internal/world/service.go`):

| Key | Default | What it does |
| --- | --- | --- |
| `tick.match_cadence` | `20s` | live-match pacing, **not** a world tick |
| `tick.daily_cadence` | `0 0 * * *` | how often the daily emission fires |
| `tick.day_length` | `86400` | the fixed world-clock scale: real seconds per game day |
| `calendar.days_per_week` | `7` | days between weekly passes (training, player state, rivalries) |
| `calendar.days_per_month` | `30` | days between monthly passes (wages, academy, board review) |
| `season.off_season_ticks` | `30` | off-season gap between seasons, in game days (IM01) |

`match_cadence` is explicitly **excluded** from the scheduler (see `internal/scheduler/service_test.go`) — it belongs to the live
match engine (`internal/match/live.go`).

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

## 3. The world clock and the calendar (IM16)

There are two time values:

- **`worldNow` — the continuous world clock.** Every playable world anchors a
  clock at **epoch = UTC midnight of `COALESCE(launched_at, created_at)`** and
  runs it at a fixed scale:
  `worldNow = epoch + (realNow − epoch) × 86400 / tick.day_length`. At the
  default `tick.day_length = 86400` (one game day per real day) the scale
  factor is 1, so **worldNow is exactly real UTC time** and a fixture scheduled
  at "Sep 27 20:00" matures at real 20:00. `tick.day_length = 1` compresses
  the world to one game day per real second for lab automation.
- **`current_day` — the day counter.** An integer `world.current_day`,
  advanced only by the daily emission. The scheduler polls playable worlds
  every ~15s and each daily fire **rolls the counter toward `floor(elapsed
  game-days)`** — the fixed-scale target — in bounded passes of **7 game-days
  per fire**, so a world that was paused or offline catches its calendar up
  without bursting (one emission ⇔ up to a few game-days when the whole target
  is within reach; the counter never overshoots the target, so left-fire
  redundant emissions are no-ops).
- Weeks and months are *derived* from the day counter: week *w* is
  `current_day / days_per_week`, month *m* is `current_day / days_per_month`
  (floor division of whole days).

Kickoffs read **`worldNow`**, not the counter: `KickoffDue` simulates a
matchday once `scheduled_at ≤ worldNow`, so a 15:00/18:00/20:00 kickoff
happens at that *moment*, not merely on that date. The worker runs an
intra-day kickoff poll (~15s cadence) on top of the post-daily-tick kickoff,
so a matchday kicks off within one poll interval of its scheduled time.

Consequences:

- **Match kickoff time is real.** At the default scale, a fixture's
  `scheduled_at` is a real wall-clock moment. Accelerating matches = shrinking
  `tick.day_length` (§5), not speeding up the cron.
- **The counter drives the day-derived passes** (weekly/monthly/seasonal) and
  season activation (`ActivateDueSeasons` watches `current_day`), so their
  cadence still follows the daily emission + catch-up rather than the raw
  clock. At default scale the catch-up keeps the counter essentially in
  lockstep with `worldNow`.
- **Pause/resume preserves history.** Pausing freezes the clock's rollover; on
  resume the counter and the clock resume from the present and any fixtures
  whose `scheduled_at` was missed are kicked off (fast-forward catch-up), one
  bounded pass per poll until caught up.

## 4. Reading a `WORLD_TICK` event

Each tick writes a `world.events` row with `world_tick` (a monotonic per-world
counter) and payload `{"granularity": "daily"}`. Consumers (worker, tests,
dashboards) use the payload to confirm it is a daily tick; the worker then
reads the world's calendar config to decide which day-derived passes to run.

## 5. Tuning the cadences (admin)

Cadences and calendar tuning are runtime config, changed through the admin API:
`POST /api/admin/worlds/:id/config` with `{"key": "tick.<name>_cadence", "value":
"<cron spec>"}` (or a JSON number for `tick.day_length` / `calendar.*`).

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.day_length","value":60}'
```

Notes:

- The scheduler re-reads config on its next poll (~15s), so a change is picked
  up within one poll interval — **not** immediately.
- **To run matchdays faster, lower `tick.day_length`** — the world-clock scale
  is what makes `scheduled_at` mature sooner. `tick.day_length: 60` = one game
  day per real minute; `1` = one per second. The daily emission + poll
  catch-up then roll the day counter (and with it the weekly/monthly passes)
  at up to ~7 game-days per 15s poll, which is ample at default scale but lags
  a heavily-compressed world — fine for kickoff-focused lab worlds.
- Setting `tick.match_cadence` does **not** change the world clock; it only
  changes how often the live-match engine advances a simulated minute
  (`internal/match/live.go`). The world clock deliberately ignores it (OPD-17).
- `calendar.days_per_week` / `calendar.days_per_month` (JSON numbers) retune the
  week/month boundaries: e.g. `{"key":"calendar.days_per_month","value":15}`
  posts wages every 15th game day.
- Paused/archived worlds don't tick regardless of cadence.
- A sub-minute or high-frequency daily cadence keeps the day counter close to
  the clock target — but note each daily tick also runs the transfer-market
  sweep, and each month boundary runs the board review for every managed club.

## 6. Common questions

**Is `hourly` still used?** No. The hourly/weekly/monthly/seasonal granularities
were retired in IM02 — the daily clock and its `calendar.*` steps replaced them.
A `WORLD_TICK` with a legacy granularity is logged and dropped.

**Which cadence starts a match?** None directly — kickoffs are gated on the
world clock (`scheduled_at ≤ worldNow`, IM16). The worker's intra-day poll
(~15s) and the post-daily-tick kickoff both call `KickoffDue`; live matches
then pace themselves on `match_cadence` (default 20s per simulated minute).

**At which moment does a match kick off?** At its `scheduled_at` as read on the
continuous world clock. At the default scale that is exactly the real UTC time
the fixture lists (e.g. 20:00); under a compressed `tick.day_length` the same
game moment arrives sooner in real time.

**Why is the board review monthly now?** IM02 re-purposed it from a weekly cron
job to the day-30 pass so wage posting and the review share one day boundary,
and evaluation cadence becomes a live `calendar.days_per_month` knob. The scores
themselves are unchanged (graded by season progress; see
[board-numerics.md](../design/board-numerics.md)).

**Do the daily passes need a playable world?** Yes; the scheduler only registers
a cadence for playable worlds, and the worker dispatch filters on the same. This
is why the launch guide's Step 8 launches the world *before* the ticks matter.