# S04-02 — Deliver the deterministic, pure match engine

**Status:** In progress — pure engine delivered (orchestration pending)
**Sprint:** 04 — Deterministic football competition  
**Source:** PRD §§55–57, 63–64, 68, 74; technical plan §§1, 9, 14  
**Depends on:** S04-01, S01-03, OPD-21, OPD-22
**Key files:** `backend/pkg/matchsim/{README,team,tuning,splitmix64,simulate}.go`, `backend/pkg/matchsim/simulate_test.go`

## What to do

Implement the pure, seeded simulation contract that resolves a match from team inputs and tactics into an ordered result/event list. Integrate it through worker-driven fixture processing, not client execution.

## Acceptance criteria

- Match simulation has no database dependency and accepts a seed, squads, and tactics to return `MatchResult` and ordered `MatchEvent` values.
- Repeating a simulation with identical seed and inputs produces identical output.
- Match seeds and sufficient input/event context are persisted for replay/audit.
- Results update fixtures/standings through server-side event handling.
- Unit and golden replay tests protect deterministic behavior; product-owned tuning formulae are not fabricated without approval.

## Delivery evidence (engine delivered)

- **Pure engine (`pkg/matchsim`, AC1+AC2):** `Simulate(Options) MatchResult` with **no DB/network/clock dependency**; inputs are `Seed`, `Team{ID, ClubName, Ability}` and a `Tuning` block (empty ⇒ `DefaultTuning()`). `MatchResult{HomeGoals, AwayGoals, HomePossession, Events}`; events typed to the `match.match_events` CHECK subset (`kickoff, goal, chance_created, yellow_card, red_card, substitution, half_time, full_time`), each with a canonical `Sequence`.
- **Approved model, spec-first:** `pkg/matchsim/README.md` pins the zone/possession + **cumulative-threshold chance table** model, the **canonical draw order** (per-minute possess → chance? → outcome/feed → card → sub, plus half/full-time), and the **SplitMix64** RNG stream — the replay contract. Reserved: identical `(seed, teams, tuning)` ⇒ byte-identical events & score.
- **Determinism (AC2, AC5), verified `-race`:** `TestDeterminism`, `TestDeterministicAcrossCopies`, `TestGoldenReplay` (pinned SHA-256 of the full output for `seed 424242` under v1.0-proposed — drifts on any score/possession/event/description change), plus `TestShape`, `TestEventsSumToScore`, `TestAbilityDominance` (home/away swap preserves totals; strong side dominates possession), `TestPerformance` (<1ms/match), `TestPossessionShare`, `TestClampAbility`.
- **Tuning governance (AC5):** tuning numbers live in the versioned `Tuning` block (`EngineVersion = "1.0-proposed"`), consumed as configuration and recorded as `match.matches.engine_version`; the **numbers are a proposal pending PM sign-off** (OPD-03), replaceable by data change, and documented in the README §5–§6. `LivePacingSecondsPerMinute = 20s` default documented (OPD-21).
- **Remaining (orchestration):** derive team `Ability` from squad attributes (generating base `player_attributes`), persist `(seed, engine_version)` + ordered `match_events`, run live goroutines at the configured pacing, compute results through `competition.ApplyResult`, live-control API (substitution/tactic), and the S04-03 feed over the realtime socket.
