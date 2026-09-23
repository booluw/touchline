# IM04 — Domestic cup: knockout staging, N/X eligibility, and golden-goal ties

**Status:** Not started
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** S04-01 (StartSeason seam + rollover); IM01 (season lifecycle);
IM03 (fixture pacing + calendar reads — cup rounds reuse the pacing machinery)

## What to do

Deliver the **system-seeded domestic cup**: an admin-declared knockout
competition that draws its entrants from a country's league clubs, stages them
with a configurable late-entry rule, resolves ties with a **golden goal**, and
advances the bracket round by round — without touching the league standings or
rollover path. This is the first real consumer of the cup schema hooks that
migrations `0012`/`0037` reserved (role `cup`, `format 'knockout'`,
`qualification_rules`, entries `qualified`/`eliminated`/`champion`), and it
lays the bracket + entry machinery that the later manager-created competition
flow (S10-02) will reuse.

1. **Admin creates a domestic cup** (`POST` endpoint): country, name, the two
   staging variables, prize pool, and scheduling rules.
2. **Eligibility is automatic and staged** (manual-session decision): *all*
   clubs with a league membership in the country are eligible; the **top N
   teams of the first tier join late** — they enter the draw once **X
   bottom-tier survivors** remain. **N and X are admin-configurable.**
3. **Deterministic bracket** built from the world replay seed (same contract
   as league seeding): Round 1 draws the bottom-tier pool with byes, later
   rounds materialize lazily from the known advancing set.
4. **Golden goal** (manual-session decision): a drawn cup tie is decided by a
   sudden-death goal after the 90-minute match — deterministic per match seed;
   no penalties, no extra-time periods.
5. **Knockout result application** advances winners (`qualified`), eliminates
   losers (`eliminated`), crowns a `champion` and closes the cup season — a
   branch of the existing `ApplyResult` choke point, never the standings/
   rollover path.
6. **Reads + frontend**: cup campaign view (rounds → ties → winner), manager
   cup list; club fixtures already listing cup ties via IM03.

## Behaviour

### The two staging variables (`qualification_rules`)

- Eligible pool P = every club holding a `role='league'` membership in a league
  whose `country_id` equals the cup's country (all tiers).
- **N** = the number of first-tier clubs that **do not** play the early rounds
  (a bye to a later stage). Ranked by most-recent standings
  (`points DESC, GD DESC, GF DESC, name`); before a tier-1 season has any
  results, ranked by the deterministic club order (seed), then name.
- **X** = the survivor threshold: the early rounds are a knockout among
  P minus the top N until exactly **X** bottom-tier clubs remain. The top-N
  clubs then enter the next round, making a field of **X + N** that plays down
  to a champion.
- Stored on the cup's `competition_rules.qualification_rules` JSONB, e.g.
  `{"entry": "country_league_members", "first_tier_late_entry": {"teams": N, "enter_when_survivors": X}}`.
  The JSONB is populated at creation for introspection; the engine reads it
  back at campaign time so the values are authoritative at both ends.
- Validation (`422`): `N >= 0`, `X >= 1`, `X + N >= 2`, and the construction
  must yield a valid knockout — every round has an even number of ties and the
  final is exactly two clubs. If the bottom pool is already exactly X, the
  early rounds are skipped and the top-N enter immediately. If the bottom pool
  is smaller than X, the cup refuses to start (`422`) until the admin lowers X.

### Bracket generation (deterministic, replay-stable)

- New `internal/competition/cup.go`. Round-1 pairings are drawn from an `rng`
  keyed by `world_seed ⊕ cup_id` over the **canonically sorted** bottom pool;
  byes are assigned so the survivor count lands exactly on X with even ties at
  every subsequent round.
- Later rounds are materialized **lazily in the result transaction**: when the
  last tie of a round completes, the advancing clubs (canonical order) are
  drawn into the next round's fixtures with an `rng` keyed
  `world_seed ⊕ cup_id ⊕ round`. Identical seed + identical tie results ⇒
  identical bracket: replay-reproducible like league seeding.
- Cup rounds reuse IM03 pacing: each round is a fixture day spaced by the
  cup's `scheduling_rules` (default `days_per_week` spacing / kickoff rotation);
  `matchday` doubles as the round index (1-based). No new columns required — the
  nullable `matchday` on `match.fixtures` is the round.

### Golden goal (matchsim)

- New option on `matchsim.Options` (e.g. `GoldenGoal bool`): if the regulation
  90 minutes end level, the engine keeps simulating deterministic sudden-death
  minutes until a goal is produced. The result carries the final score and the
  deciding goal's minute (>90) in the event stream.
- Wired from the fixture's `competition_rules.format = 'knockout'` through
  `FixtureContext` (which already carries `IsCupTie`), so `KickoffMatchday`,
  `Finalize`, and the quick-play `PlayFixture`/`PlayMatchday` paths all agree.
- First cut: the live stream paces regulation 90 minutes unchanged; the golden
  goal resolves inside `Finalize`'s deterministic re-sim, so the JSON feed
  shows 90' and the deciding goal lands in the persisted event stream. Live
  extra-time streaming is a later enhancement.
- A regulation (non-level) result is untouched: golden goal only applies when
  the score is level at 90'.

### Knockout result application

- **`ApplyResult` becomes format-aware** at its existing single choke point
  (the matchday runner calls it for every finalized fixture, live + reconcile,
  `internal/matchday/runner.go:261`):
  - `format = 'round_robin'` → existing standings + `maybeCompleteSeason`
    (untouched, regression-guarded by the IM01/IM03 rollover tests).
  - `format = 'knockout'` → `applyKnockoutResult`: no `competition.standings`
    writes; winner entry → `qualified`, loser → `eliminated`; final tie →
    winner → `champion`, cup season → `completed` + `end_date`; otherwise the
    next round's fixtures are materialized in the same transaction.
- Cup finals **never** trigger the country promotion/relegation cascade.
- The cup campaign is framed by a `competition.seasons` row created at
  campaign start (`status 'in_progress'`), matching the reads the calendar
  endpoints expect.

### Round/home-away and membership cap

- Ties are **single-legged** by default (`is_home_and_away = FALSE` on the cup
  rules); home advantage is drawn deterministically. Two-legged aggregate play
  is recorded for a later task, not built here.
- Membership cap enforced: a club may hold at most **3** `role='cup'`
  memberships (the deferred contract from migration `0037`); exceeding it
  returns `ErrCupLimit` (`409`).

### New endpoints

- `POST /api/admin/cups` `{world_id, country_id, name, first_tier_bye N,
  survivor_threshold X, prize_pool?, scheduling_rules?}` → `201` cup (decorated
  with its qualification rule). Errors: `404` world/country, `422` staging
  validation, `409` name collision.
- `POST /api/admin/worlds/:id/countries/:countryID/cups/:cupID/campaign` →
  `201` campaign created: cup `competition.seasons` (`in_progress`), `role='cup'`
  memberships, `competition_entries`, Round-1 fixtures. A second call while the
  campaign is live → `409` (mirrors `ErrLeagueAlreadySeeded`).
- `GET /api/cups` (manager) → the caller's country cups; `GET /api/cups/:id`
  (manager, world-scoped) → campaign view: competition, season, rounds, each
  tie with `scheduled_at`, `home/away`, `status`, `home_score/away_score`,
  `winner`.

### Frontend

- Admin: a cup-create form (country, name, N, X, prize pool) and a "start
  campaign" action.
- Manager: a cup page rendering the rounds (each tie with its kickoff day/time
  and result, champion highlighted where decided). Bracket-tree rendering is
  optional/stretch. Club fixtures page already shows cup ties (IM03
  `ListClubFixtures`).

## Changes

### internal/competition

- `cup.go` (new): `CreateCup`/`GetCup`/`ListCups`, `qualificationRules`
  (read + validate), `campaign` (season + memberships + entries + R1), bracket
  draw helpers (`drawRound`, bye allocation), `applyKnockoutResult`.
- `standings.go` `ApplyResult`: dispatch on `competition_rules.format`
  (`round_robin` unchanged vs `knockout`).
- `service.go`: `ErrCupLimit`, `ErrCupCampaignExists`, cup validation errors.
- `seeding.go`/`pacing.go`: unchanged — cup fixtures reuse
  `scheduledAtFromDay`/`kickoffHour`.

### internal/matchday + internal/match + pkg/matchsim

- `matchsim.Options`: `GoldenGoal` handling in the simulation driver
  (deterministic sudden-death after minute 90) + events for the deciding goal.
- `FixtureContext` (`internal/squad/squad.go`): knockout signal already via
  `IsCupTie`; the fixture→format lookup reaches `Options.GoldenGoal` uniformly
  in `KickoffMatchday`/`Finalize`/`PlayFixture`.
- Unit seam: `PlayFixture`/`Finalize` must resolve a knock-out tie the same way
  (replay risk — this is the regression hotspot for the golden goal).

### internal/httpapi + openapi + frontend

- `router.go`: cup admin + manager routes (`requireAuth`/`requireAdmin`).
- `openapi.yaml`: the three endpoints + `Cup`/`CupRound`/`CupTie` refs;
  route-coverage test (`TestDocsCoverRouter`) updated.
- `useCompetition.ts` (or `useCups.ts`): `getCups`, `getCup`,
  `createCup`, `startCupCampaign`; manager cup page.

## Tests

- `internal/competition` (unit + integration): staging math (bottom pool →
  X survivors with byes, top-N enters, X+N field, final = 2); field-size
  invariants for 4/6/8/12-club tiers; bracket determinism per
  (seed, cup, round, advancing set); `ErrCupLimit`; campaign double-start
  `409`; `applyKnockoutResult` progression (`qualified`/`eliminated`/
  `champion`, season `completed`), **no `competition.standings` rows, no
  rollover events**; two-cup worlds; the round spacing reuse of IM03 pacing.
- `pkg/matchsim`: level-at-90 ⇒ golden goal decides (soonest goal, minute >90,
  seed-stable); regulation win ⇒ unchanged; `Finalize` re-run equals
  quick-play re-run on the same seed.
- `cmd/api` integration: admin create + campaign, manager reads, 401/404/409/
  422, world-scoped isolation, cup result advancing a round through the HTTP
  surface.
- Regression: the full league path (IM01 rollover anchors day 12 → starts
  17/32, activations 18/33; standings; season calendar) must stay green
  unchanged — `ApplyResult` dispatch is the blast radius.
- Frontend: `pnpm typecheck` + `pnpm lint` on touched files.

## Docs

- `docs/tasks/improvements/IM04-domestic-cup-and-golden-goal.md` (this file).
- `docs/how-to/cup-competitions.md` (new): admin create + campaign, staging
  semantics (N/X), golden-goal rule, round pacing, the mismatch between cup
  rounds and the league week-grouped calendar, recorded decisions.
- `docs/how-to/glossary.md`: `domestic cup`, `qualification_rules`, `golden
  goal`, `cup campaign`, "late entry" rows.
- `docs/how-to/setup-and-launch.md`: a short cup step after the league season
  step.
- `docs/product_manager.md`: record the cup contract that supersedes the
  schema-only hooks (qualification staging + golden-goal resolution + the
  capped-membership rule).

## Recorded decisions

- Eligibility = country league members with a **top-N first-tier late entry**:
  the top N of tier 1 skip the early rounds and enter exactly when X
  bottom-tier survivors remain; N and X are admin-configurable (`422` on
  invalid staging).
- Ties are resolved by **golden goal** (deterministic per match seed); there
  is no penalty shootout and no extra-time period beyond the sudden-death
  minutes. Regulation draws outside knockout formats are unaffected.
- Ties are **single-legged** by default; two-legged aggregate cup rounds and
  live ET streaming are recorded for later tasks, not built here.
- Cup brackets are generated deterministically from `world_seed ⊕ cup_id ⊕
  round` over canonically sorted advancing sets — replay-stable like league
  seeding.
- A cup campaign is one `competition.seasons` row; when the final completes
  the season closes (`completed`). Re-creating a fresh campaign each league
  season (cup reset on league rollover) is recorded for a later task.
- Knockout results never write `competition.standings` and never trigger the
  country promotion/relegation rollover.
- The 3-cups-per-club cap (migration 0037) is enforced now that cup
  memberships are actually written.
- Manager-created competitions (eligibility evaluation, entry fees, invites,
  approval, reputation growth) remain S10-02; this task supplies the bracket +
  entry machinery and the `role='cup'` surface it builds on.