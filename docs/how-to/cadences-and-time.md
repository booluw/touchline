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
(`backend/internal/app/worldtick.go`, `handleWorldTick`). Every gate below reads
**the day stamped on the emission** (`payload.day`, IM23), not the world's
counter at processing time — a catch-up pass commits several days at once, and
each of its emissions must still run its own day's passes. The realtime
`world_tick` socket push that precedes the dispatch is best-effort: a Redis
failure is logged and the day's gameplay still runs. In this order:

| When | Effect (in order) |
| --- | --- |
| **always** | (1) `Runner.KickoffDue` + live pacing of in-progress matches — but only in the worker that holds the match-runner lock; (2) `Policy.RespondToBidsForAbsent` — the PolicyBot answers bids for away-managed clubs before the market sweep; (3) `Transfers.DailyTick` — expire stale bids, recompute player valuations, and run AI buyer activity. |
| **every day where `day % days_per_week == 0`** (7/14/21/28…) | (1) `Policy.EnsureTraining` (bot writes plans before the pass); (2) `Training.ApplyWeekly` — apply the club's training archetype deltas + condition; (3) `Players.WeeklyTick` — morale recovery, playing-time shares, squad-role backfill, transfer-request assessment; (4) `Social.ReconcileRivalries` (backfill older fixtures). |
| **every day where `day % days_per_month == 0`** (30/60/90…) | (1) `Finance.ApplyMonthlyWages` — post `4 × weekly_wage` per active wage commitment (dedup-keyed, idempotent); (2) `Academy.Maintenance` — debit each club's `annual_cost / 12` facility cost; (3) `Board.Review` — score every managed club against its mandates, snapshot the seven factors, and sack human managers at/under the confidence threshold. |
| **every day where `day % 364 == 0`** (day 364, league-less worlds only) | (1) seasonal fallback — drives the full player-lifecycle sweep (intake, retirement, pool replenish) once per season for worlds with no leagues. A world with any league skips it (IM24) and rides its countries' `SEASON_COMPLETED` events instead; across all of a season's rollovers, retirement runs once per world. |

A day that is both a week and a month boundary runs weekly before monthly, so
the board grades post-wage books exactly once.

The home dashboard also receives a best-effort `dashboard_update` sweep once
per tick after the passes (`internal/app/worldtick.go`, S07-01).

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
counter) and payload `{"granularity": "daily", "day": N}` — `day` is the
calendar day this emission advanced the world to (IM23). Consumers (worker,
tests, dashboards) use the payload to confirm it is a daily tick; the worker
gates its day-derived passes on `day` and reads the world's calendar config
(`days_per_week` / `days_per_month`) for the step sizes. Emissions written
before IM23 have no `day`; the worker falls back to the world's current counter
for them.

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

## 7. Live matches: how long a match really takes (IM18)

A live match is **not** paced by the world clock. It paces itself on
`tick.match_cadence`, which is read **once at kickoff** and frozen into
`match.matches.pacing_millis`. So the real-time cost of a match is exactly:

```
real minutes ≈ 90 × tick.match_cadence
```

| `tick.match_cadence` | Real time for a 90-minute match |
| --- | --- |
| `20s` (default) | ~30 minutes |
| `10s` | ~15 minutes |
| `60s` | ~90 minutes |
| `10ms` (lab worlds) | under a second |

A golden-goal cup tie (a knockout fixture still level after 90) takes **one extra
step**, so add one `tick.match_cadence` to those numbers. See
[cup-competitions.md](cup-competitions.md).

**A match always finishes.** Since IM18 the worker's kickoff poll re-enters the
pacing loop for every world on **every** pass, not only right after a kickoff, and
a single flaky write is retried three times before the loop hands the world back.
Either way the next poll adopts the match and carries it on from its last persisted
minute — the pacing loop is resumable by design (one transaction per simulated
minute, `Finalize` idempotent). Before IM18 a match whose loop died mid-way was
never re-entered, and because the no-overlap rule refuses to start a later matchday
while one is live, that fixture could sit at the same minute forever.

**Diagnosing a long match.** Two numbers tell you which it was:

```sql
-- 1. What pace is this world running at, and what is a live match actually frozen at?
SELECT w.id, c.config_value AS match_cadence
FROM world.world_config c JOIN world.worlds w ON w.id = c.world_id
WHERE c.config_key = 'tick.match_cadence';

SELECT f.id AS fixture_id, m.id AS match_id, m.status, m.current_minute,
       m.pacing_millis, m.started_at
FROM match.matches m JOIN match.fixtures f ON f.id = m.fixture_id
WHERE m.status = 'in_progress'
ORDER BY m.started_at;

-- 2. Has the live match been going longer than 90 × pacing?
SELECT current_minute, pacing_millis,
       pacing_millis * 90 / 1000 AS expected_seconds,
       EXTRACT(EPOCH FROM (now() - started_at))::int AS live_seconds
FROM match.matches WHERE status = 'in_progress';
```

- `pacing_millis` ≈ 60000 and `current_minute` climbing at one per minute is not a
  stall — the world is simply configured for a 90-minute pace. Lower
  `tick.match_cadence` (§5) to shorten matches; the change applies to the **next**
  matchday, since a running match keeps its frozen pacing.
- A `current_minute` that stays put for minutes of real time, or a `live_seconds`
  far beyond `expected_seconds`, is a stall. The worker log now brackets every
  match with `live …: fixture … kicked off …` and `live …: full time …`, and a
  resumed match logs `resuming … from minute N … live for …`, so a stalled loop
  is visible without running queries.

## 8. Staggered kickoffs: a round is a spread of slots, not one moment (IM22)

Since IM22 a competition round no longer shares one kickoff time by default.
Each tie gets its own **kickoff slot** resolved from two per-competition hour
pools — human-involving ties (a club with `club.clubs.is_ai_controlled =
false` on either side) from `human_kickoff_hours`, default **18:00 / 20:00
UTC**, and AI-only ties from `ai_kickoff_hours`, default **12:00 / 15:00 /
17:00 / 23:00 UTC** — spread across the round's allowed weekdays.

**When does a round span days?** When the competition resolves **≥ 2 allowed
weekdays** (`scheduling_rules->'allowed_weekdays'`, the IM05 chain). The default
for unconfigured competitions is the built-in set **{5, 6, 7, 1}** (Fri/Sat/Sun/
Mon), so a normal league matchweek is a real multi-day spread like the European
leagues. A single-weekday competition (or one opted out with
`scheduling_rules->>'staggered' = 'false'`) keeps the legacy single-day
calendars unchanged.

**How the slots are chosen.** The round walk places round 1 on the first allowed
weekday after the season anchor and each later round on the first allowed
weekday ≥ 2 game-days after the previous round's last kickoff day. Within a
round, ties sorted by `(home, away)` fill slots on those days — one tie per
slot; a round too big for the first day's slots advances to the next allowed
weekday, **within its own pool** (human ties stay in the evening slots, AI ties
stay in the day slots). The 23:00 AI slot is the matchweek's "midnight kickoff"
tail.

**How many matches run at once.** `max_simultaneous_matches` (default 3) caps a
_staggered_ competition's live fixtures per round at kickoff time: the worker
admits the due round's earliest-scheduled ties up to the cap and defers the
rest (`Summary.Skipped`) until some finish. The cap does **not** apply to
single-day rounds, legacy pacing, or the season-final matchday — those kick
their whole round at once. A competition also never has two rounds live at a
time: before admitting a round, the runner checks the competition has no `live`
fixture from an earlier matchday (round-order gate, replacing the old world-wide
"any live match blocks everything" rule).

**The season final plays together.** For leagues the final matchday is stamped
on one day at `final_kickoff_hour` (default **20:00 UTC**) with every tie
kicking simultaneously — the title race finishes at once even in a staggered
league. Cup finals are not forced single-evening (cups have their own date
policy).

**Operational notes.** The intra-day poll (§3) matures each slot: a staggered
round's later lanes kick off when their own `scheduled_at` passes, not on the
matchday's first kickoff. Config keys live under
`competition.competition_rules.scheduling_rules` JSONB (`staggered`,
`human_kickoff_hours`, `ai_kickoff_hours`, `max_simultaneous_matches`,
`final_kickoff_hour`); nothing else changed — advanced users who want the
legacy rhythm set `"staggered": false`.

How to see a competition's resolved settings (run from `psql`):

```sql
SELECT competition_id, scheduling_rules
FROM competition.competition_rules
WHERE competition_id = '<competition-id>';
```