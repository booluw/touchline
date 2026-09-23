# IM05 — Weekday-aware fixture scheduling: allowed weekdays, cup anchoring, live re-pacing

**Status:** Implemented
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** IM01 (season lifecycle + off-season anchors); IM03 (day-formula
pacing + season calendar); IM04 (domestic cup bracket + golden goal — the cup
calendaring seam `cupPlan.ladder` and `materializeRound`)

## What to do

Give the admin real control over **when** fixtures are played. Today the
calendar follows a mechanical day formula (IM03): 3 matchdays in a 7-game-day
week on days 1, 3, 5. IM05 replaces that with a **weekday set the admin
picks** — Mon to Sun — resolved per competition first, falling back to the
country default, and defends two structural rules no matter what set is
chosen:

1. **A club never plays two matches closer than two game-days apart**
   (a full rest day minimum), across every competition in their week.
2. **Cup ties never land on a day the league plays** — the league calendar is
   the country's anchor. A cup round is placed on a league-free day that keeps
   the two-day rest, and the cup **final** closes a few days past the league
   season's last fixture.
3. **A league can be re-paced mid-life**: an admin change to its weekdays
   freezes every matchday with a live/completed fixture and slides the
   unstarted matchdays forward onto the new weekdays, keeping order and the
   kickoff-hour rotation.

Nothing is configurable unless it has to be: the two-day rest is structural
(not a setting), clearing a weekday set restores the old day-formula rules,
and an unconfigured season reproduces the IM03 calendar byte-for-byte.

## Behaviour

### `allowed_weekdays`: the one new knob

- A weekday set is a JSONB array of ISO weekdays, `1`..`7` (`1` = Monday …
  `7` = Sunday), stored under `scheduling_rules->'allowed_weekdays'` for a
  league/cup and under `world.countries.default_scheduling_rules->
  'allowed_weekdays'` for a country default.
- Resolution order (per competition, at season materialization and on
  re-pacing): **per-competition override → country default → none** (legacy
  IM03 day formula). Explicitly clearing a per-competition override
  (`jsonb 'null'`) restores the fallback; clearing a country default restores
  the day formula for its fallback leagues.
- Weekday sets are validated to `1..7`; out-of-range or duplicate entries are
  dropped, and a set that normalizes to empty behaves as "not set".

### League pacing on a weekday set

- Matchday 1 = the **earliest allowed weekday on/after the day after the
  season anchor** (the anchor is `COALESCE(worlds.launched_at, created_at)`,
  the same ref the day formula uses).
- Each later matchday = the **earliest allowed weekday at least two game-days
  after its predecessor**, so consecutive matchdays are ≥ 2 days apart by
  construction even when the set is sparse (e.g. only Saturdays).
- Kickoff times reuse the deterministic per-matchday rotation from the world
  seed (`kickoffHour`), so a weekday-paced season still varies kickoffs.
- With no weekday set, the fixtures use the unchanged IM03 `scheduledAtFromDay`
  formula — `TestFixturePacing` stays green.

### Cup calendaring: anchored backward from the final

- `cupPlan` round entries (the persisted IM04 ladder) now carry an ISO `date`
  (`roundPlan.Date`, `omitempty` — nil keeps the legacy weekly placement for
  countries with no league season to anchor against).
- **Anchor**: the final is the first allowed weekday at least **3 game-days
  after the country's latest league fixture** (any non-cancelled country-league
  fixture; the gap sits before IM01's off-season rollover window).
- **Backward walk**: working from the final, each earlier round is placed
  `cupGap` game-days back — a seeded 2-3 day gap where the round directly
  before the final draws 3 with 75% (P(3)=3/4), one step out 50/50, and every
  earlier round takes the flat 2-day minimum. The draw is seeded by
  `world_seed ⊕ cup_id ⊕ round` (same `roundSeed` as the bracket draw), so the
  same cup + world reproduces the same dates.
- **Snapping**: the round lands on the best league-free day inside
  `[target-2, min(target+2, nextRoundDate-2)]`: it never picks a league day
  while any free day survives in the window, prefers the furthest full-rest
  clearance (≥ 2 days from the nearest league day, then the adjacent case),
  honors the allowed-weekday set when the cup declares one (falling back to
  any fit only if no allowed weekday exists — anchoring beats never playing),
  and keeps the two-day rest from its successor. In a fully packed window the
  best-available clearance wins (degraded but still placed); cross-league
  nudging is explicitly **not** done in this pass.
- Materialization (`materializeRound`) prefers the plan's `date` (keeping the
  cup's kickoff-hour rotation); nil falls back to the IM04 weekly formula.

### Live league re-pacing (freeze + slide)

- `PATCH /leagues/:id/scheduling` with a non-empty weekday set re-paces the
  league: any matchday containing a `live` or `completed` fixture is **frozen**
  with its existing dates; only entirely-unstarted matchdays move.
- The first unstarted matchday lands on the earliest allowed weekday ≥ 2
  game-days after the frozen horizon's last fixture (or, with nothing played,
  mirrors fresh materialization from the anchor + 1); each subsequent matchday
  steps 2 game-days off its predecessor. Matchday *order* and the kickoff-hour
  rotation are always preserved; the move is strictly **forward**.
- **Clearing** the override (empty array) persists the rules and does **not**
  move fixtures. A country-default change re-paces every league of that country
  that has **no override of its own** — one admin action propagates across the
  whole calendar; clearing the default just persists.
- Cups: `PATCH /cups/:id/scheduling` persists the set; existing cup dates
  stand until the next round materializes (the anchored walk re-stamps future
  campaigns).

### Country-scoped scheduling news

- `world.news_stories` gains a nullable `country_id`; stories with one are
  seen only by that country's feeds, `NULL` stories stay world-wide.
- Cup round materialization and any league re-pacing that moves fixtures
  publish a category `scheduling` story for the owning country (written in the
  same transaction, so feeds never report a calendar the engine didn't keep).
- The manager feed (`GET /api/news`) shows world-wide stories plus the
  caller's own club country's stories.

## Changes

### Migration `0051`

- `world.countries.default_scheduling_rules JSONB` (nullable).
- `world.news_stories.country_id UUID REFERENCES world.countries(id)
  ON DELETE CASCADE` + `idx_news_country_published(world_id, country_id,
  published_at DESC)`.
- Category CHECK re-declared to include `'scheduling'` (default constraint
  name `news_stories_category_check`).

### internal/competition

- `weekdays.go` (new): ISO-weekday math (`isoWeekday`, `normalizeWeekdays`,
  `isAllowedWeekday`, `nextAllowedWeekday`, `kickOff`, `paceWeekdayMatchday`),
  persistence (`weekdaysJSON`, `decodeWeekdays`), resolution
  (`resolveAllowedWeekdays`, `competitionCountry`).
- `pacing.go`: `scheduleParamsResolved.allowedWeekdays`; `scheduleParams`
  resolves the league's own override → country default.
- `seeding.go`: `createFixtures` uses `paceWeekdayMatchday` when a weekday set
  is present, else the untouched IM03 formula.
- `cup.go`: `roundPlan.Date`; `cupGap` (seeded, near-final 3-biased);
  `countryLeagueDays`; `dayClearance`; `findCupRoundDay`; `planCupCalendar`;
  `materializeRound` honors `plan.Date`; **F==X join-guard fix** — when the
  bottom pool already equals X the late entrants join at Round 1 and the plan
  marks itself `Joined` so `applyKnockoutResult` never joins them again.
- `scheduling.go` (new): `UpdateCountryScheduling`, `UpdateLeagueScheduling`,
  `UpdateCupScheduling` (+ `pendingRulesWeekdays` upsert, `fallbackLeagues`,
  `requireCompetition`, `repaceLeague`, `RescheduleResult`,
  `publishSchedulingNews`, `competitionName`).
- `scheduling_test.go` (new): unit coverage for all pure helpers + invariants.

### internal/httpapi + openapi + frontend

- `router.go`: `PATCH /admin/worlds/:id/countries/:countryID/scheduling`,
  `PATCH /admin/leagues/:id/scheduling`, `PATCH /admin/cups/:id/scheduling`
  (all `requireAuth` + `requireAdmin`).
- `scheduling_handlers.go` (new): three handlers, body `{allowed_weekdays: [1..7]}`.
- `openapi.yaml`: the three documents (route-coverage test `TestDocsCoverRouter`
  green).
- `admin/rename.go` `News`: optional country scope; `admin_club_handlers.go`
  `handleNews` resolves the caller's club country (`callerCountry`) and serves
  `country_id IS NULL OR country_id = <club country>`.
- Frontend `admin/competitions.vue`: Mon-Sun chip pickers + Apply for country
  default, per-league override, and per-cup override; `useCompetition.ts` adds
  the three `update*Scheduling` calls.

### docs

- `docs/tasks/improvements/IM05-weekday-fixture-scheduling.md` (this file).
- `docs/how-to/seasons.md`, `docs/how-to/cup-competitions.md`,
  `docs/how-to/glossary.md`, `docs/product_manager.md`: weekday resolution,
  anchored cup final, re-pacing freeze/slide, rest floor, scheduling news.

## Tests

- Unit (`internal/competition/scheduling_test.go`): `normalizeWeekdays`,
  `nextAllowedWeekday`, `paceWeekdayMatchday` (allowed-weekday membership +
  ≥ 2-day gaps for sparse sets), `cupGap` determinism + near-final bias
  (dist-1 draws 3 more often than dist-2; dist ≥ 3 and the final itself are
  always 2), `dayClearance`, `findCupRoundDay` (never a league day while a
  free day fits, two-day rest from successor, allowed-weekday honoring),
  `cupLadder` plans carry nil dates (IM04 ladder equality unchanged).
- Integration (`competition_integration_test.go`; `integration` tag, CI-only):
  `TestWeekdayPacingAndRePace` — country weekend default → season on Mon/Sun
  weekdays with valid gaps and hours; freeze matchday 1; league override
  Mon/Wed re-paces 2..N forward off the frozen day, hours preserved, a
  country-scoped `scheduling` story is published. `TestCupCalendarAnchoredToLeagueEnd`
  — campaign rounds land only on allowed weekdays, never on league days, with
  the final clearing the league end and ≥ 2-day round gaps.
- Regression: `TestFixturePacing`, `TestFixturePacingWorldCalendarFallback`
  (unconfigured ⇒ exact IM03 calendars), all IM04 cup/ladder tests,
  `TestDocsCoverRouter`.
- Frontend: `pnpm typecheck` + `pnpm lint` on touched files (repo-wide
  failures on unrelated pages are pre-existing).

## Recorded decisions

- **Weekdays only, hard rest**: the knob is `allowed_weekdays`; the two-day
  rest floor is structural and never exposed as a setting.
- **Country default + per-competition override**, resolution per-competition →
  country → legacy formula; clearing restores the fallback.
- **Cup schedule is anchored, not layered**: the final sits 3+ days after the
  league's last fixture (weekday-snapped), earlier rounds walk backward on
  seeded 2-3-day gaps *snapped away from league days*. The league calendar is
  never moved to fit a cup in this pass; a packed window degrades to the
  best-available clearance.
- **Live re-pacing is freeze + slide, forward only**, order/hours preserved;
  clearing never moves fixtures, and cups keep their dates until the next
  materialization.
- **Scheduling news is country-scoped** and transactional with the fixtures it
  describes; the manager feed shows world-wide + own-country stories.
- **F==X join resolved**: if the bottom pool already equals X the late
  entrants play from Round 1 and the plan is marked `Joined` to keep the
  knockout from joining them a second time (IM04 leftover fixed).
- Cross-league fixture nudging as a last resort, per-club conflict resolution
  beyond the two-day floor, and weekday-aware cup *re-materialization* on rule
  change are recorded for later tasks.
```