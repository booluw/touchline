# matchsim — deterministic match engine

`pkg/matchsim` is a pure, seeded, deterministic match engine (S04-02). It has no
database, network, or wall-clock dependency: identical
`(seed, teams, tuning, live inputs)` produce byte-identical results. Persistence,
live pacing, and result application live one layer up in `internal/match`.

## Spec (authoritative)

The product/engineering contract for this engine is versioned in the addenda,
in reading order. **v1.4 is cumulative** — where later addenda restate an
earlier definition, the later one wins.

| File | What it adds |
| --- | --- |
| `matchsim_addendum_v1.1.md` | Two-sided Attack/Defense, possession model, form/morale/motivation factors, tuning versioning. |
| `matchsim_addendum_v1.2.md` | Approved baseline, tuned numbers, marginal referee events, penalty data model, postulate on variance band. |
| `matchsim_addendum_v1.3.md` | Proposed Part 3: canonical replay draw order + red-card/substitution effects + penalty/award/reference scopes. |
| `matchsim_addendum_v1.4.md` | APPROVED Part 4-6 block: final numbers, draw order, penalty/referee scopes; engine_version `1.2-approved`. |
| `matchsim_addendum_v1.5.md` | PROPOSAL: five tactical styles (Simple Mode, S05-01), per-side stamina + post-75 fatigue, live `tactic_change` with a real effect. |

Implementation interpretations fixed by this repo (posted in the addenda as
proposals; where the spec does not say, the engine owner's interpretation below
is the contract):

- **Variance** (§2.3): one triangular draw per team per match from
  `[0.85, 1.15]` centred at 1.0, drawn home-first in the canonical order.
- **Foul/card model** (Concern 2 / Understanding 13): defensive fouls are a
  per-minute rate per team (`FoulBasePerMatchMin/90`) scaled by the defender's
  Aggression×RivalryIntensity; cards couple to fouls — there is **no**
  independent per-minute card draw (draw order Part 3, step 3-4).
- **Home advantage**: the ×1.08 multiplier is applied to the HOME side's raw
  effective ratings at kickoff (before form→variance), feeding possession and
  goal scaling; red/subs subsequently act on the already-adjusted ratings.
- **tactic_change LiveInputs** carry a numeric effect from v1.5: a live
  `{"style": "<key>"}` input switches the side to that style for the rest of the
  match (possession/chance/conversion/cards/stamina re-weight existing draws,
  never new RNG consumption); default `balanced` is the identity block, so a
  tactics-less legacy call replays the v1.4 behavior exactly.
- Post-75th-minute fatigue penalty applies when a side's stamina tank lags the
  healthy norm (addendum v1.5): `deficit = max((90-minute)/90 - stamina, 0)`,
  `eff = 1 - clamp(deficit × FatiguePenaltyScale, 0, FatiguePenaltyMax)`, gated
  from `FatigueStartMinute`; substitutions restore the tank to `SubFitness`.
- Late-game urgency / fatigue is **out of scope** for v1.2 (flat goal timing).

## Canonical draw order (replay contract, EngineVersion "1.2-approved")

See `matchsim_addendum_v1.4.md` Part 3 for the normative text. The engine's RNG
consumption order, which replay depends on:

1. `triangular` variance for the **home** side
2. variance for the **away** side
3. per minute, in order: possess → chance? → outcome (in-box foul → referee
   award → conversion) → on-target feed → foul draw (non-possession side) →
   card/injury on any foul of the minute → substitutions at windows (LiveInput
   or auto). Half/full time are synthesized at 45/90 after the minute's draws.

## Tuning

`Tuning` is a versioned block (data, not code). The committed numbers live in
`tuning.go`; fields are marked **approved** (PM-signed) or **proposal**
(implemented, awaiting PM tuning sign-off). Any structural or numeric change
must bump `EngineVersion`.

## Persistence contract

The engine emits `MatchEvent` types that are an exact subset of the
`match.match_events` `CHECK` constraint (`migrations/0011_match.up.sql:40`).
`EventDetail` is persisted as `match_events.detail`. Penalty decisions reuse
`chance_created` for waved appeals rather than inventing event types.

## Tests

`simulate_test.go` pins the golden replay digest of seed 424242 under the
v1.2-approved block (`crypto/sha256` of score/possession/events). Any change to
structure, numbers, or commentary must re-pin that value.