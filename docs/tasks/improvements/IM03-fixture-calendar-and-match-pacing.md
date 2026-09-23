# IM03 — Fixture calendar and match pacing: 3 league games a week + season fixture calendar

**Status:** Not started
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** S04-01 (fixture generation); S05-02 (finance); IM02 (single
daily cadence — this task builds on the day-based pacing)

## What to do

1. **Pace league fixtures at 3 matchdays per 7 game-days** instead of one
   matchday per game-day (`createFixtures` today schedules matchday *r* on day
   `r+1`). Longer real-time rhythm: with the IM02 daily default (1 game-day per
   real day) an in-game week is a real week and a club plays about 3 league
   games in it.
2. **Give managers a full-season fixture calendar with kickoff times** — a
   season-wide, week-grouped view of every fixture and when it plays, plus a
   per-club fixture list.
3. Note (recorded, not implemented here): managers setting **friendlies and cup
   matches** is a later task; the schema hooks already exist.

## Behaviour

### Matchday spacing (league season)

Replace the `day = start + k` schedule in `createFixtures`
(`internal/competition/seeding.go:449-467`) with a deterministic day map:

```
day(anchor, k) = anchor + floor((k - 1) * daysPerWeek / matchdaysPerWeek) + 1
```

- `k` = matchday index (1-based, `1..2·(n−1)` for the double round-robin).
- `matchdaysPerWeek` from `competition_rules.scheduling_rules ->> 'matchdays_per_week'`
  (default **3**).
- `daysPerWeek` from `scheduling_rules ->> 'days_per_week'`, falling back to the
  world's `calendar.days_per_week` (IM02, default **7**).
- Consequence: consecutive matchdays are ≥ `daysPerWeek / matchdaysPerWeek`
  apart (a club cannot play two fixtures on consecutive days — satisfies the
  existing phase-2 rest expectation), and a season of 10 matchdays (6 teams)
  spans ~3.5 weeks.

### Kickoff times

- Replace the single `KickoffHourUTC` (19:00, `competition/service.go:185`)
  with a deterministic per-matchday kickoff hour.
- Source: `scheduling_rules ->> 'kickoff_hours'` (array of hours, default
  `[15, 18, 20]` UTC), index rotated deterministically from
  `world_seed ⊕ league_id` so every replay reproduces the same times.
- `scheduled_at` stays the calendar day + chosen hour (UTC ISO); **no wall-clock
  dependence** — the daily calendar date is still
  `worldDate = launched_at/created_at + current_day` and fixtures kick when
  `scheduled_at::date <= worldDate` (unchanged `KickoffDue`).
- Rollover (`rollover.go`) inherits the spacing via `createFixtures(anchor, …)`
  where `anchor = lastScheduledDay + off_season_ticks` (IM01) — spacing and the
  off-season gap compose.

### New endpoints

`GET /api/competitions/:id/calendar` (manager-scoped, optional `?season=<n>`)

- Returns the season's fixtures grouped by world-week and matchday:
  `{league, season:{id,label,number,status}, weeks:[{week, first_day, matchdays:[{matchday, scheduled_at, fixtures:[{id, home, away, status, home_score, away_score}]}]}]}`.
- Unplayed fixtures show `scheduled_at` (their calendar day + kickoff hour); the
  client additionally derives the real-time "when" from the world reference
  date + daily cadence if desired.

`GET /api/clubs/:id/fixtures` (own-club read)

- The club's season fixture list (home and away), each with `matchday`,
  `scheduled_at`, `opponent`, `status`, `home_score/away_score`; ordered by
  `scheduled_at`. World-scoped to the caller's manager.

Both ride the existing `Fixture` read model (`standings.go` `GetFixtures`) with
added grouping; no new storage.

### Frontend

- `frontend/app/pages/play/competitions.vue`: replace the matchday dropdown
  with the **calendar view** (week rows; each fixture shows its kickoff day +
  time and links into `/matches/:id`); keep the standings table. Drop the
  hardcoded "Kick-off 19:00 UTC" text (`competitions.vue:122`) — times now come
  from `scheduled_at`/the calendar payload.
- Optional: a small club-fixtures panel on the club page using
  `GET /api/clubs/:id/fixtures`.

### Out of scope (recorded for a later task)

- **Friendly matches** and **cup matches** set by managers. Not implemented
  here. Schema hooks already in place: `competition.competitions`
  (`competition_type 'preseason'|'domestic_cup'`, `created_by_manager_id`),
  `competition_rules.scheduling_rules`, and `competition.club_competitions`
  role `cup`. This task only guarantees the calendar/reads and spacing are ready
  to absorb them.

## Changes

### internal/competition

- `seeding.go` `createFixtures`: add the day-map + kickoff-hour selection;
  read `scheduling_rules` for `matchdays_per_week`/`days_per_week`/`kickoff_hours`
  inside the caller's tx.
- New `Service.SeasonCalendar(ctx, worldID, leagueID, season *int)` and
  `Service.ClubFixtures(ctx, worldID, clubID)`; reuse fixture scanning, add
  week grouping (deterministic: `week = floor(worldDay / days_per_week)`).
- Delete/retire the `KickoffHourUTC` constant usage (or keep as the fallback
  when `kickoff_hours` is absent).

### internal/httpapi

- `router.go`: `api.GET("/competitions/:id/calendar", …)`,
  `api.GET("/clubs/:id/fixtures", …)`.
- Handlers mapping sentinel errors (404 league/club, 409 world mismatch).

### frontend

- `useCompetition.ts`: add `getCalendar` (and club-fixtures helper).
- `play/competitions.vue`: calendar view.
- Keep openapi spec + route-coverage test in sync.

## Tests

- `internal/competition`: spacing invariants for 4/6/8/12-team leagues — every
  two consecutive matchdays of a club are ≥ 2 days apart; matchday count per
  7-day window ≤ 3; day map is pure in `(seed, leagueID, n)` and replay-stable.
- Kickoff-hours determinism: same seed ⇒ same hours; `kickoff_hours` override
  respected; default fallback applied.
- Rollover + IM01 gap compose: next season fixtures land after
  `lastDay + off_season_ticks` and still respect the 3-per-week pattern.
- `SeasonCalendar`: weeks/grouping correct for a full season; optional `?season`
  filter; `ClubFixtures`: own-club only, ordered by `scheduled_at`.
- `cmd/api` integration: both endpoints happy path + 404/409.
- Frontend typecheck + lint (`pnpm run typecheck`, `pnpm run lint`).

## Docs

- `docs/how-to/seasons.md`: fixture density (3/week), kickoff-hour variety, the
  calendar endpoint.
- `docs/how-to/setup-and-launch.md`: Step 7/8 note fixture pacing (match on days
  1,3,5 of each week with the default cadence) and where to see the calendar.
- `docs/how-to/glossary.md`: `matchday`, `scheduled_at`, `kickoff_hours`,
  "match week" terms.
- `internal/apidocs/openapi.yaml`: the two new endpoints + `calendar`/
  `club fixtures` refs; update the hardcoded-kickoff documentation.
- Recorded-decision note for friendlies/cups (out of scope) in this file and a
  pointer in `docs/product_manager.md` if a rule needs tracking.

## Recorded decisions

- "3 league games a week" = 3 **matchdays** per `days_per_week` (default 7
  game-days), not a hard 3-per-365-day calendar — the league's own schedule
  density is finite chiefly for small leagues (4 teams → 6 matchdays ≈ 2/week).
- Kickoff hours are per-league tuning data (`scheduling_rules.kickoff_hours`),
  deterministic per world seed.
- No storage/migration needed for the calendar — it is a grouping read over
  `match.fixtures`.
- Friendlies and cup matches are explicitly deferred to a follow-up task.