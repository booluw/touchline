# How to: start a season

How a league season comes into being, plays out, and rolls into the next one.
There are two very different paths — **season #1 is created by a service seam**
(`StartSeason`), and **every later season is created automatically** by the
country-wide rollover that fires when the final result of the previous season
is applied.

Relates to: [setup-and-launch.md](setup-and-launch.md),
[cadences-and-time.md](cadences-and-time.md) (matches only kick off on daily
ticks), [glossary.md](glossary.md) (the terms used here).

**In short:** a season exists per league, one at a time. Season #1 needs
`StartSeason` (there is no endpoint yet). After that the last result of a
season triggers a single atomic transaction per country that completes the
old season, applies promotions/relegations, and materialises the next season
with fresh fixtures.

---

## 1. Season lifecycle

A `competition.seasons` row carries a `season_number` and a status:

| Status | Set by | Meaning |
| --- | --- | --- |
| `in_progress` | `StartSeason` (season #1) | active, being simulated |
| `upcoming` | rollover (season #2+) | next season, fully fixture-scheduled, kicks off on its own schedule |
| `completed` | rollover | the previous season is closed |

Game reads (standings, fixtures, stakes, job-offer context) treat everything
**not** `completed` as active, so an `upcoming` season's fixtures participate in
matchdays normally the moment their scheduled day arrives — there is no manual
"start the new season" step after the first.

One season per league at a time (`ErrLeagueAlreadySeeded`).

## 2. Season #1: the `StartSeason` service seam

Creating a league's first season is a service call on the competition package —
**no HTTP endpoint exists today** (the launch guide drives it from a scratch
`go run` main or an integration test).

```go
started, err := compSvc.StartSeason(ctx, worldID, leagueID)
// started.SeasonLabel, started.SeasonNumber, started.Status ("in_progress")
```

What it does, in one transaction:

1. Validates the world exists and is not `archived`, and that
   `leagueID` belongs to that world (`ErrCompetitionWorldMismatch`).
2. Guards `leagueHasSeason` — a second call returns `ErrLeagueAlreadySeeded`.
3. Requires the league to have members (`ErrCompetitionNotSeeded` if it has
   none — run `POST /api/admin/worlds/:id/seed` first).
4. Creates the season (`season_number = MAX + 1`, label like `2026/27`,
   status `in_progress`) with `competition_entries` from the current league
   memberships.
5. Generates the **deterministic double round-robin** fixture list, one
   matchday per game-day, anchored to the world's reference date
   (`launched_at`/`created_at`); fixtures are spaced ≥2 game-days apart to
   satisfy the phase-2 rest rule.
6. Emits `SEASON_CREATED` (world event, carries the world replay seed).

Requires: a **seeded** league (clubs + members) and a world that will be
playable when matchdays arrive. The world may be `provisioning` or even
`paused` for the call itself — matches simply won't kick off until the world is
playable and the daily tick fires (see cadences-and-time).

The integration suite has executable samples
(`internal/competition/competition_integration_test.go`,
`TestStartSeasonAndApplyResultAndRollover`).

## 3. Season #2 and beyond: the automatic rollover

When the **final fixture of the final league in a country** is applied, the
season completion cascades country-wide in **one transaction**
(`rolloverCountry`, `internal/competition/rollover.go`):

1. Every league in the country finishes: `SEASON_COMPLETED` emitted, each
   season marked `completed`, `end_date = now()`.
2. Final standings are computed with the standard tie-breakers (points, goal
   difference, goals scored, club name) and promotions/relegations applied per
   the leagues' adjacency rules (`CLUB_PROMOTED` / `CLUB_RELEGATED` events).
3. Each league's **next** season is created as `upcoming` (`SEASON_CREATED`),
   with fresh entries (promoted/relegated clubs move leagues) and a new
   double round-robin fixture list anchored to the day after the old season's
   last scheduled matchday — calendaring stays continuous.

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
  `ErrAdjacencyMismatch`), validated at seed time.
- A league with no declared movement simply keeps its members.
- Season-complete promotion/relegation uses the final table (points, GD, GF,
  name); the "best season" and mid-season reads use the same ordering.

## 4. Driving a season end-to-end (today)

Summary of the fastest full path (detailed commands in
[setup-and-launch.md](setup-and-launch.md)):

1. Create the world, countries, and leagues (admin API).
2. `POST /api/admin/worlds/:id/seed` — materialises clubs + members; poll
   `seed-status` until `world_seeded: true`.
3. Run the `StartSeason` seam for each league (service call, Step 2 above).
4. Launch the world (`provisioning → active`) so the clock starts.
5. Accelerate `tick.daily_cadence` (e.g. to every minute) so matchdays kick
   off quickly; the final result of the season trips the automatic rollover.

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
`season_number`; events `SEASON_CREATED` / `SEASON_COMPLETED` /
`CLUB_PROMOTED` / `CLUB_RELEGATED` land in `world.events` with the replay
seed on the creation event.

## 6. Common questions

**Is there an admin "start season" endpoint?** No. Season #1 requires the
`StartSeason` service seam; all later seasons roll over automatically. This is
deliberate (seeding and seasons are decoupled, launcher-style).

**Do I need to start each season manually?** Only season #1, per league. After
the first rollover the loop is self-sustaining as long as the world stays
playable and the daily tick fires.

**When does the new season kick off?** Its fixtures are day-gated like any
other: they kick off on the daily tick when the world's current day reaches
their scheduled date. Creating the `upcoming` season does not wait for a
"season start" toggle.

**How do the standings decide the champion?** Points, then goal difference,
then goals scored, then club name (`GetStandings` ordering); the champion is
`order[0]` in the `SEASON_COMPLETED` payload.