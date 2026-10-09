# Chapter 6 — Leagues, fixtures and scheduling

## 6.1 Leagues

A league is an admin-declared `competition.competitions` row
(`competition_type = 'league'`) per country, with `tier`, an even
`team_count ≥ 4`, and optional `promotions` / `relegations` and
`promotes_to` / `relegates_to` links. The engine **invents nothing**: every
size and movement number is the admin's declaration (OPD-20, resolving OPD-01).

### Adjacency rules

- Links must point to a league **in the same country** (`ErrBadAdjacency`).
- Counts must be **symmetric** across each edge: the league above relegates
  exactly as many as the league below promotes (`ErrAdjacencyMismatch`).
- A league with no neighbours must declare zero movement.
- Capacity changes auto-adjust the neighbour's reciprocal count (IM14).

### Memberships

`competition.club_competitions` rows tie clubs to competitions:

| `role` | Meaning |
| --- | --- |
| `league` | the club's domestic league seat. Written **once per club for life** (`uq_club_one_league`). A promoted club keeps its now-stale row, so a club's *current* league is derived from the latest non-completed season entries, never from the membership alone. **Job offers require a `league` membership.** |
| `cup` | a cup entry; **at most 3 per club** (`ErrCupLimit`), champions and cascade replacements exempt ([Ch. 8](08-cups.md)) |

A **league-less club** holds no `role='league'` row anywhere; only such clubs
can be admitted by an admin or drawn by rollover auto-fill.

## 6.2 Fixture generation

Season creation ([Ch. 7](07-seasons-and-rollover.md)) generates a
**deterministic double round-robin**: every pair meets home and away;
`matchday` = round index + 1. Determinism comes from the world seed (OPD-22).
Season creation also writes one zeroed `competition.standings` row per entrant,
so every club appears in the table from day one.

## 6.3 Pacing: which day each matchday lands on

There are three layers; the first that applies wins.

### Layer A — staggered, human-aware slots (IM22, default)

When a competition resolves **≥ 2 allowed weekdays** (the default set for an
unconfigured competition is **{5, 6, 7, 1}** = Fri/Sat/Sun/Mon), a round is a
*spread* of kickoff slots across those days:

- Human-involving ties (either club `is_ai_controlled = false`) use
  `human_kickoff_hours` — default **18:00, 20:00 UTC**.
- AI-only ties use `ai_kickoff_hours` — default **12:00, 15:00, 17:00, 23:00
  UTC** (23:00 is the "midnight kickoff" tail).
- Pools never mix. Ties sorted by `(home, away)` fill one slot each; overflow
  advances to the next allowed weekday **within its own pool**.
- Round 1 = first allowed weekday after the season anchor; each later round =
  first allowed weekday ≥ **2 game-days** after the previous round's last
  kickoff day.
- **The league's final matchday plays together** on one day at
  `final_kickoff_hour` (default **20:00 UTC**) so the title race finishes
  simultaneously.
- Opt out with `scheduling_rules->>'staggered' = 'false'`.

### Layer B — weekday-aware pacing (IM05)

`allowed_weekdays` (ISO `1`=Mon … `7`=Sun) resolves **per competition →
country default → legacy formula**:

- `competition_rules.scheduling_rules->'allowed_weekdays'` (league or cup), else
- `world.countries.default_scheduling_rules->'allowed_weekdays'`, else
- Layer C.

With a set: matchday 1 = earliest allowed weekday ≥ 1 day after the anchor;
each later matchday = earliest allowed weekday ≥ **2 game-days** later. The
two-day rest is **structural** (never configurable).

### Layer C — the legacy day formula (IM03)

```
game_day(k) = floor((k − 1) × days_per_week / matchdays_per_week) + 1
```

Defaults `matchdays_per_week = 3`, `days_per_week = 7` (falls back to the
world's `calendar.days_per_week`) → days 1, 3, 5, 8, 10, 12, … Kickoff hour
rotates through `kickoff_hours` (default `[15, 18, 20]`) as
`kickoff_hours[(k − 1 + base) % len]`, `base` derived from `world_seed ⊕
league_id`.

### Where the knobs live

All in `competition.competition_rules.scheduling_rules` (JSONB):

| Key | Default | Layer |
| --- | --- | --- |
| `staggered` | true when ≥2 weekdays | A |
| `human_kickoff_hours` | `[18, 20]` | A |
| `ai_kickoff_hours` | `[12, 15, 17, 23]` | A |
| `max_simultaneous_matches` | `3` | A (kickoff admission) |
| `final_kickoff_hour` | `20` | A |
| `allowed_weekdays` | `{5,6,7,1}` built-in | A/B |
| `matchdays_per_week` | `3` | C |
| `days_per_week` | world `calendar.days_per_week` | C |
| `kickoff_hours` | `[15, 18, 20]` | B/C |
| `off_season_ticks` | world `season.off_season_ticks` | [Ch. 7](07-seasons-and-rollover.md) |

Rules are read **at season creation** (and re-read when serving the calendar),
so the calendar is stamped onto the fixtures.

## 6.4 Re-pacing a live league

- `PATCH /api/admin/leagues/:id/scheduling` with a non-empty weekday set
  re-paces a live league: matchdays with a `live`/`completed` fixture are
  **frozen**; unstarted matchdays slide **strictly forward** onto the new
  weekdays, order and kickoff hours preserved. Clearing the set persists but
  never moves fixtures.
- `PATCH /api/admin/worlds/:id/countries/:countryID/scheduling` changes the
  country default and re-paces every league without its own override.
- A re-pace that moves fixtures publishes a country-scoped **`scheduling`
  news story** in the same transaction ([Ch. 26](26-dashboard-news-scouting-realtime.md)).

## 6.5 Kickoff admission (how many matches run at once)

The matchday runner ([Ch. 16](16-matchday-and-live-matches.md)) enforces two
per-competition rules (IM22, replacing an old world-wide "any live match blocks
everything" gate):

1. **Round-order gate** — a competition never kicks a round while an earlier
   round still has a `live` fixture.
2. **Simultaneity cap** — a *staggered* competition admits at most
   `max_simultaneous_matches` (default 3) of the due round's earliest ties;
   the rest wait. The cap does not apply to single-day rounds, legacy pacing,
   rounds whose ties share one kickoff, or the season-final matchday.

## 6.6 Standings

Ordering everywhere: **points DESC → goal difference DESC → goals for DESC →
club name**. The champion is `order[0]` in the `SEASON_COMPLETED` payload.
(The job-offer `league` block uses a simpler points + id tiebreak —
[Ch. 22](22-managers-and-job-offers.md).)

Derived labels used by other systems:

- **Six-pointer** — both clubs share a promotion or relegation battle band
  (feeds match stakes, [Ch. 16](16-matchday-and-live-matches.md)).
- **Dead rubber** — nothing at stake (ambient motivation drag).
- **Derby** — see [Chapter 24](24-social-and-rivalries.md).

## 6.7 Reads

| Route | Returns |
| --- | --- |
| `GET /api/competitions` | declared competitions (world-scoped) |
| `GET /api/competitions/:id/standings` | table (with `season_id/label/number`); each row has `position` and `form` (last ≤5 league results this season, newest first — IM55) |
| `GET /api/managers/me/competitions/:id/standings` | the same table cut to 3 above / 3 below the caller's club; shifts at the edges to keep 7 rows (IM60) |
| `GET /api/managers/me/competitions/:id/outlook` | what is at stake + projected finish (§6.8, IM56/IM57) |
| `GET /api/competitions/:id/fixtures` | fixtures |
| `GET /api/competitions/:id/calendar` | active season grouped by game-week (`week = game_day ÷ days_per_week`) — league-shaped |
| `GET /api/clubs/:id/fixtures` | one club's fixtures across all competitions; `?upcoming=true&limit=N` returns only scheduled ones, each with a `difficulty` (§6.9, IM59) |
| `GET /api/clubs/:id/next-fixture` | next match + opponent dossier ([Ch. 26](26-dashboard-news-scouting-realtime.md)) |
| `GET /api/admin/competitions/:id/detail` | admin dossier: history, past winners, top scorers (IM15) |

## 6.8 League outlook: stakes, clinching and projection (IM56/IM57)

`GET /api/managers/me/competitions/:id/outlook` is a pure read for the
caller's club. It never claims what the maths has not proven.

**Finish range.** With `max = points + 3 × league games left`:
- `best  = 1 + clubs whose points already exceed our max`;
- `worst = 1 + clubs whose max reaches our current points`. **Equal points
  count against us** — goal difference can still change, so a one-point lead
  over a club that can still win is never "champions".
- Once every league game is played the final table (with tiebreaks) decides.

**Races.** Title `[1,1]`; promotion `[1, promotions]` and relegation
`[N−relegations+1, N]` only when the league has those slots; every
`cup_qualification` band on the league (any scope — domestic, regional,
continental, international) as a qualification race. A race is `clinched`
when the whole finish range sits inside it, `eliminated` when none of it can
(for relegation that means safe), otherwise `alive`. Copy follows the status:
clinched → "Champions / Promoted / Relegated / Qualified for X"; alive →
"on course for". `guaranteed_at_least` names the cup of our worst finish when
every still-possible finish qualifies for some cup ("qualified for at least
the Regional Cup"). Qualification here is the league position only; actual
entry still runs through cup qualification and double-booking (Ch. 8).

**Stakes card.** The first race not settled against us, in the order title →
promotion → relegation → nearest cup band. Early in a season the title race
is mathematically alive for most clubs, so most clubs see it; `races` lists
every race for clients that want a different emphasis.

**Next match.** Positions after a 1-0 win, 0-0 draw or 0-1 loss in our next
league fixture; the opponent's points move too, every other club stands still.

**Projection.** 1000 seeded runs of the remaining league fixtures. Each result
is drawn from the home side's home points-per-game against the away side's
away points-per-game, both shrunk toward the league average by 3 games, with a
fixed 26% draw rate. Seed = season id + completed fixtures, so the answer is
stable for a matchday. Returns the median finish and the 10th–90th
percentile range. No cache: one run is milliseconds.

**Why factors** (positive helps us): remaining games vs the current top 6
against an even schedule; home points vs the league home average; best-XI
players unavailable for the next match; goal-difference trend (last 5 vs
season pace); chance-quality trend (xG difference, last 5 vs season — from
IM58, omitted until our matches carry xG).

## 6.9 Fixture difficulty (IM59)

`?upcoming=true` rates each fixture for the club in the path on one scale of
overall-rating points: **strength gap** (opponent's best available XI mean
overall minus ours, on that matchday — injuries and suspensions count),
**venue** (+2 away, −2 home; fixtures carry no neutral venue), and **opponent
form** (their last-5 points above a neutral 7, × 0.5, all competitions).
Score buckets: ≤ −8 Very easy, ≤ −3 Easy, ≤ 2 Even, ≤ 7 Hard, else Very hard.
Ratings, not league positions, so cup ties across divisions compare fairly.

## Connections

- Season creation and rollover: [Chapter 7](07-seasons-and-rollover.md).
- Cups reuse memberships, pacing and the bracket machinery: [Chapter 8](08-cups.md).
- Kickoff timing is on the world clock: [Chapter 4](04-world-clock-and-time.md).
- Code: `internal/competition/{seeding,pacing,calendar,weekdays,scheduling,standings,membership,outlook,outlook_load,difficulty}.go`.
- Source: `docs/how-to/seasons.md`, `docs/how-to/cadences-and-time.md` §8, OPD-20, OPD-22, OPD-33, OPD-48, OPD-63, IM03, IM05, IM22, IM55–IM60.

---
[← Worlds](05-worlds-and-setup.md) · [Contents](the-touchline-book.md) · [Next: Seasons →](07-seasons-and-rollover.md)
