# How to: cadences and game time

How the game clock works: Touchline runs a **simulated calendar** driven by
real-world cron schedules called **cadences**. This document explains what each
cadence means, what it drives on the gameplay side, and how to tune it.

Relates to: [setup-and-launch.md](setup-and-launch.md) (launching + accelerating
a world), [seasons.md](seasons.md) (what the matches produce),
[transfer-market.md](transfer-market.md) (daily market maintenance).

**In short:** a *daily* tick advances the in-game calendar and kicks off
matches; *weekly* ticks run training, player state and the board review;
*monthly* ticks post wages and academy costs; *hourly* and *seasonal* are
registered but not yet wired to gameplay. A separate `match` cadence paces
live matches, but is deliberately **not** a world-clock granularity.

---

## 1. The cadence model

Every playable world stores its own cadences as runtime config keys
(`world.world_config`, prefix `tick.`). The scheduler runs **one cron entry per
playable world per granularity**, re-reads the config every poll (~15s), and
fires a `WORLD_TICK` event on the bus per world per granularity. Paused and
archived worlds never tick.

Only `WORLD_TICK{daily}` advances the world calendar (see §3); every
granularity still writes its `WORLD_TICK` event log line for ordering/audit.

Registered granularities (from `internal/scheduler`) and the default specs
seeded at world launch (`internal/world/service.go`):

| Key | Granularity | Default spec | Default rhythm |
| --- | --- | --- | --- |
| `tick.hourly_cadence` | `hourly` | `0 * * * *` | every real hour |
| `tick.daily_cadence` | `daily` | `0 */8 * * *` | every 8 real hours (~3 game days/day) |
| `tick.weekly_cadence` | `weekly` | `0 0 * * 0` | every Sunday 00:00 |
| `tick.monthly_cadence` | `monthly` | `0 0 1 * *` | 1st of the month 00:00 |
| `tick.seasonal_cadence` | `seasonal` | `0 0 1 1 *` | 1 Jan 00:00 |
| `tick.match_cadence` | *(excluded)* | `20s` | live-match pacing, not a world tick |

`match_cadence` is explicitly **excluded** from the scheduler (see
`internal/scheduler/service_test.go`) — it belongs to the live match engine
(`internal/match/live.go`).

## 2. What each tick drives on the gameplay side

The dispatch lives in the worker's `WORLD_TICK` handler
(`backend/internal/app/app.go`). In this order:

| Granularity | Effect (in order) |
| --- | --- |
| `hourly` | **Nothing yet.** The tick is recorded in `world.events`; no gameplay hook consumes it today. Reserved for future faster-than-daily needs. |
| `daily` | Advance the calendar, then: (1) `Runner.KickoffDue` — start every matchday whose `scheduled_at` has arrived (fixtures are day-gated by the season reference date); (2) live pacing of in-progress matches (`Runner.RunLive`); (3) `Policy.RespondToBidsForAbsent` — the PolicyBot answers bids for away-managed clubs before the market sweep; (4) `Transfers.DailyTick` — expire stale bids, recompute player valuations, and run AI buyer activity. |
| `weekly` | (1) `Policy.EnsureTraining` (bot writes plans before the pass); (2) `Training.ApplyWeekly` — apply the club's training archetype deltas + condition; (3) `Players.WeeklyTick` — morale recovery, playing-time shares, squad-role backfill, transfer-request assessment; (4) `Social.ReconcileRivalries` (backfill older fixtures); (5) `Board.WeeklyReview` — score every managed club against its mandates, snapshot the seven factors, and sack human managers at/under the confidence threshold. |
| `monthly` | (1) `Finance.ApplyMonthlyWages` — post `4 × weekly_wage` per active wage commitment (dedup-keyed, idempotent); (2) `Academy.Maintenance` — debit each club's `annual_cost / 12` facility cost. |
| `seasonal` | **Nothing yet.** Registered and recorded; reserved for future yearly hooks. |

The home dashboard also receives a best-effort `dashboard_update` sweep once
per tick after the daily/weekly/monthly passes (`internal/app/app.go`,
S07-01).

## 3. The calendar

- The world's clock is `world.current_day`, an integer day counter.
- **Only `WORLD_TICK{daily}` advances it** (the scheduler notes explicitly:
  "only WORLD_TICK{daily} advances the world calendar").
- The season reference date is the world's `launched_at` (or `created_at`);
  matchday *n* plays on day *n*. With the default `0 */8 * * *` daily cadence
  the world runs ~3 game days per real day.

Consequences:

- **Faster seasons** = accelerate the daily cadence (see §5). Every daily tick
  that lands on or past a scheduled matchday's date kicks that matchday off.
- The weekly/monthly/monthly ticks do **not** move the calendar — they run on
  their own real-world schedules (which is why league success, wage posting,
  and training cadence don't need a "day" aligned with matches).

## 4. Reading a `WORLD_TICK` event

Each tick writes a `world.events` row with `world_tick` (a monotonic per-world
counter) and payload `{"granularity": "<hourly|daily|weekly|monthly|seasonal>"}`.
Consumers (worker, tests, dashboards) use the payload to decide which passes to
run.

## 5. Tuning the cadences (admin)

Cadences are runtime config, changed through the admin API:
`POST /api/admin/worlds/:id/config` with `{"key": "tick.<name>_cadence",
"value": "<cron spec>"}`.

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
- Paused/archived worlds don't tick regardless of cadence.
- A sub-minute or high-frequency daily cadence makes the dev loop fast, but keep
  in mind each daily tick also runs the transfer-market sweep and each weekly
  tick runs the board review for every managed club.

## 6. Common questions

**Is `hourly` used?** No — it's registered and recorded, but nothing consumes
it. Reserved.

**Which cadence starts a match?** `daily`. `KickoffDue` fires on the daily tick
for any matchday whose scheduled date has arrived. Live matches then pace
themselves on `match_cadence` (default 20s per simulated minute).

**At which point in a day do matches kick off?** Fixtures are day-gated, not
hour-gated: when the daily tick lands on the scheduled day, the matchday is
kicked. Scheduling constraints (≥2 game-days between a club's fixtures) come
from `StartSeason`/fixture generation — see [seasons.md](seasons.md).

**Do weekly/monthly passes need a playable world?** Yes; the scheduler only
registers cadences for playable worlds, and the worker dispatch filters on the
same. This is why the launch guide's Step 8 launches the world *before* the
ticks matter.