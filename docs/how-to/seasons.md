# How to: start a season

How a league season comes into being, plays out, and rolls into the next one.
There are two very different paths — **season #1 is started through an admin
endpoint**, and **every later season is created automatically** by the
country-wide rollover that fires when the final result of the previous season
is applied, after an admin-tunable off-season gap.

Relates to: [setup-and-launch.md](setup-and-launch.md),
[cadences-and-time.md](cadences-and-time.md) (matches only kick off on daily
ticks), [glossary.md](glossary.md) (the terms used here).

**In short:** a season exists per league, one at a time. Season #1 is started
via `POST /api/admin/worlds/:id/leagues/:leagueID/season` — optionally with a
chosen kickoff date pinned in the body. After that the last result of a season
triggers a single atomic transaction per country that completes the old season,
applies promotions/relegations, and materialises the next season — scheduled
to start after the configured off-season gap, with its `upcoming` status
flipped to `in_progress` the moment its first fixture is due.

---

## 1. Season lifecycle

A `competition.seasons` row carries a `season_number` and a status:

| Status | Set by | Meaning |
| --- | --- | --- |
| `in_progress` | start-season endpoint (season #1) or the daily tick (season #2+, `ActivateDueSeasons`) | active, being simulated |
| `upcoming` | rollover (season #2+) | next season, fully fixture-scheduled, waits out the off-season gap |
| `completed` | rollover | the previous season is closed |

Game reads (standings, fixtures, stakes, job-offer context) treat everything
**not** `completed` as active, so an `upcoming` season's fixtures participate in
matchdays normally the moment their scheduled day arrives — there is no manual
"start the new season" step after the first. The status flip is cosmetic +
informational (`SEASON_STARTED` event) and guarantees the season reads
`in_progress` when its first matchday simulates.

One season per league at a time (`ErrLeagueAlreadySeeded`).

## 2. Season #1: the admin start-season endpoint

`POST /api/admin/worlds/:id/leagues/:leagueID/season` (admin-only) starts a
league's first season from its seeded member clubs.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST \
  localhost:8080/api/admin/worlds/$WORLD_ID/leagues/$LEAGUE_ID/season
# 201 → { "id": ..., "competition": {...}, "season_label": "2026/27",
#          "season_number": 1, "status": "in_progress" }
```

Leaving the body empty starts the season and lets it kick off the **day after
the world's current date**. To pin matchday 1 to a date instead, send it in a
JSON body:

```bash
curl -c /tmp/jar -b /tmp/jar -X POST \
  -H 'content-type: application/json' \
  -d '{"kickoff_date": "2031-08-02"}' \
  localhost:8080/api/admin/worlds/$WORLD_ID/leagues/$LEAGUE_ID/season
# 201 → matchday 1 now lands exactly on 2031-08-02
```

Wraps the `Service.StartSeason` / `Service.StartSeasonKickoff` seams
(`internal/competition/seeding.go`). What it does, in one transaction:

1. Validates the world exists and is not `archived`, and that
   `leagueID` belongs to that world (`ErrCompetitionWorldMismatch`).
2. Guards `leagueHasSeason` — a second call returns `ErrLeagueAlreadySeeded`
   (`409`).
3. Requires the league to have members (`ErrCompetitionNotSeeded` if it has
   none — run `POST /api/admin/worlds/:id/seed` first).
4. Creates the season (`season_number = MAX + 1`, label like `2026/27`,
   status `in_progress`) with `competition_entries` from the current league
   memberships.
5. Generates the **deterministic double round-robin** fixture list, paced
   across the game-week (IM03). The anchor for the calendar is the world's
   current date by default (matchday 1 = current date + 1), or **the day
   before a supplied `kickoff_date`** — so a pinned date lands exactly on
   matchday 1. A pinned date that is not a calendar day before the world's
   current date (matchday 1 would already be in the past) is rejected, and so
   is one that is **not an allowed scheduling weekday** when the league's
   effective `allowed_weekdays` set (IM05) is non-empty. Matchday `k` lands on
   game-day `floor((k-1) × days_per_week / matchdays_per_week) + 1` over the
   league's pacing rules — with the defaults, days 1,3,5,8,10,12,… (two
   playing days, one rest).
6. Emits `SEASON_CREATED` (world event, carries the world replay seed).
7. Publishes **two country-wide `announcement` news stories** so the world sees
   the new season land: the fixture list going out, and the official kickoff
   day. Each links to the `SEASON_CREATED` event and reads the actual first
   matchday from `MIN(scheduled_at)` of the generated fixtures.

### Fixture pacing and kickoff times (IM03)

- All three pacing knobs live on the league's rules row
  (`competition_rules.scheduling_rules`, JSONB) and are **read at season
  creation**, so the calendar is stamped onto the fixtures it produces:

  - `matchdays_per_week` — matchdays packed into one game-week. Default `3`.
  - `days_per_week` — the length of a game-week. Default: the world's
    `calendar.days_per_week` config (IM02), else `7`.
  - `kickoff_hours` — a JSONB array of UTC hours the season's matchdays rotate
    through (default `[15, 18, 20]`). Matchday `k` kicks off at
    `kickoff_hours[(k - 1 + base) % len]` where `base` derives from the world's
    replay seed ⊕ the league id, so a season's kickoff times vary while replay
    stays deterministic.
- Every fixture of one matchday shares the same `scheduled_at` (a whole day at
  a UTC kickoff hour). Kickoffs stay **date-gated** on the world clock —
  `KickoffDue` simulates a matchday once `scheduled_at::date ≤ current_day` —
  so kickoff hours are calendar display, never wall-clock logic.
- The same parameters are re-read when the fixture calendar is served, so
  `GET /api/competitions/:id/calendar` reproduces the exact week grouping a
  season was built with: week = `game_day ÷ days_per_week` from the season's
  `start_date`.

### Weekday-aware pacing (IM05)

IM05 adds one optional knob on top of the day formula: an **allowed weekday
set**, which replaces the mechanical day spacing when present.

- `allowed_weekdays` is a JSONB array of ISO weekdays (`1` = Monday … `7` =
  Sunday) under `competition_rules.scheduling_rules->'allowed_weekdays'` for a
  single league or cup, or under a country-wide default at
  `world.countries.default_scheduling_rules->'allowed_weekdays'`.
- Resolution is **per competition → country default → legacy day formula**: a
  league without its own set inherits its country's; a league whose country has
  none (or an explicitly cleared set) reproduces the exact IM03
  day-spaced calendar.
- When a set resolves, matchday 1 = the earliest allowed weekday on/after the
  day after the season anchor; every later matchday = the earliest allowed
  weekday at least **two game-days** after its predecessor (the two-day rest
  is structural — it still holds for monotone sets like "Saturdays only").
  Kickoff times keep the same deterministic `kickoff_hours` rotation.
- `PATCH /api/admin/leagues/:id/scheduling` with a non-empty set **re-paces a
  live league**: matchdays containing a `live`/`completed` fixture are frozen;
  every unstarted matchday slides forward onto the new weekdays, strictly
  forward, order and kickoff hours preserved. Clearing the set (empty array)
  persists the rules but never moves fixtures. A country-default change
  (`PATCH /api/admin/worlds/:id/countries/:countryID/scheduling`) re-paces
  every league of that country with no override of its own. A league re-pacing
  that moves fixtures publishes a country-scoped `scheduling` news story.

Error mapping: `404` for unknown world/league; `409` for an archived world,
a league from another world, an already-seeded league, or an unseeded league;
`400` for a malformed `kickoff_date` (must be a `YYYY-MM-DD` calendar date);
`422` for a kickoff date that precedes the world's current date or lands on a
day the league's effective `allowed_weekdays` forbids (both validate before
anything is written — the season is not created, the 201 `SEASON_CREATED`
event and announcements do not appear).

Requires: a **seeded** league (clubs + members) and a world that will be
playable when matchdays arrive. The world may be `provisioning` or even
`paused` for the call itself — matches simply won't kick off until the world is
playable and the daily tick fires (see cadences-and-time).

The integration suite has executable samples
(`internal/competition/competition_integration_test.go`,
`TestStartSeasonAndApplyResultAndRollover`, plus
`TestOffSeasonGapRolloverAndActivation`).

## 3. Season #2 and beyond: the automatic rollover and off-season

When the **final fixture of the final league in a country** is applied, the
season completion cascades country-wide in **one transaction**
(`rolloverCountry`, `internal/competition/rollover.go`):

1. Every league in the country finishes: `SEASON_COMPLETED` emitted, each
   season marked `completed`, `end_date = now()`.
2. Final standings are computed with the standard tie-breakers (points, goal
   difference, goals scored, club name) and promotions/relegations applied per
   the leagues' adjacency rules (`CLUB_PROMOTED` / `CLUB_RELEGATED` events).
3. Each league's **next** season is created as `upcoming` (`SEASON_CREATED`),
   with fresh entries and a new double round-robin fixture list **anchored to
   the last scheduled matchday plus the off-season gap**
   (`season.off_season_ticks`).

**Composing the next season's table (IM14).** A league's next entries are, in
order:

1. its **stayers** — everyone not relegated, plus the clubs promoted up from
   below and relegated down from above (per the adjacency counts, ~:211);
2. its **declared members** — clubs an admin added mid-season via
   `POST /api/admin/leagues/:id/…/league` (they bind here, never mid-season);
3. its **auto-fill** to `team_count` — first from the country's league-less
   club pool, then freshly generated AI clubs, so the table is never holey.

Declared members sort ahead of any automated fill. Promotions/relegations and
`team_count` are read **at rollover**, so a mid-season capacity change
(`PATCH /api/admin/leagues/:id/capacity`, IM14) takes effect on this very
table.

### The off-season gap

- The gap is measured in **daily world ticks**. Default **30**; read at
  rollover time from:
  1. per-league override `competition_rules.scheduling_rules ->> 'off_season_ticks'`
     (JSONB on the league's rules row), then
  2. the world config key `season.off_season_ticks`
     (`POST /api/admin/worlds/:id/config`, seeded to 30 at launch), then
  3. the compiled default (`30`).
- The next season's `start_date` is set to `lastFixtureDay + gap`, so
  **no fixture falls inside the off-season** — the fixture calendar pauses
  while wages, expenses, and the (always-open) transfer market keep running,
  so managers can rebuild before kick-off.
- The `upcoming` season flips to `in_progress` — and emits `SEASON_STARTED` —
  on the daily tick (`ActivateDueSeasons`, before kickoff) when the world's
  current day reaches its first scheduled fixture. Idempotent: each season
  flips exactly once.

Atomicity is a hard contract: a failure in any of the three steps aborts the
whole completion — the final fixture stays unapplied, the old season stays
open, and no `SEASON_COMPLETED` survives
(`TestRolloverEventFailureAbortsSeasonCompletion`). Redelivery replays safely.

### Requirements for a rollover to trigger

- Every league in the country has its final fixture applied (a match only
  finishes when it is actually simulated).
- The world is playable and the daily tick fires — that is what runs the
  kickoff → simulation → result-application path. In practice: accelerate the
  daily cadence and let the ticks run the matches to completion (see
  cadences-and-time + the launch guide's Step 8).

### Promotion / relegation rules (recap)

- Adjacencies are admin-declared per league (`promotes_to`, `relegates_to`)
  and must be **within the same country** with **symmetric counts** (the
  league above relegates exactly as many as the league below promotes —
  `ErrAdjacencyMismatch`). A capacity change auto-adjusts the neighbour's
  reciprocal count so the ladder stays symmetric (IM14).
- A league with no declared movement simply keeps its members.
- Season-complete promotion/relegation uses the final table (points, GD, GF,
  name); the "best season" and mid-season reads use the same ordering.

## 4. Driving a season end-to-end (today)

Summary of the fastest full path (detailed commands in
[setup-and-launch.md](setup-and-launch.md)):

1. Create the world, countries, and leagues (admin API).
2. `POST /api/admin/worlds/:id/seed` — materialises clubs + members; poll
   `seed-status` until `world_seeded: true`.
3. `POST /api/admin/worlds/:id/leagues/:leagueID/season` for each league.
4. Launch the world (`provisioning → active`) so the clock starts.
5. Accelerate `tick.daily_cadence` (e.g. to every minute) so matchdays kick
   off quickly; the final result of the season trips the automatic rollover,
   whose next season lands after `season.off_season_ticks`.

To see season rollover smoke-tested without a manual main, the integration
suite plays a full league end-to-end
(`internal/competition/competition_integration_test.go` and
`outbox_integration_test.go`).

## 5. Reading a season

Manager-scoped reads (world is resolved from the session):

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/competitions/$LEAGUE_ID/standings
curl -c /tmp/jar -b /tmp/jar "localhost:8080/api/competitions/$LEAGUE_ID/fixtures"
```

Seasons/standings responses embed `season_id`, `season_label` and
`season_number`; events `SEASON_CREATED` / `SEASON_STARTED` /
`SEASON_COMPLETED` / `CLUB_PROMOTED` / `CLUB_RELEGATED` land in `world.events`
with the replay seed on the creation event.

## 6. Common questions

**Is there an admin "start season" endpoint?** Yes — `POST
/api/admin/worlds/:id/leagues/:leagueID/season` starts season #1. All later
seasons roll over automatically.

**Can I choose when season #1 kicks off?** Yes. An empty body starts the
season on the world's current date and schedules matchday 1 for the next day,
so a freshly-seeded world becomes playable immediately. A JSON body with
`kickoff_date` pins matchday 1 to exactly that date (handy for aligning a
launch calendar); the fixture calendar anchors the day before. Dates already
in the past, or off the league's allowed weekdays, come back `422` and write
nothing.

**Do I need to start each season manually?** Only season #1, per league. After
the first rollover the loop is self-sustaining as long as the world stays
playable and the daily tick fires.

**When does the new season kick off?** Its fixtures are day-gated like any
other: they kick off on the daily tick when the world's current day reaches
their scheduled date, which is the last matchday of the old season plus the
off-season gap. The `upcoming` season simultaneously flips to `in_progress`
(`SEASON_STARTED`); creating it does not wait for a "season start" toggle.

**How do I change the off-season length?** Configure `season.off_season_ticks`
world-wide via `POST /api/admin/worlds/:id/config`, or set a per-league
override in the league's `competition_rules.scheduling_rules` JSON. The value
is read at rollover, so set it before the final matchday lands.

**What still runs during the off-season?** The daily clock, wages (each
`calendar.days_per_month` day boundary), academy maintenance, and the transfer
market — there is no season
gating on them, so managers can prepare (and trade) during the break.

**How do the standings decide the champion?** Points, then goal difference,
then goals scored, then club name (`GetStandings` ordering); the champion is
`order[0]` in the `SEASON_COMPLETED` payload.