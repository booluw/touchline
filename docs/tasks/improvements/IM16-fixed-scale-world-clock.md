# IM16 — Fixed-scale world clock and real kickoff times

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (match engine / world clock)
**Source:** Live-bug investigation (S04 worlds with fixtures "won't simulate at
their kickoff time")
**Depends on:** `world.worlds` (`launched_at`, `created_at`, `current_day`,
`current_tick`, `status`), `world.world_config` config keys, `match.fixtures`
(`scheduled_at`), the scheduler (`WORLD_TICK{daily}`), and the matchday runner
(`KickoffDue`/`RunLive`). No in-flight improvement changes any of these read
shapes.

## What to do

Root cause of "clubs won't simulate": match kickoffs were gated on the world's
integer day counter (`scheduled_at::date <= worldDate`), so a fixture scheduled
at a *time* of day only ever became due when a daily fire landed on its date —
and with the day counter advancing at 3/day by default, kickoff *hour* was
never real. Fix it by giving each playable world a **continuous fixed-scale
clock** (`worldNow`) that kickoffs compare against as a timestamp, and make the
daily emission roll the day counter toward that clock's day target.

## Delivery evidence

- `backend/internal/world/clock.go` — the clock primitive:
  - `LoadScale(ctx, q, worldID)` returns `(playable bool, epoch time.Time,
    dayLength int64, err)`: playability from status (`active`/`open_beta`),
    epoch = UTC midnight of `COALESCE(launched_at, created_at)`, dayLength from
    the `tick.day_length` config key (default `DefaultDayLength = 86400`).
  - `ScaleNow(real time.Time, epoch time.Time, dayLength int64) time.Time` —
    `epoch + (real − epoch) × 86400 / dayLength`. At the default scale it is
    exactly real UTC time.
  - `TargetDay(t, epoch, dayLength) int64` — `floor` of elapsed game-days, the
    counter's rollover target.
- `backend/internal/world/service.go` — `defaultConfigKeys` now seeds
  `"tick.day_length": 86400` (no migration; ON CONFLICT DO NOTHING at launch
  means existing worlds adopt 1:1 as well).
- `backend/internal/scheduler/service.go` — the daily emission is now a
  **bounded rollover** rather than an unconditional +1:
  - `FireTick(ctx, id, "daily")` loads the world's scale and, when playable,
    rolls `current_day`/`current_tick` one day at a time toward
    `world.TargetDay`, capped at `maxDaysPerFire = 7` game-days per call, one
    `WORLD_TICK{daily}` event (with `world_tick` stamp) per game-day advanced.
  - Legacy granularities (`hourly`, `weekly`, `monthly`, `seasonal`) keep the
    old semantics: monotonic `current_tick`+1, one event, **no day movement**,
    still guarded by playable status inside the same transaction.
  - The scheduler `Run` poll loop now also calls `catchUp`: it scans playable
    worlds each poll (~15s) and rolls each one's counter toward its target, so
    a paused-then-resumed or server-down world catches its missed week up over
    a few polls instead of in one burst. `fireMu` serializes fires per process.
  - A no-op fire (target reached, or paused/archived/missing world) stays a
    safe no-op — stale cron fires can never roll a frozen world (OPD-17(5)).
- `backend/internal/matchday/runner.go` — kickoff gate is now the continuous
  clock:
  - `worldNow(ctx, worldID)` uses `world.LoadScale` + `world.ScaleNow(time.Now(), …)`
    (replaces the integer `worldDate` derivation).
  - `dueMatchdays` compares `scheduled_at <= $2` (timestamptz) — a 20:00
    fixture becomes due at 20:00, not merely on its date.
  - `PlayableWorlds(ctx)` lists playable world ids for the worker's poll.
- `backend/internal/app/app.go` — a worker **intra-day kickoff poll**:
  `kickoffPoll` loops `Runner.PlayableWorlds` on `a.Poll` (default 15s) calling
  the same `kickDueWorld` helper as the daily-tick handler
  (`ActivateDueSeasons` + `KickoffDue` + spawn `RunLive`/reconcile). Both paths
  are idempotent and the OPD-21 no-overlap gate still defers a matchday while a
  match is live, so the poll only adds timeliness, never double-kicks.
  `ActivateDueSeasons` itself stays date-gated on `current_day` (deliberate):
  preserving the IM01 off-season contract means a post-pause season status can
  briefly lag its kickoffs — accepted v1, noted in code comments.
- Tests (all unit `go test ./...` pass; integration compile-gated locally —
  live Postgres required for runtime):
  - `backend/internal/scheduler/service_integration_test.go` — `backdateLaunch`
  helper (backdates `launched_at` so the scale target is deterministic);
  existing daily-semantics tests backdate by 1 day so a single daily fire
  advances exactly one game-day; new
  `TestFireTickRollsOverMissedDays` proves the catch-up contract: 30 missed
  days converge in capped 7-day passes, the final pass lands exactly on the
  target, an idempotent fire is a no-op, and a paused world never rolls.
    `internal/matchday` and `cmd/api` tests that relied on SQL-bumping
  `current_day` were rewritten to scale-driven flow (see below).
  - `backend/internal/matchday/runner_integration_test.go` —
    `runnerWorld` now runs the two-tier world at `tick.day_length=2`
    (one game day per 2 real seconds) with a short off-season; new
    `scaleNow`/`waitForNextDue` helpers wait for the *clock* to mature
    `scheduled_at`. `TestRunnerAdvancesMatchdaysAndRollsOver` (six matchdays
    + OPD-21 overlap sub-test + season-2 rollover under the new gate) and
    `TestRunnerPublishesMatchTickFeed` (feed test waits for the first matchday
    to mature on the clock) both exercise the time gate.
  - `backend/internal/bootstrap/bootstrap_integration_test.go` and
    `backend/cmd/api/phase0_slice_integration_test.go` — backdate `launched_at`
    one day before their `FireTick(daily)` so the first fire still produces a
    tick under rollover semantics.
- Verify (all green): `gofmt -w`, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/... ./cmd/...`, `go test ./...`.
  Touched files are gofmt-clean. (Integration tests cannot execute locally —
  no `TEST_DATABASE_URL`/Docker — so they are compile-gated here and run in CI
  with a live Postgres.)
- Docs: `docs/product_manager.md` OPD-42 (+ OPD-24 supersession note and
  cadence correction), `docs/how-to/cadences-and-time.md` (world-clock model,
  `tick.day_length` key, acceleration now a scale knob), `docs/how-to/seasons.md`
  and `docs/how-to/setup-and-launch.md` (time-gated kickoffs, `tick.day_length`
  acceleration), `docs/how-to/glossary.md` (`worldNow`, `tick.day_length`),
  `backend/internal/apidocs/openapi.yaml` config-key docs.

## Recorded decisions

- **No migration; catch-up on resume.** The alternative — persisting a
  `clock_anchored_at` anchor on pause so resumed worlds "replay the downtime" —
  was rejected: it adds a column and a policy question for no gameplay gain.
  Instead, world time is always derived from real time via
  `epoch + (real − epoch) × 86400 / day_length`; a paused/offline world simply
  resumes from the present and plays its missed fixtures (fast-forward
  catch-up through the bounded rollover). There is no way to "shift worldNow
  by editing launched_at" at the default 1:1 scale — the epoch term cancels —
  which is exactly why integration tests drive kickoffs by compressing
  `tick.day_length` and waiting rather than by backdating launches.
- **The day counter is a rollover target, not a free counter.** `current_day`
  maintains OPD-24's meaning ("in-game days since launch") but the daily
  emission now rolls it **toward** the scale's target in bounded 7-day passes
  instead of insisting on exactly +1. This preserves the pause/resume contract
  (a resumed world's weekly/monthly/seasonal boundaries catch up) while keeping
  fires bounded and idempotent.
- **Kickoffs are the clock's consumer; `ActivateDueSeasons` stays on the day
  counter.** The kickoff gate is a pure timestamp comparison against
  `worldNow`. Season activation keeps the OPD-24/IM01 off-season behavior
  untouched so `TestOffSeasonGapRolloverAndActivation` and the worker-daily
  tests remain valid; the only cost is that a heavily-paused world's season
  status can lag kickoffs by a poll or two, accepted for v1 and commented in
  code.
- **Acceleration is `tick.day_length`, not the cron.** Shortening
  `tick.daily_cadence` cannot make a 20:00 fixture kick off any earlier — only
  the scale factor does (default 86400 = real time; `60` = a game day per
  minute; `1` = per second). The daily cron + 15s poll catch-up rolls the day
  counter at up to ~7 days/poll, ample at default scale but a documented lag
  under heavy compression.