# IM18 — Live match pacing: guaranteed termination and self-healing resume

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (live match reliability)
**Source:** Product report — a live match ran for a full 90 real minutes and
never ended. Two separate defects could produce it: a match that outlives its
pacing loop because nothing re-entered it after a transient failure, and a
golden-goal cup tie whose sudden-death block the engine resolves only once the
tie is decided, which can be hundreds of minutes long
**Depends on:** the S04-02 live pacing contract (OPD-21), the per-minute durable
pacing step (`Service.PaceMinute`), the resumable `LiveSession`
(`LoadLiveSessions`), and the match engine's sudden-death block
(`matchsim.GoldenGoalMaxMinute`). No schema, config key, or route changes.

## What to do

1. Make the live pacing loop **self-healing**: the kickoff poll must re-enter
   `RunLive` for every world on every pass, not only right after a kickoff, and a
   transient per-step failure must be retried in place instead of killing the
   loop.
2. Make every match **terminate on schedule**: a golden-goal tie must cost one
   extra step past full time, not the engine's whole sudden-death block, and the
   engine's extra-time loop must be bounded so no tie can pace forever.
3. Make the real-time cost **observable**: log kickoff and full time with the
   match's frozen pacing and elapsed live time, so "my match took 90 minutes"
   is diagnosable from the worker log and the match row.

## Delivery evidence

- `backend/internal/app/app.go` — the intra-day kickoff poll starts the live
  pacing goroutine on **every** pass, moved out of the `sum.Kicked > 0` branch.
  That branch was the stall: a `RunLive` that died mid-match (transient error,
  worker restart) was never re-entered, and `KickoffDue`'s no-overlap gate (OPD-21)
  then refused to start any later matchday, so the fixture sat at its last
  persisted minute forever while the world's matchday ladder was held open. With
  an unconditional entry, a fresh `RunLive` adopts the stalled sessions and drives
  them to full time; `RunLive` claims the world first, so the extra passes are a
  no-op when a loop is already running.
- `backend/internal/matchday/runner.go`:
  - `RunLive` wraps every error with its world, match and minute (a failure in a
    goroutine was previously an opaque `log.Fatal`-less drop), and paces and
    finalizes through `paceWithRetry` / `finalizeWithRetry`.
  - `liveRetryBackoff = {0, 2s, 10s}` — one flaky write (dropped connection,
    restarted database) no longer strands a match. Both steps are safe to retry:
    `PaceMinute` rolls back whole minutes, `Finalize` is idempotent.
  - `logResumed` reports each adopted match's persisted minute, frozen pacing and
    elapsed live time **once per entry** (not once per minute — a resumed match
    can run for hundreds of steps). A match live far longer than `pacing ×
    minute` is the signature of a loop that was not running.
- `backend/internal/match/live.go`:
  - `liveMinuteBound(goldenGoal)` is `90`, or `91` for a golden-goal tie:
    regulation ends at 90 and a level cup tie takes exactly **one** extra step.
    `PaceMinute` short-circuits to finished past the bound, so a session can never
    sit at full time indefinitely.
  - The golden-goal flush step (`m == 91`) persists the engine's **whole**
    extra-time block — every extra minute plus the deciding goal — in one
    transaction, so the tie ends on time and the deciding goal is still in the
    feed. `liveFinished` reports full time on that step; the running-scoreline
    tally that would have streamed minute by minute is gone.
  - `LiveSession` carries `StartedAt` (from the match row, so it survives a
    restart) with `LiveFor()`; kickoff and full time are logged with the frozen
    pacing and the elapsed real time. The duplicated `matchWallClock` is removed
    in favour of the session value.
  - `Finalize`'s full-time guard reads the shared `matchsim.RegulationMinutes`.
- `backend/pkg/matchsim/simulate.go` — `RegulationMinutes = 90` and
  `GoldenGoalMaxMinute = 1000` are exported constants, and the sudden-death loop
  is bounded: the extra-time loop's exit condition is a goal, and with the shipped
  scoring rates a level tie needs hundreds of extra minutes to find one (measured:
  the worst of 10,000 sampled seeds decides at minute 613; 84 of the first 200
  seeds were level at 90). A tie still level at the bound finishes level with an
  explicit `Still level …` full-time event.
- Tests:
  - `backend/pkg/matchsim/simulate_test.go` — `TestGoldenGoalExtraTimeIsBounded`
    pins the termination guarantee: with `ChancesPerMatchMin = 0` (no goal is
    possible) `Simulate` still returns, no event is written past the bound, and
    exactly one still-level full-time summary lands on the bound minute. This is
    the test that caught the first attempt, which bounded the tie at 120 and
    failed `TestGoldenGoalDecidesLevelTie` / `TestGoldenGoalScansManySeeds` on a
    seed needing minute 436.
  - `backend/internal/match/live_finish_test.go` (new, unit) —
    `TestLiveFinished` walks the whole minute ladder of a regulation match and a
    golden-goal tie: `liveFinished` is false for every minute before the last
    step the match is allowed to take, true exactly on it, and a step past the
    bound reports finished without persisting (which is what lets `Finalize` run).
  - `backend/internal/match/live_integration_test.go` —
    `TestLiveGoldenGoalTieEndsOneStepPastFullTime` (new) arms
    `competition.competition_rules.format = 'knockout'`, pins a seed that is level
    at 90 (probed on the pure engine: `levelSeedFor` simulates **regulation
    only**, because `Options.GoldenGoal` is read only *after* the 90-minute
    ladder, so a level regulation-only result is exactly the level-at-90 state —
    a golden-goal `Simulate` can never report a tie, since sudden death always
    ends it. The pinned seed is also written to `matches.seed`, because that is
    what a crashed worker rehydrates from; leaving the row on the kickoff seed
    would make a rehydrated match replay a different tie than the one paced here),
    paces all 90 regulation minutes (none reported finished), then asserts the
    single step past 90 returns the deciding events, reports full time, leaves
    nothing paceable, replays the engine's whole output, and finalizes — with the
    stamped scoreline equal to the engine's and, necessarily, not a draw.
  - `backend/internal/matchday/runner_integration_test.go` —
    `TestRunnerResumesStalledLiveMatch` (new) reproduces the reported bug at the
    runner level: kick off, pace 30 minutes, walk away (a dead loop), then let a
    fresh `RunLive` adopt the stalled match. It must return, and every match the
    world kicked must be `completed` at minute 90 with the configured
    `pacing_millis`. The feed assertions are: exactly one `kickoff` (1'), one
    `half_time` (45') and one `full_time` (90') per match, no event outside 1..90,
    a gapless 1..N run of sequences — and, as the real double-pacing canary, **the
    goals in the feed must add up to the recorded scoreline**. `Finalize` re-runs
    the engine once over the same seed and inputs, so that identity only holds if
    the paced feed holds exactly one event per engine event; a resumed loop that
    re-persisted a minute would write its events a second time under *fresh*
    sequence numbers (the `(match_id, sequence)` unique index would never fire)
    and the feed's goal tally would outrun the score. Note what is deliberately
    *not* asserted: `COUNT(DISTINCT minute) = 90` and "no minute holds two events"
    were both in the first draft of this test and both are simply false of a
    healthy match — most simulated minutes produce no event at all, and several
    events in one minute is ordinary football. The second `RunLive` must be a
    cheap no-op.
  - `backend/internal/matchday/runner.go` — `publishCompleted` sends
    `sess.NextMinute() - 1` as the envelope minute instead of a hardcoded `90`.
    For a league match that is still exactly 90; for a golden-goal tie it is 91,
    which is the minute the deciding goal actually arrived on. The hardcode would
    walk the clock backwards for precisely the matches whose last event is the one
    that mattered.
- Verify: `gofmt -w` on the touched files, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/... ./cmd/...`, `go test ./...`
  all green. (Integration tests cannot execute locally — no `TEST_DATABASE_URL`
  and no usable local Postgres or Docker daemon on this arm64 host, every
  `postgres` binary present is x86_64 — so they are compile-gated here and run in
  CI against a live Postgres.)
- Docs: `docs/how-to/cadences-and-time.md` §7 (live match duration, the
  self-healing poll, and how to diagnose a long match), `docs/product_manager.md`
  OPD-44, `docs/how-to/cup-competitions.md` (golden-goal ties cost one step).

## Recorded decisions

- **Self-healing beats one-shot delivery.** The poll enters `RunLive` for every
  world on every pass rather than only after a kickoff. The claim
  (`Runner.claim`) already makes concurrent and repeat entries safe, so the cost
  of unconditional entry is one `LoadLiveSessions` on a quiet world — the price of
  never again stranding a live match.
- **Retry in place, then give the world back.** Three attempts with a 12-second
  budget (`0`, `2s`, `10s`) cover a dropped connection or a database restart
  without turning a long outage into a busy loop; after that the loop returns the
  error and the next poll re-enters and resumes. Retries are only sound because
  the pacing step is one transaction per simulated minute and `Finalize` is
  idempotent.
- **A golden-goal tie costs one step, not its true length.** The engine's
  sudden-death block is resolved in one go at minute 91 instead of streamed, and
  the deciding goal therefore arrives in the completion tick rather than
  minute-by-minute. This is a deliberate product trade: an extra-time block that
  can run to several hundred simulated minutes (worst measured 613) would hold a
  world matchday ladder open for hours, and the deciding goal is still shown, with
  its true extra-time minute, on the final tick. Streaming it is a pure feed
  change if the pacing is ever revisited.
- **The engine's bound is a safety net, not a rule.** `GoldenGoalMaxMinute = 1000`
  exists so a tie that cannot be broken (a no-chance tuning, a pathological seed)
  still returns instead of looping forever; no seeded match in the suite reaches
  it, and reaching it finishes the tie level with an explicit summary rather than
  inventing a winner. The honest alternative — a product-level extra-time cap of
  30 minutes that decides by penalty shoot-out — is a competition-layer decision
  and is **not** taken here: cup progression today has no shoot-out or aggregate
  rule to carry a drawn tie.
- **Pacing stays a frozen, per-match value.** `tick.match_cadence` is read once at
  kickoff into `match.matches.pacing_millis` (pre-existing contract): a match
  cannot change pace halfway because an admin edited the config, and a long match
  can be diagnosed from that column alone.
- **No config key, migration, or route.** This is a reliability fix inside the
  existing S04-02 contract; the only user-visible change is that matches now
  always finish, and that they log how long they took.
