# Chapter 4 — The world clock, time and cadences

Time is the heartbeat of Touchline. Fixtures, wages, training, board reviews,
season activation and the transfer market are all driven by one daily clock.
If you understand this chapter you understand *when* everything in the rest of
the book happens.

## 4.1 Two time values

| Value | What it is | Who reads it |
| --- | --- | --- |
| **`worldNow`** | a continuous clock: `epoch + (realNow − epoch) × 86400 / tick.day_length`, where `epoch` = UTC midnight of `COALESCE(launched_at, created_at)` | **kickoffs** (`scheduled_at ≤ worldNow`), bid TTL |
| **`current_day`** | an integer day counter on `world.worlds`, advanced only by the daily emission | weekly/monthly/seasonal passes, season activation, `world_date` |

At the default `tick.day_length = 86400` the scale factor is 1, so **`worldNow`
is exactly real UTC**: a fixture listed for "Sep 27 20:00" kicks off at real
20:00 (IM16). Code: `internal/world/clock.go`.

The **world date** (IM25, OPD-51) is `COALESCE(launched_at, created_at)` (UTC)
`+ current_day` days, exposed as SQL functions `world.world_date(world_id)` and
`world.club_world_date(club_id)` (migration 0056). **Every game-date
computation uses it**: contract start/end, wage-commitment windows, player ages
and contract days-left, dashboard expiry windows, manager-history dates,
offer TTLs, instalment due dates. `now()` is reserved for real audit stamps
(`created_at`, `responded_at`, sessions).

> **Product decision OPD-57 (2026-09-29): one world day = one real day.** The
> product owner decided the whole game runs in real time. `tick.day_length`
> still exists in code today (default 86400, used for lab compression), but
> IM31 ("real-time world clock", *Not started*) will retire it as a setting;
> tests will speed time with an injected clock. Do not build features that
> depend on compressing `day_length`.

## 4.2 The cadence model

Since IM02 there is exactly **one registered cadence per playable world**:

| Config key (`world.world_config`) | Default | Meaning |
| --- | --- | --- |
| `tick.daily_cadence` | `0 0 * * *` | cron spec for the daily emission |
| `tick.day_length` | `86400` | real seconds per game day (see OPD-57) |
| `tick.match_cadence` | `20s` | live-match pacing; **not** a world tick |
| `calendar.days_per_week` | `7` | week boundary step |
| `calendar.days_per_month` | `30` | month boundary step |
| `season.off_season_ticks` | `30` | off-season gap in game days ([Ch. 7](07-seasons-and-rollover.md)) |

Defaults are seeded at launch from `internal/world/service.go`
(`defaultConfigKeys`). The scheduler (`internal/scheduler`) re-reads config
every poll (`SCHEDULER_POLL_INTERVAL`, ~15 s), so a config change applies within
one poll, without a redeploy. The legacy hourly/weekly/monthly/seasonal
granularities were retired; a `WORLD_TICK` carrying one is logged and dropped.

### Catch-up

Each daily fire **rolls `current_day` toward the clock's target**
(`floor(elapsed game-days)`) in bounded passes of **up to 7 game-days per
fire**, never overshooting. A world that was paused or offline catches its
calendar up without a burst; redundant emissions are no-ops.

### Day-stamped ticks (IM23, OPD-49)

Each emission carries the day it advanced to:

```json
{ "granularity": "daily", "day": 30 }
```

The worker gates every pass on **`payload.day`**, never on a re-read of
`current_day` — a catch-up commits several days at once, and each emission must
still run *its own* day's boundaries exactly once. Emissions written before
IM23 have no `day`; the worker falls back to the counter.

## 4.3 What runs when — the dispatch table

The dispatcher is `handleWorldTick` in `backend/internal/app/worldtick.go`.

| When | Passes (in order) | Chapter |
| --- | --- | --- |
| **every day** | (1) `Runner.KickoffDue` + live pacing (only in the worker holding the match-runner lock); (2) `Policy.RespondToBidsForAbsent`; (3) `Transfers.DailyTick` = expire stale bids → recompute valuations → AI bid activity; plus season activation (`ActivateDueSeasons`) and job-offer expiry | [16](16-matchday-and-live-matches.md), [25](25-policybot-and-absence.md), [21](21-transfer-market.md), [7](07-seasons-and-rollover.md), [22](22-managers-and-job-offers.md) |
| **`day % days_per_week == 0`** (7, 14, 21…) | (1) `Policy.EnsureTraining`; (2) `Training.ApplyWeekly` (archetype deltas, development, condition, training injuries, setbacks); (3) `Players.WeeklyTick` (morale recovery, shares, role backfill, promise grading, transfer-request assessment, auto-list); (4) `Social.ReconcileRivalries` | [13](13-training-and-development.md), [14](14-condition-and-injuries.md), [18](18-morale-and-transfer-requests.md), [24](24-social-and-rivalries.md) |
| **`day % days_per_month == 0`** (30, 60…) | (1) `Finance.ApplyMonthlyWages`; (2) `Academy.Maintenance`; (3) **`Board.Review`** (confidence, mandates, sackings) | [20](20-finance.md), [11](11-player-lifecycle-and-academy.md), [23](23-board-and-job-security.md) |
| **`day % 364 == 0`** — league-less worlds only | seasonal lifecycle fallback (intake, retirement, pool replenish) | [11](11-player-lifecycle-and-academy.md) |
| after the passes | `Dashboard.PushWorldDelta` (best-effort) | [26](26-dashboard-news-scouting-realtime.md) |

Ordering guarantees:

- A day that is both a week and a month boundary runs **weekly before
  monthly**, and within monthly the **board runs last**, so it grades post-wage
  books exactly once.
- The PolicyBot answers absent managers' bids **before** the market sweep.
- `EnsureTraining` writes bot plans **before** training applies.

### Intra-day kickoffs

Kickoffs are gated on `worldNow`, not on the daily tick. The worker runs an
intra-day **kickoff poll (~15 s)** that calls `KickoffDue` and re-enters the
live pacing loop for **every** playable world on **every** pass (IM18), so a
15:00 fixture starts within one poll of 15:00 and an interrupted match always
resumes ([Chapter 16](16-matchday-and-live-matches.md)).

## 4.4 Pause, resume, archive

- Only **playable** worlds (`active`, `open_beta`) tick; `provisioning`,
  `paused` and `archived` never do.
- Pausing freezes the clock's rollover. On resume the counter and clock
  continue from the present; fixtures whose `scheduled_at` was missed kick off
  in bounded catch-up passes.
- Lineup/tactics commands are refused (`409`) while a world is paused.

## 4.5 Live match time is separate

A live match paces itself on `tick.match_cadence`, frozen into
`match.matches.pacing_millis` **at kickoff**:

```
real duration ≈ 90 × tick.match_cadence     (default 20 s → ~30 min)
```

A golden-goal cup tie adds exactly one more step
([Chapter 8](08-cups.md)). Changing `match_cadence` affects only the *next*
matchday.

## 4.6 Tuning recipes (admin)

```bash
# change a cadence / calendar key (picked up within one scheduler poll)
curl -b jar -X POST localhost:8080/api/admin/worlds/$WORLD/config \
  -H 'Content-Type: application/json' \
  -d '{"key":"calendar.days_per_month","value":15}'
```

- Shorter months = more frequent wages **and board reviews**.
- `tick.match_cadence: "10ms"` makes lab matches finish in under a second.
- Each config change records a `WORLD_CONFIG_CHANGED` event (IM27).

## 4.7 FAQ

**Is the hourly tick used?** No — retired in IM02.

**Which cadence starts a match?** None directly. `scheduled_at ≤ worldNow`,
checked by the daily tick and the 15 s kickoff poll.

**How often is the board review?** Every month boundary — default every 30th
world day. See [Chapter 23](23-board-and-job-security.md) for why per-match
ratings exist alongside it.

**Do real days and world days differ?** At the default scale, no — and per
OPD-57 they never will in production.

## Connections

- Seasons are activated by the day counter: [Chapter 7](07-seasons-and-rollover.md).
- Scheduling of fixtures onto days and hours: [Chapter 6](06-leagues-and-scheduling.md).
- Source: `docs/how-to/cadences-and-time.md`, OPD-17, OPD-24, OPD-42, OPD-49, OPD-51, OPD-57, IM02, IM16, IM18, IM23, IM25.

---
[← Events](03-events-and-explanations.md) · [Contents](the-touchline-book.md) · [Next: Worlds →](05-worlds-and-setup.md)
