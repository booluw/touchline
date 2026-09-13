# S04-02 — Deliver the deterministic, pure match engine

**Status:** Done
**Sprint:** 04 — Deterministic football competition
**Source:** PRD §§55–57, 63–64, 68, 74; technical plan §§1, 9, 14
**Depends on:** S04-01, S01-03, OPD-21, OPD-22
**Key files:** `backend/pkg/matchsim/{README,team,tuning,splitmix64,simulate,simulate_test}.go`, `backend/internal/match/{live,service,caster}.go`, `backend/internal/matchday/runner.go`, `backend/cmd/worker/main.go`

## What to do

Implement the pure, seeded simulation contract that resolves a match from team inputs and tactics into an ordered result/event list. Integrate it through worker-driven fixture processing, not client execution.

## Acceptance criteria

- Match simulation has no database dependency and accepts a seed, squads, and tactics to return `MatchResult` and ordered `MatchEvent` values.
- Repeating a simulation with identical seed and inputs produces identical output.
- Match seeds and sufficient input/event context are persisted for replay/audit.
- Results update fixtures/standings through server-side event handling.
- Unit and golden replay tests protect deterministic behavior; product-owned tuning formulae are not fabricated without approval.

## Delivery evidence (task delivered)

- **Pure engine (`pkg/matchsim`, AC1+AC2):** `Simulate(Options) MatchResult` with **no DB/network/clock dependency**; inputs are `Seed`, `Team{ID, ClubName, Ability}` and a `Tuning` block (empty ⇒ `DefaultTuning()`). `MatchResult{HomeGoals, AwayGoals, HomePossession, Events}`; events typed to the `match.match_events` CHECK subset (`kickoff, goal, chance_created, yellow_card, red_card, substitution, half_time, full_time`), each with a canonical `Sequence`.
- **Approved model, spec-first:** `pkg/matchsim/README.md` pins the zone/possession + **cumulative-threshold chance table** model, the **canonical draw order** (per-minute possess → chance? → outcome/feed → card → sub, plus half/full-time), and the **SplitMix64** RNG stream — the replay contract. Reserved: identical `(seed, teams, tuning)` ⇒ byte-identical events & score.
- **Determinism (AC2, AC5), verified `-race`:** `TestDeterminism`, `TestDeterministicAcrossCopies`, `TestGoldenReplay` (pinned SHA-256 of the full output for `seed 424242` under v1.0-proposed — drifts on any score/possession/event/description change), plus `TestShape`, `TestEventsSumToScore`, `TestAbilityDominance` (home/away swap preserves totals; strong side dominates possession), `TestPerformance` (<1ms/match), `TestPossessionShare`, `TestClampAbility`.
- **Tuning governance (AC5):** tuning numbers live in the versioned `Tuning` block (`EngineVersion = "1.0-proposed"`), consumed as configuration and recorded as `match.matches.engine_version`; the **numbers are a proposal pending PM sign-off** (OPD-03), replaceable by data change, and documented in the README §5–§6. `LivePacingSecondsPerMinute = 20s` default documented (OPD-21).
- **Orchestration (AC3+AC4) — deterministic live execution (OPD-21):** worker-integrated, not client-driven. `KickoffMatchday` freezes the full simulation input (`sim_inputs`: teams, XIs, benches, keepers, form states, world tick, fixture context; `pacing_millis` = resolved `tick.match_cadence`, `current_minute` = the authoritative match clock) and marks the fixture live (idempotent, `FOR UPDATE`). `PaceMinute` re-runs the engine with inputs ≤ m and persists **one minute per tx** (only the new-sequence tail; fresh casters link players; the loop sleeps the cadence). `Finalize` completes match + fixture, updates both clubs' rolling form from the snapshot, and records/publishes `MATCH_PLAYED`. `competition.ApplyResult` (via the matchday runner) updates fixtures + `standings`. Crash-safe: `LoadLiveSessions` rehydrates from snapshot + events + inputs (`nextMinute = current_minute+1`); `reconcileApplied` idempotently closes the Finalize→ApplyResult window; a start-up sweep resumes worlds with live matches.
- **Live-control inputs (AC "live-control API"):** `Service.Substitute`/`TacticChange` record ordered inputs into `match.match_inputs` (`SubWindows = [60, 75]`, exact-squad validation, per-window duplicate rejection, past-minute rejection; each substitution consumes zero RNG so a full machine replay reproduces the manager's exact feed). Full-match integration suite: paced 90-minute stream is **structurally byte-equal to an instant `Simulate` over the same seed/teams** (`assertReplaysExpected`), substitution replay equals `Simulate` + manager input and links the chosen players, mid-match rehydration resumes at the exact minute, kickoff redelivery is a no-op, and validation paths return the sentinel errors.
- **Runner (worker-driven):** `internal/matchday.Runner.KickoffDue` kicks due matchdays behind a **no-overlap gate** (any live match ⇒ skip, never double-kick) and `RunLive` paces all in-progress matches to full time, finalizes, and applies standings (`run E2E: 4 fixtures × 6 matchdays to season rollover + 28 matches + matches↔fixtures score mirror + `MATCH_PLAYED` count + season-1→2 rollover with promotion/relegation events`). `cmd/worker` holds a `touchline:match_runner` advisory lock, kicks + launches `RunLive` on daily ticks and rehydrates live worlds at startup.
- **Schema (AC3):** migration `0031` adds `match.matches.sim_inputs` (JSONB), `pacing_millis`, and `current_minute` (the match clock — event-less minutes make `MAX(minute)` invalid); `match.match_inputs` (ordered manager input stream) added by `0030`.
- **Full-suite verification:** `go test -p 1 -tags integration -count=1 ./...` green across every package (live-match, matchday-runner, competition rollover, worker, plus the existing Phase-0–6 suites), `go build ./...` and `go vet ./...` clean.
- **Remaining (not this task):** the S04-03 observer feed over the S02-04 realtime socket; the HTTP live-control surface (service methods exist); S05-01 tactical modulation (the engine consumes `tactic_change` inputs for replay but applies no numeric effect).
