# IM22 — Staggered, human-aware kickoff scheduling

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (kickoff scheduling)
**Source:** User requirement — a competition round must not all kick off at once:
human-managed clubs play in the evening, AI-only fixtures scatter through the
day including a ~23:00 slot, at most a few fixtures of one league are live at a
time, and a round spreads across days (like a European matchweek) instead of one
shared kickoff moment.
**Depends on:** IM05 weekday resolution (`scheduling_rules->'allowed_weekdays'`),
IM11 kickoff anchoring, IM16 continuous world clock + intra-day kickoff poll,
IM18 `Runner.KickoffDue`/`RunLive`, and the club `is_ai_controlled` flag on
`club.clubs`. No migration: every new key lives on the existing
`competition.competition_rules.scheduling_rules` JSONB.

## What to do

Give each competition round per-fixture kickoff slots instead of one shared
stamp: resolve a `human_kickoff_hours` pool (default `[18,20]` UTC) and an
`ai_kickoff_hours` pool (default `[12,15,17,23]` UTC) per competition, spread a
round across the resolved `allowed_weekdays` when that set has ≥ 2 days, cap
how many of a competition's fixtures may be live at once at kickoff time
(`max_simultaneous_matches`, default 3), and pin the season-final matchday of a
league to one day, one evening hour (default 20:00 UTC) so the title race plays
together. Default it **on** for every league and cup; opt out per competition
with `scheduling_rules->>'staggered' = 'false'`.

## Delivery evidence

- `backend/internal/competition/service.go` — IM22 defaults as package vars:
  `DefaultHumanKickoffHours = [18,20]`, `DefaultAIKickoffHours = [12,15,17,23]`,
  `DefaultWeekdays = [5,6,7,1]` (Fri/Sat/Sun/Mon), `DefaultMaxSimultaneous = 3`,
  `DefaultFinalKickoffHour = 20`.
- `backend/internal/competition/pacing.go`:
  - `scheduleParamsResolved` extended with `humanHours`, `aiHours`,
    `maxSimultaneous`, `finalKickoffHour`, `staggered`; `scheduleParams` reads
    `staggered`, `max_simultaneous_matches`, `final_kickoff_hour`,
    `human_kickoff_hours`, `ai_kickoff_hours` from `scheduling_rules` with the
    defaults above, and injects `DefaultWeekdays` as the fallback when
    `staggered` is true and nothing resolves (the IM05 fallback was "no
    weekdays = legacy day formula"; the staggering default changes that to the
    4-day set).
  - `resolveHourPool` + `cloneWeekdays`/`cloneHours` helpers.
  - `Service.StaggeredCap(ctx, q, worldID, competitionID, openMatchday) (cap,
    maxMatchday, err)` — resolves `max_simultaneous_matches` (default 3) but
    returns `0` unless the competition is staggered **and** resolves ≥ 2 allowed
    weekdays **and** `openMatchday < MAX(matchday)` of its fixtures. A `0` cap
    means "the whole open round may kick at once" — that is what makes
    single-day/legacy rounds and the season-final matchday behave.
- `backend/internal/competition/stagger.go` (new) — the pure slot assigner:
  - `assembleRoundKickoffs(p, fixtures, anchor) []time.Time` packs a round's ties
    into `(allowed weekday × hour)` slots. Capacity is **one fixture per slot**
    (so a 10-tie round over a 4-day × 4-hour grid spans 2–3 days), keys for
    determinism, and never mixes pools: a human-involving tie takes the human
    pool (evening) on its own days, an AI-only tie takes the AI pool; a round
    too big for the first day's slots advances to the next allowed weekday
    **within the same pool**. The first tie always lands on the round's anchor
    day, so `MIN(scheduled_at)::date` matches the pinned round day (IM11).
  - `roundAnchorFirst(seed, compID, weekdays, seasonAnchor)` and
    `roundAnchorNext(weekdays, lastRoundEndDay)` — the round walk: round 1 =
    first allowed weekday after the season anchor; round k+1 = first allowed
    weekday ≥ 2 game-days after round k's last kickoff day. `roundLastDay`
    computes that bound.
- `backend/internal/competition/seeding.go` — `createFixtures` loads each
  club's `is_ai_controlled` flag into a human map and picks the branch per
  round: staggered multi-day (`assembleRoundKickoffs`), single-day weekday
  (`paceWeekdayMatchday`, unchanged), or the legacy IM03 day formula (unchanged,
  reached when nothing resolves to weekdays — i.e. non-staggered unconfigured
  competitions). The final round in every mode goes to the league's
  `final_kickoff_hour` on a single day (all fixtures at once), so a league
  always ends on a simultaneous even kickoff.
- `backend/internal/competition/cup.go` — `materializeRound` staggers cup ties
  the same way for cups whose params are staggered, anchored to the cup's
  planned round day (`plan.Date` or the legacy day), snapped to an allowed
  weekday when the anchor falls off it. Cup finals are not forced single-evening
  (the final-evening rule is a league rule; cup progressions have their own
  date policy).
- `backend/internal/competition/scheduling.go` — `repaceLeague` re-reads ties
  with `(home, away, kickoff, status)` and restamps staggered rounds per-tie
  through `assembleRoundKickoffs` (deterministic in `(home, away)`, so repacing
  a pristine staggered season is a no-op), single-day rounds keep their one
  stamp, and final-round restamps go to `final_kickoff_hour` regardless.
- `backend/internal/match/live.go` — `Service.KickoffFixtureIDs(ctx, ids)`
  kicks an explicit set of fixtures (each `kickoffFixture` is idempotent);
  `KickoffMatchday` now delegates to it.
- `backend/internal/matchday/runner.go` — `KickoffDue` reworked:
  - `dueFixtureGroups` lists due **fixtures** (`scheduled_at ≤ worldNow`) grouped
    by `(competition_id, matchday)` instead of whole matchdays, so a staggered
    round's later lanes kick only when their own slot matures.
  - Per-competition decisions in `admitKickoffs`: the **round-order gate** (a
    competition whose `matchday < open` still has a live fixture defers its
    whole open round — never two rounds live at once, which keeps standings,
    qualify and six-pointer applies strictly round by round) replaces the old
    world-wide `worldHasLive` gate; then `StaggeredCap` limits the open round's
    admission, kicking the earliest-scheduled due fixtures first under the cap.
  - `Summary.Skipped` now counts **competition-round groups deferred** (gate or
    cap), and `Matchdays` counts distinct matchdays actually kicked.
- Tests:
  - `backend/internal/competition/stagger_test.go` (new, unit) —
    `TestAssembleRoundKickoffsAnchorManyDays` (a round too big for one day
    spills onto the next allowed weekday), `TestAssembleRoundKickoffsPools`
    (human ties evening-pool, AI ties day-pool, both pinned to the anchor day),
    `TestAssembleRoundKickoffsDeterminism`, `TestRoundAnchorWalk`.
  - `backend/internal/competition/competition_integration_test.go` — existing
    calendar tests (`TestFixturePacing`, `TestFixturePacingWorldCalendarFallback`,
    `TestSeasonCalendar`, `TestWeekdayPacingAndRePace`,
    `TestCupCalendarAnchoredToLeagueEnd`) opt out via the new `optOutStaggered`
    helper where they pin legacy dates, and new tests cover the default
    staggered calendar (`TestStaggeredDefaultCalendar`), rounds spanning days
    (`TestStaggeredRoundSpansDays` with `assertRoundGaps`), repacing reproducing
    the spread (`TestStaggeredRePaceReproducesSpread`), and
    `TestStaggeredOptOutKeepsSingleDay`. `TestScheduleParamsResolvesIM22DefaultsAndOverrides`
    pins the defaults + override resolution, including the built-in weekday set.
  - `backend/internal/competition/kickoff_integration_test.go` —
    `TestStartSeasonKickoffPinsMatchdayOne` opts out; the IM11 "kicks off next
    day" assertion is now `TestStartSeasonDefaultKicksOffNextAllowedWeekday`
    (the anchored round-1 day is the first allowed weekday after the anchor).
  - `backend/internal/matchday/runner_integration_test.go` —
    `TestRunnerAdvancesMatchdaysAndRollsOver` updated to fixture-level passes
    under a staggered calendar (passes carry whichever lanes are due; the second
    lane of a round matures a few game-hours later), the no-overlap block now
    exercises the **round-order gate** directly, and two new tests:
    `TestRunnerCapOnlyForStaggered` (4 due fixtures of a staggered league → 3
    live + 1 deferred; the same league opted out kicks the deferred one next
    pass) and `TestRunnerFinalRoundBypassesCap` (a due final matchday kicks all
    4 at once). Runner tests inject due fixtures borrowing another league's club
    pairings (`insertDueFixtures`) so they never collide with the real calendar.
- Verify: `gofmt -w`, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` all
  green. (Integration tests are compile-gated locally — no `TEST_DATABASE_URL`
  or local Postgres/Docker — and run against a live Postgres in CI.)
- Docs: this file, `docs/how-to/cadences-and-time.md` §8 (staggered kickoffs),
  `docs/product_manager.md` OPD-48.

## Recorded decisions

- **Staggering is the default, and the weekday fallback changes with it.** A
  competition that resolves nothing to `allowed_weekdays` used to get the exact
  legacy IM03 day formula; under IM22 it gets the built-in `{5,6,7,1}` set and a
  multi-day calendar. The blast radius is intentional (user-approved):
  unconfigured leagues change pacing. The callers that must keep legacy dates
  opt out explicitly.
- **A round spans multiple days iff it resolves ≥ 2 allowed weekdays.** One
  weekday, `staggered=false`, or the legacy no-weekday fallback all mean the
  whole round shares one day (and the single-kickoff paths stay byte-for-byte).
  Cups follow the same rule; the season-final matchday is an exception for
  leagues — one day, one time (default 20:00 UTC), whole round at once.
- **Slots hold one fixture; the cap lives at kickoff.** The assigner packs one
  tie per `(weekday, hour)` slot — not `max_simultaneous_matches` per slot — so
  the calendar for a big league is a real spread-over-days matchweek. The
  equivalent of "max 3 live" is enforced when the runner admits kickoffs, in
  real time, where the actual live count is known. This split emerged during
  implementation: a cap sized at the schedule is unfalsifiable (the live count
  changes by the time you kick), while a kickoff-time cap is exactly the user's
  "no more than N matches of my league running at once".
- **Pools never mix, and days advance within the pool.** A human-involving tie
  is never shoe-horned into a day slot; when a round outgrows the first day,
  ties advance to the next allowed weekday **within their own pool** (human ties
  stay evening, AI stays day). The AI pool's late `23` slot is the "midnight
  kickoff" tail of a matchweek.
- **The round-order gate replaces the world-wide no-overlap gate.** Before IM22,
  *any* live fixture in a world blocked *every* later kickoff. Now a competition
  only waits on **its own** earlier rounds, so two unrelated competitions can
  play in parallel. The gate also guarantees standings, qualify and six-pointer
  results apply strictly round-by-round within each competition.
- **The season-final kicks together regardless.** `StaggeredCap` returns 0 the
  moment the open matchday equals the competition's `MAX(matchday)`, and
  `createFixtures`/`repaceLeague` stamp the final round at `final_kickoff_hour`
  (default 20:00 UTC) on one day — the title race and relegation finale play at
  once, the way real final round evens do, even in a staggered league.
- **No migration, no new config keys beyond `scheduling_rules` JSONB.** All IM22
  knobs resolve through the existing per-competition → country →
  built-in-default chain, and repacing stays deterministic in `(home, away)` —
  repacing a pristine staggered season is a no-op.

## Notes / risks

- The cap test's second pass relies on the deferred fixture staying the only
  due one (the real season is game-days ahead), and runner tests inject due
  fixtures read from *another* league's pairings so a `(competition_id,
  matchday, home, away)` unique index can never fire.
- The earlier live-finalize fix (`match/live.go`,
  `match/live_integration_test.go`, `player/morale.go`) remains in the working
  tree, uncommitted, at the user's request.