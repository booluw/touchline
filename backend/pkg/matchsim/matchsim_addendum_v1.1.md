# pkg/matchsim — the pure deterministic match engine

Status: **proposed engine spec, v1.0-proposed** · Sprint: S04-02 · Owner: engine + PM (tuning sign-off)

## 1. Purpose

`pkg/matchsim` is the **pure, seeded simulation contract** that resolves a match
from team inputs into an ordered `MatchEvent` list and final score. It has **no
database, network, or wall-clock dependency** — repeat it with the same inputs
and you get the identical result. Persistence, live pacing, and result
application are orchestration concerns (`internal/match`, S04-02), never this
package.

## 2. Approval-gated model (recorded, product-owned)

The simulation structure below is the **approved product model** (user-picked in
Sprint 04 planning):

- **Zone/possession model.** Ability-weighted possession decides who attacks
  each minute; a cumulative-threshold chance table resolves what that attack
  creates.
- **Cumulative-threshold draws.** Every stochastic outcome is a walk over a
  `[0,1)` draw against a tuned, ordered threshold table.
- **Determinism = `(seed + team inputs + tuning) → identical events & score`.**
  The draw order is canonical (Section 4).
- **Live pacing** default **20 real seconds per simulated minute** (~30 real
  minutes per match), world-config key `tick.match_cadence` (OPD-21/OPD-17(2)).

The **numbers** in `Tuning` (Section 5) are a **proposal awaiting PM sign-off**
(OPD-03 core, S04-02 AC5). They are consumed as configuration in versioned
blocks, never hardcoded at call sites; a sign-off is a data change, not a code
change. `engine_version` on a completed `match.matches` row records exactly
which tuning block produced it.

## 3. Inputs & outputs

```
Simulate(opts Options) MatchResult
```

`Options`:

| Field | Meaning |
|---|---|
| `Seed` | int64 seed; identical seeds ⇒ identical match |
| `Home`, `Away` | `Team{ID, ClubName, Ability}` — ability is [1,100], computed by the orchestration layer from squad attributes (S04-02 generates the base `player_attributes` this derives from) |
| `Tuning` | versioned tuning block (Section 5); **empty ⇒ the current `DefaultTuning()` proposal** |

`MatchResult`:

| Field | Meaning |
|---|---|
| `HomeGoals`, `AwayGoals` | final score |
| `HomePossession` | home minutes-with-ball share in percent |
| `Events` | ordered feed events, one `MatchEvent` per entry |

`MatchEvent`:

| Field | Meaning |
|---|---|
| `Sequence` | 1-based canonical order (BOTH for the event log and the draw log — see Section 4) |
| `Minute` | 1..90 (45 = half-time, 90 = full-time) |
| `Type` | `kickoff, goal, chance_created, yellow_card, red_card, substitution, half_time, full_time` |
| `ClubID` | owning/attacking club id (empty for kickoff/half/full) |
| `Description` | deterministic feed text; `{player}` is a token the orchestration layer replaces with a real squad member name — no name data lives in this package |

## 4. Canonical draw order (determinism)

All randomness flows through one **SplitMix64** sequence, advanced in exactly
this order **for each minute `m` in 1..90** — this order is the replay contract
and must never change without bumping `Tuning.Version`/`engine_version`:

1. `possess` — possession draw for minute `m`; decides attacker.
2. `chance?` — does the attacker create a chance this minute?
   - If yes: `outcome` — resolve via the chance table (ability-scaled), then
     `feed?` — decide whether this chance becomes a feed event.
3. `card` — card check for minute `m` (opponent of the attacker on a foul
   outcome also counts; see `ChanceOutcome` details).
4. `sub` — substitution check at the canonical windows `[60..64]`, `[75..79]`
   (draw once per window, per team).

Events are **appended in outcome order**, and every emitted event bumps a shared
`Sequence` counter. Persisting `(match.seed, engine_version)` plus the ordered
`match_events` sequence reproduces the feed and score exactly (AC: replay/audit).

**Scoring:** a goal outcome increments the attacker's net. Half/full time are
synthesized at minutes 45 and 90 after the minute's draws.

## 5. Tuning (proposal v1.0-proposed — PM sign-off pending)

| Key | Value | Meaning |
|---|---|---|
| `PossessionExponent` | `3.0` | `share = a^e / (a^e + b^e)`; e=0 ⇒ 50/50 |
| `ChancesPerMatchMin` | `13` | baseline attacking chances per team per 90 min vs equal (75) opponent |
| `ChanceOutcome` | goal `0.10` · on-target `0.30` (cum) · off-target `0.75` · blocked `0.95` · foul `1.00` | cumulative-threshold table; goal bucket scales with ability (Section 6) |
| `ShotFeedFraction` | `0.5` | fraction of on-target shots that become `chance_created` feed events |
| `YellowPerMatchMin` | `2.6` | baseline yellows per match total |
| `RedPerMatchMin` | `0.10` | baseline reds per match total |
| `GoalAbilityScale` | `0.6` | goal odds multiplier `(attack/defense)^scale`, clamped `[0.2, 3.0]` |
| `LivePacingSecondsPerMinute` | `20s` | real-time pacing for live goroutines (default; world-config override) |

## 6. Ability scaling (deterministic)

- **Possession:** `ph = h^e/(h^e+a^e)` (attacker = home iff `possess < ph`).
- **Goal bucket:** the goal threshold `g0 = 0.10` is multiplied by
  `min(max((attAbility/defAbility)^scale, 0.2), 3.0)`, then re-normalised so the
  table stays monotonic: the goal bucket is `min(g0·m, 0.85)` and on-target
  upper bound `= max(goal, 0.30·… )` recomputed against the scaled goal bound.
  A scaled table walk resolves the outcome.

## 7. Invariants & tests

- Same `(seed, teams, tuning)` ⇒ byte-identical `Events`, scores, possession.
- Full 90-minute simulation is memory-safe and alloc-free-per-minute stable;
  a single match runs < 1ms (unit).
- Golden replay: a pinned seed's event stream and score are frozen into the
  test suite; any drift (even a description string) fails the build.
- Boundaries: minute is clamped to [1,90]; team counts 2; net never negative.

## 8. Contract hand-offs

- **S04-02 orchestration (`internal/match`):** derives `Ability` from squad
  `player_attributes` (generating base attributes where none exist), persists
  `(seed, engine_version)` + ordered `match_events`, live-steps in wall clock per
  `LivePacingSecondsPerMinute`, and computes the final result through
  `competition.ApplyResult`.
- **S04-03 feed:** reads `match_events` ordered by `sequence`; live pushes use
  the S02-04 realtime socket (`match_tick`).
- **Replay/audit (S11):** `(seed + ordered live inputs from match.match_inputs)`.

---

## 9. Product Manager Audit & Data-Backed Realism Review

> **PM Mandate:** Gameplay must be realistic and backed by real-world football data (Opta / FBref / Transfermarkt benchmarks across top European leagues). Below are PM concerns and required tuning adjustments prior to final v1.0 sign-off (OPD-03).

### Concern 1: Missing Home Advantage Modifier (Critical Realism Gap)
- **Data Benchmark:** Across 50,000+ top-flight matches in professional football, home teams win ~45-48%, draw ~26%, and away teams win ~26-29%. Home teams average a net goal advantage of +0.30 to +0.40 goals (~1.45 home goals vs ~1.10 away goals per match).
- **Engine Issue:** The current formula `ph = h^e / (h^e + a^e)` treats two equal teams (75 vs 75) as 50/50, producing equal expected goals (1.30 vs 1.30).
- **PM Requirement:** Introduce `HomeAdvantageFactor` (default `1.08` multiplier on home effective ability or +0.08 addition to home possession probability) into `Tuning` and Section 6 formula.

### Concern 2: Card Attribution Uncoupled from Foul Events
- **Data Benchmark:** Yellow cards (~3.5 to 4.5 per match in top leagues) occur predominantly during defensive tackles, tactical fouls on counters, or high-pressing turnover attempts.
- **Engine Issue:** Draw 3 checks `r.nextFloat() < YellowPerMatchMin/90` every minute independently of fouls. Cards drop out of thin air on random minutes without a preceding foul event.
- **PM Requirement:** Couple yellow/red card draws to foul outcomes (`outcomeFoul`) or defensive pressing events, and scale card probability by team aggression/tackling attributes and match derby/rivalry intensity.

### Concern 3: Tactical Intent vs Raw Possession Exponent
- **Data Benchmark:** Counter-attacking teams (e.g. Real Madrid vs Man City, or 2016 Leicester City) intentionally cede possession (35-42% possession) while maintaining equal or superior expected goals (xG).
- **Engine Issue:** `PossessionExponent = 3.0` derives possession purely from raw team ability. A higher ability team will always dominate possession regardless of tactical setup.
- **PM Requirement:** Ensure the orchestration layer (`internal/match` / S05-01) modulates effective possession weights using team tactical mentality (Possession vs Counter-Attack vs Low-Block) before passing abilities into `matchsim`.

### Concern 4: Late-Game Urgency & Goal Timing Histogram
- **Data Benchmark:** Opta match timing data shows ~23-25% of all goals occur in the final 15 minutes (75'-90'+) due to defender fatigue and trailing teams pushing players forward.
- **Engine Issue:** Goal generation probability is completely flat across minutes 1..90.
- **PM Requirement:** Incorporate fatigue decay and late-game urgency multipliers (scaling chance frequency or goal conversion in minutes 75-90 for trailing teams) to mirror real-world goal histograms.

### Concern 5: Data Validation of Core Chance & Conversion Metrics (Approved Baseline)
- **Data Benchmark Check:**
  - **Expected Total Goals:** Engine yields ~2.60 goals per match for equal teams (13 chances * 10% goal weight * 2 teams = 2.6). This matches top-flight Opta league averages (~2.65 - 2.80 goals/match) exceptionally well.
  - **Shot Conversion Rate:** Base goal threshold `0.10` (10% xG per chance) aligns with real-world Opta shot conversion averages (9.8% - 10.2%).
  - **Shot Distribution:** Cumulative weights (10% goal, 20% saved on target, 45% off target, 20% blocked, 5% foul) accurately reflect European league shot outcome breakdowns.
- **PM Conclusion:** The cumulative-threshold chance table structure is approved. Resolving Concerns 1-4 elevates the engine from a good mathematical abstraction to a data-backed realistic simulation.

An improved version of this is at [MatchSim v1.2](./matchsim_addendum_v1.2.md)