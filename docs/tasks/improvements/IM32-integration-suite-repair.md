# IM32 — Integration suite repair (pre-existing failures)

**Status:** Not started
**Owner:** unassigned
**Sprint:** Improvements (integration suite health)
**Source:** The first full run of every `//go:build integration` suite against a
real Postgres 16 (2026-09-29, recorded in
[IM29 § Verification](IM29-single-backend-image-deploy.md#verification)):
545 pass / 69 fail after IM23–IM30. **Every one of the 69 also fails on the
code before IM23–IM30** — they are old breakages that compile-only checks never
caught. Several are real product bugs (a club-less manager cannot log in; the
home login flow 500s), others are stale test fixtures.
**Depends on:** a reachable Postgres (`TEST_DATABASE_URL`). A local
`postgresql-16` cluster works:
`initdb -D <dir> -A trust -U postgres && pg_ctl -D <dir> -o '-p 5433' start`,
then `TEST_DATABASE_URL=postgres://postgres@127.0.0.1:5433/<db>?sslmode=disable`.
Run packages with `-p 1` (each test truncates the shared database).

## How to work each failure

For every failing test, decide **"is the code wrong or the test wrong?"**
against the docs (`docs/how-to/*.md`, `docs/product_manager.md`, the task
file). Fix the code when it breaks documented behaviour; fix the test when its
fixture or expectation is stale. Never delete or skip a test to get green; if a
test encodes a rule the docs don't settle, record it as an open decision in
`docs/product_manager.md`. Log each fix under **Delivery evidence** below.

## Work list (in priority order)

### P0 — blocks play and most of the HTTP suite

1. **A club-less manager cannot log in.** `auth.Service.managerClub`
   (`internal/auth/auth.go` ~L231) scans `c.name` / `c.short_name` from a
   `LEFT JOIN` into plain `string`s; for a manager with no club they are NULL →
   `cannot scan NULL into *string` → login 500. Scan into `*string` (or
   `COALESCE`). This single bug causes most HTTP failures:
   `TestLogin_ManagerSingleWorld`, `TestLogin_ExplicitWorldID`,
   `TestRefresh_ManagerSession`, `TestRegister_SinglePlayableWorld`
   (`internal/auth`) and the "login = 500" group in `internal/httpapi`
   (`TestHTTPAdminWorldLifecycle`, `TestHTTPLogin_WorldPickerAndExplicitPick`,
   `TestHTTPLogout_RevokesAndClearsCookies`, `TestHTTPCompetitionAdminAndReads`,
   `TestHTTPSeasonCalendarAndClubFixtures`, `TestHTTPClubNameParts`,
   `TestHTTPJobOfferFlow`, …). Re-run the whole HTTP suite after this fix — many
   of the remaining HTTP failures may disappear. Closes most of **A12**.
2. **`TestHTTPInjuryReadAndRushReturn` hangs** until the package timeout (it
   first gets a 500 on `GET …/injury`). IM30 fixed the injury context query, but
   the HTTP path still stalls after its injury read returns. Find the 500's
   cause and the leaked connection/transaction (`internal/player/injury.go`,
   `internal/injury/store.go` read path; check every `rows.Close()` /
   `tx.Rollback`). Until fixed, CI loses the whole `internal/httpapi` package
   to the timeout.
3. **A13 auto-offer returns no club** — `TestHTTPRegister_OnboardsWithAutoOffer`
   (offer club is the nil UUID). Check `manager.OnboardingAIClubID` /
   `CreateJobOffer` against the seeded AI club. Closes **A13**.

### P1 — real product bugs

4. **Player transfer requests never raised / not found** —
   `TestWeeklyTickRaisesTransferRequestForDeepShortfall`,
   `TestDenyPlayerRequestDropsMoraleAndSetsCooldown`,
   `TestApprovePlayerRequestListsPlayer`, and promise grading
   `TestPromisePlayingTimeGradedWeekly` (`internal/player`). The weekly player
   pass ran with errors before IM30; re-check S06-03 behaviour end to end.
5. **Board mandates not generated** — `TestBoardMandatesGeneratedOnFirstView`
   (0, want 4), `TestNegotiateMandateLifecycle`, `TestHTTPBoardRoundTrip`
   (snapshot total 0 ≠ confidence) (`internal/board`, S06-02).
6. **Message sanitization keeps script bodies** —
   `TestSendMessageSanitizationAndLength`: `"alert(1)Hello!"`, want `"Hello!"`.
   Tag stripping leaves `<script>` contents (`internal/social`, S06-04b).
   Security-relevant.
7. **API reference names come back empty** — match feed club names / goal
   `club_id` (`TestMatchFeedFixtureHeader`, `TestMatchFeedEventsEndpoint`),
   rivalry peer names (`TestRivalEdgesResolveFromEitherOrientation`,
   `TestPublishRelationshipChangeRealtime`), job-offer club name
   (`TestJobOfferFullLifecycle`), manager h2h wins (`TestHTTPManagerProfile`).
   Likely the same class as IM30 (joins/columns that moved).
8. **PolicyBot** — `TestAttendOrMissAutoAway`, `TestEnsureTrainingNoPlanGetsArchetype`,
   `TestRespondToBidsForAbsent`, `TestEnsureMatchInputsLineupWritten`
   (`internal/policybot`, S06-05). Some are stale fixtures (fixture insert
   missing `competition_id`), check the rest.
9. **Squad dynamics / lineup HTTP** — `TestHTTPGetSquadDynamics*` (500),
   `TestHTTPLineupRoundTrip` (fresh lineup has 0 slots).
10. **`playerpool` bulk create panics** — `TestBulkCreateIntegration`:
    `assignment to entry in nil map` (A10).

### P2 — competition scheduling / cups (code vs test drift since IM22)

11. Scheduling expectations: `TestFixturePacing`,
    `TestOffSeasonGapRolloverAndActivation`, `TestWeekdayPacingAndRePace`,
    `TestCupCalendarAnchoredToLeagueEnd`, `TestSetCupFinalDateOverrideAndClear`,
    `TestStaggeredOptOutKeepsSingleDay` (opt-out ignores allowed weekdays),
    `TestRunnerAdvancesMatchdaysAndRollsOver`, `TestRunnerCapOnlyForStaggered`
    (`internal/matchday`). Decide per `docs/how-to/seasons.md` and
    `cup-competitions.md` whether IM22's staggered defaults made the tests stale
    or broke behaviour.
12. Regional cups / qualification: `TestRegionalCupIntegration*`
    ("region not found", "already has a live campaign"),
    `TestQualifyIntegrationConflicts`, `TestStakesSixPointerBands`.
13. Membership: `TestMembershipCapacityAutoAdjustsNeighboursAndAutoFill`,
    `TestMembershipRefusals` (IM14).
14. Admin/detail: `TestCompetitionDetailLeague` (duplicate event sequence in
    the fixture), `TestCompetitionDetailCup`,
    `TestHTTPAdminCompetitionDetail` (test inserts `final_date_mode` into
    `competition_rules`; migration 0054 put it on `competitions`),
    `TestHTTPStartSeasonEndpoint` (unseeded start returns 201, want 409).

### P3 — stale fixtures and determinism

15. Stale fixtures: `TestPlayerDetailCarriesAbilityAndCareer` (column `key` of
    `player_attributes`), `TestRecordMatchAppearancesInsideTx` (fixtures check
    constraint), `TestPersistMatchRoundTrip` (`fk_injuries_match`),
    `TestReviewSacksUnderperformingHumanManager` (entry FK),
    `TestAutoFillRestoresThinSquad` (odd `team_count`),
    `TestDevelopmentWeeklyFlexExpansion`, `TestNextFixtureScout`,
    `TestHTTPPlayerDevelopmentDetail`, `TestHTTPTransferMarketRoundTrip`,
    `TestHTTPClubReads` (seed = 500).
16. Determinism: `TestBootstrapWorldDeterministic` (same seed, different
    players), `TestApplyWeeklyMatchesDeterministicModel`, and the flaky
    `TestRecoveryDetrainsAfterThreeConsecutiveWeeks` (fails ~5 of 6 runs even on
    the old code — an ordering or randomness leak).

### P4 — keep it green

17. **Run every integration package in CI.** `.github/workflows/ci.yml` lists
    only a subset of packages, which is how seeding, dashboard and transfer
    reads broke unnoticed. Switch the integration job to `./...` with
    `-p 1 -tags integration` and a timeout. Do this once P0 is fixed so the job
    can finish.

## Acceptance criteria

- Every integration package passes against Postgres (or a remaining failure is
  an explicitly recorded open product decision).
- No test is skipped, deleted or quarantined to get there.
- CI runs the full integration suite.
- A12 and A13 are set to Implemented with evidence.

## Delivery evidence

- (to be filled per fixed item)

## Recorded decisions

- Password / registration policy (OPD-54, under OPD-02) is **deferred** by the
  product owner (2026-09-29); do not add password rules while repairing A13.
