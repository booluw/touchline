# pkg/matchsim — Addendum v1.5 (tactical styles, stamina, and live tactical changes)

Status: **PROPOSAL** — structure follows the product-approved 5-style Simple-Mode
tactical specification (S05-01); the numeric modifiers are the midpoints of the
published ranges, marked **proposal** pending PM tuning sign-off (the tuning
block is data, so a later sign-off is a data-only change). Sprint: S05-01.
Companion to: `README.md` (`pkg/matchsim`), `docs/design/tactics-training-numerics.md`,
`docs/tasks/S05-01-squad-tactics-and-simple-training.md`.

This addendum upgrades the engine from **recorded-but-neutral** `tactic_change`
inputs (v1.4) to real, deterministic tactical modulation. It deliberately keeps
the v1.4 RNG consumption order unchanged: every style effect either (a) changes
a probability boundary, or (b) scales a value that never consumes RNG, so
replay determinism is preserved byte-for-byte and the v1.2-approved golden
digest still pins (the default **balanced** style is the identity block).

## What is new

1. **`Tactics`** — one field per team, `Team.Tactics { Style string }`.
   The style is one of the five product keys below. Empty or unknown styles
   normalize to `balanced` (identity), so any legacy caller that omits tactics
   plays the v1.4 behavior exactly.
2. **`Team.Fitness`** — a `float64` in `[0,1]`, defaulting to `1.0` when `<=0`.
   It seeds the team's per-side stamina accumulator so the orchestration
   layer's player-condition subsystem (`player.player_condition`) can reach the
   engine without the engine knowing anything about the database.
3. **`Tuning.Styles`** — per-style numeric block, plus `Tuning.DefaultStyle`.
   Values are consumed as config, never hardcoded; a tuning change is a data
   change with a version bump (same discipline as the rest of `Tuning`).
4. **A per-side stamina accumulator** — drains every minute by the team's style
   stamina rate, restores on substitution, and applies a **post-75th-minute
   fatigue penalty** when drained. This gives `stamina` a match-day
   consequence in the engine itself.
5. **Live `tactic_change` inputs now have a numeric effect.** A
   `LiveInput{Kind: "tactic_change", Detail: {"style": "low_block"}}` switches
   that side's active style for the remainder of the match from the input's
   minute. Because styles only re-weight existing draws, switching styles
   mid-match stays fully deterministic (`seed + ordered inputs` ⇒ identical
   outcome).

## Style keys and their semantics

| Key | Name | PossessionShift (Δp_home) | ChanceVolume | GoalConversion | ConcededConversion | CardRate | StaminaDecay |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `balanced` | Balanced (default) | `0` | `1.00` | `1.00` | `1.00` | `1.00` | `1.00` |
| `possession` | Possession Control | `+0.20` | `0.90` | `1.20` | `1.20` | `0.80` | `1.00` |
| `gegenpress` | Gegenpress / High-Press | `+0.125` | `1.25` | `1.40` | `1.80` | `1.40` | `1.35` |
| `low_block` | Low-Block / Counter | `-0.175` | `0.70` | `2.20` | `0.60` | `1.15` | `0.85` |
| `direct` | Direct / Long-Ball | `-0.075` | `1.15` | `0.80` | `1.00` | `1.10` | `1.05` |

Numbers are **midpoints** of the published ranges in
`docs/tasks/S05-01-squad-tactics-and-simple-training.md` (e.g. possession shift
`-15%..-20%` ⇒ `-0.175`); all are **proposal** and swapped by a tuning pass.

### How each modifier reaches the simulation

- **Possession** — the engine's possession share (spec §2.1, v1.4) is computed
  from the two combined ratings, then both styles shift it additively:
  `share' = clamp(share + ΔHome − ΔAway, 0.05, 0.95)`. Each team's
  `PossessionShift` applies to its own share, so a low-block host drops toward
  35% against a balanced visitor, and a possession-control host rises toward
  65% against the same opponent (matching the matrix's "35–42%" and "60–70%"
  bands).
- **Chance volume** — a team's per-minute chance arrival rate is
  `(ChancesPerMatchMin / 90) × teamStyle.ChanceVolume`. This is the matrix's
  `−30% / +25% / −10% / +15% / 0%` lever.
- **Goal conversion (own + conceded)** — the goal bucket's weight is scaled by
  `attackingStyle.GoalConversion × defendingStyle.ConcededConversion`. Own
  conversion is the matrix's "base shot xG"; conceded conversion is the
  "conceded shot xG risk" (a high-press side's 0.18 breakaway risk inflates the
  opponent's goal chance; a low-block's 0.06 suppresses it).
- **Card rate** — the fouling side's yellow-card probability is scaled by
  `foulingStyle.CardRate` (tactical-foul coefficients: high press +40%, low
  block +15%, possession −20%).
- **Stamina** — per side, `stamina` starts at `Team.Fitness` and drains each
  minute: `stamina -= (1/90) × style.StaminaDecay`. On substitution (manager
  `LiveInput` or auto) `stamina` restores to `Tuning.SubFitness` and the
  existing `SubStaminaBoost` applies. **From `Tuning.FatigueStartMinute` (76)**,
  a side whose tank lags the healthy norm — the tank of a fresh (Fitness 1.0)
  side draining at the neutral (1/90) rate, i.e. `(90-minute)/90` — takes an
  effectiveness penalty on its Attack/Defense:
  `deficit = max((90-minute)/90 - stamina, 0)`,
  `eff = 1 − clamp(deficit × FatiguePenaltyScale, 0, FatiguePenaltyMax)`
  (`0.5`, `0.15` respectively — max −15%). Because the deficit is the hole vs
  the *healthy norm*, a fully-fit balanced side is at `eff = 1` every minute
  (byte-identical to v1.4), while lower squad conditioning and faster-decay
  styles (gegenpress +35%) burn the tank late; a low-block side (−15%) preserves
  it. The penalty scales that side's ratings uniformly, so it cuts possession
  momentum and both attacking and conceding conversion through the existing
  ratio math.

All five levers keep the canonical draw order (addendum v1.4 Part 3) intact —
they re-weight existing draws, never add new RNG consumption.

## Live tactical changes (replaces the v1.2 "no numeric effect" note)

- The persisted and wire kind stays the canonical **`tactic_change`** (existing
  `match.match_inputs` replay continuation). The S05-01 spec's examples spell it
  `tactical_change`; `internal/match` normalizes that spelling to `tactic_change`
  on ingress (`match.match_inputs.kind` is free `TEXT`, so no schema change is
  needed). `pkg/matchsim` only ever sees `tactic_change`.
- `LiveInput.Detail` for a tactical change carries `{"style": "<key>"}`; see
  `Team.Tactics` for validation. On replay, the input's minute applies the new
  style from that minute on — possession, chance volume, conversion, cards, and
  stamina drain all follow the new style for the remainder of the match.

## `internal/match` orchestration contract (S05-01)

The engine takes the *effective* team ratings; the orchestration layer owns:

1. translating the club's saved tactical setup into the style's
   attribute-weight profile when aggregating `Attack`/`Defense`
   (`squad.StyleProfiles`, see `docs/design/tactics-training-numerics.md`);
2. folding player condition (`player.player_condition`: fitness, sharpness,
   fatigue) into `Team.Fitness` and the per-player performance factor;
3. stamping `Team.Tactics` from the saved setup (or `balanced` for AI clubs
   with no saved setup);
4. persisting the frozen setup in the kickoff snapshot (`sim_inputs`) so replay
   rehydrates the identical tactics.

## Versioning

- New `Tuning.Version` for this block; `match.matches.engine_version` records
  it per match.
- RNG consumption is unchanged, so a `balanced`-vs-`balanced` match is
  numerically identical to v1.4. Cross-version *byte-exact* replay of v1.4
  matches is still not guaranteed for non-balanced teams (outcome meanings
  differ); replay exactness applies within one engine version (documented
  limitation, unchanged from v1.4).