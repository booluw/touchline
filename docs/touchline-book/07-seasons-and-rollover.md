# Chapter 7 — Seasons, rollover and promotion/relegation

## 7.1 Season states

A `competition.seasons` row exists per league, **one at a time**
(`ErrLeagueAlreadySeeded`), with `season_number = MAX + 1` and a label such as
`2026/27`.

| Status | Set by | Meaning |
| --- | --- | --- |
| `in_progress` | start-season endpoint (season 1) or daily `ActivateDueSeasons` (season 2+) | being played |
| `upcoming` | rollover | fully scheduled; waiting out the off-season |
| `completed` | rollover | closed |

Game reads treat everything **not** `completed` as active, so an `upcoming`
season's fixtures play normally once their time arrives. The flip to
`in_progress` (with `SEASON_STARTED`) is informational and idempotent.

## 7.2 Season #1 — the admin start

```
POST /api/admin/worlds/:id/leagues/:leagueID/season          # empty body
POST … with {"kickoff_date":"2031-08-02"}                     # pin matchday 1
```

In one transaction (`Service.StartSeason` / `StartSeasonKickoff`,
`internal/competition/seeding.go`):

1. Validate world (not archived) and that the league belongs to it.
2. Guard one season per league (`409`).
3. Require members (`ErrCompetitionNotSeeded` → seed first; an odd member count
   is refused with `ErrOddMemberCount`).
4. Create the season `in_progress` with `competition_entries` from current
   memberships and zeroed standings.
5. Generate the paced double round-robin ([Ch. 6](06-leagues-and-scheduling.md)).
   Anchor: world date (matchday 1 = tomorrow) or `kickoff_date − 1`.
6. Emit `SEASON_CREATED` (carries the replay seed).
7. Publish **two country-wide announcement news stories** — fixtures released
   and official kickoff day (from `MIN(scheduled_at)`) (IM11, OPD-36).

Validation is write-free: `400` malformed date; `422` date before the world's
current date or on a disallowed weekday; `404`/`409` for world/league state.

## 7.3 Season #2+ — the automatic country rollover

When the **final fixture of the final league in a country** is applied,
`rolloverCountry` (`internal/competition/rollover.go`) runs **one atomic
transaction**:

1. **Complete** every league's season: `SEASON_COMPLETED`, status
   `completed`, `end_date`.
2. **Move clubs** per final standings and adjacency: `CLUB_PROMOTED` /
   `CLUB_RELEGATED`.
3. **Create** each league's next season as `upcoming` (`SEASON_CREATED`) with
   fresh entries and fixtures anchored at **last fixture day + off-season
   gap**.

Atomicity is a hard contract: any failure aborts everything — the final
fixture stays unapplied, the old season stays open, no `SEASON_COMPLETED`
survives (`TestRolloverEventFailureAbortsSeasonCompletion`). Redelivery replays
safely.

### Composing the next table (IM14)

Each league's next entries, in order:

1. **Stayers** — everyone not relegated, plus clubs promoted up from below and
   relegated down from above.
2. **Declared members** — clubs admitted by an admin mid-season (they bind
   here, never mid-season).
3. **Auto-fill** to `team_count` — first the country's league-less clubs
   (matched on `lower(country)`), then freshly generated AI clubs — so the
   schedule is never holey.

`team_count`, promotions and relegations are read **at rollover**, so a
mid-season capacity change applies to this very table.

## 7.4 The off-season

- Gap in game days, read at rollover from: per-league
  `scheduling_rules->>'off_season_ticks'` → world `season.off_season_ticks` →
  compiled default **30**.
- Next `start_date = lastFixtureDay + gap`, so **no fixture falls inside the
  off-season**.
- Everything else keeps running: the daily clock, wages and academy costs on
  month boundaries, training, the **always-open transfer market**
  ([Ch. 21](21-transfer-market.md)).
- `ActivateDueSeasons` (daily, before kickoff) flips the `upcoming` season to
  `in_progress` when the world's day reaches its first fixture.

## 7.5 The player lifecycle rides on rollover

Each country's `SEASON_COMPLETED` drives the seasonal player pass
([Ch. 11](11-player-lifecycle-and-academy.md)): academy and street intake, aging,
retirement (once per `(world, season)` — IM24), eligibility, AI auto-fill.
Worlds with **no leagues** use the day-364 fallback instead.

Board mandates are per `(club, manager, season)`; a new season seeds a fresh
mandate set on the next board view ([Ch. 23](23-board-and-job-security.md)).

## 7.6 Cups are independent

Cup results never write league standings and never trigger rollover. Starting a
fresh cup campaign each season is currently an **admin action**, not automatic
([Ch. 8](08-cups.md), [Ch. 30](30-roadmap-and-open-decisions.md)).

## 7.7 FAQ

- **Do I start every season manually?** Only season #1 per league.
- **When does the new season kick off?** After `lastFixtureDay + gap`, at its
  fixtures' `scheduled_at` on the world clock.
- **How is the champion decided?** Points → GD → GF → name.
- **Can I change the off-season?** Yes, before the final matchday lands (read at
  rollover).

## Connections

- Pacing of the new season's fixtures: [Chapter 6](06-leagues-and-scheduling.md).
- Day counter that activates seasons: [Chapter 4](04-world-clock-and-time.md).
- Code: `internal/competition/{seeding,rollover,season,membership}.go`; tests `competition_integration_test.go` (`TestStartSeasonAndApplyResultAndRollover`, `TestOffSeasonGapRolloverAndActivation`).
- Source: `docs/how-to/seasons.md`, IM01, IM11, IM14, IM24, OPD-36, OPD-40, OPD-50.

---
[← Leagues](06-leagues-and-scheduling.md) · [Contents](the-touchline-book.md) · [Next: Cups →](08-cups.md)
