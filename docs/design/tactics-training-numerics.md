# Tactics & Training Numerics (S05-01)

Source of truth for the byte-compatible numbers behind S05-01. The product
spec (`docs/tasks/S05-01-squad-tactics-and-simple-training.md`) publishes
ranges; this document fixes the implemented values (midpoints where ranges) and
binds them to the existing schema and the match engine. All numbers here are
**proposal** until PM tuning sign-off; every block is config/data, never
hardcoded in engine code.

## 1. Tactical styles — Simple Mode

The manager's tactical control in Simple Mode is **one of five styles**. Styles
map to `matchsim.StyleSpec` (see `matchsim_addendum_v1.5.md`); the engine
consumes possession shift, chance volume, own + conceded conversion, card rate,
and stamina decay. The orchestration layer additionally applies a per-style
attribute-weight profile when rating the team.

### 1.1 Styles and their formations

A style may be played in any of its **allowed formations**; the first is its
default (used when the manager only picks a style). Slot orders are 11 slots in
`club.club_lineups` / `squad.FormationFor(name)` coordinates.

| Style key | Name | Allowed formations (default first) |
| --- | --- | --- |
| `balanced` | Balanced | `4-3-3`, `4-4-2`, `4-2-3-1`, `5-3-2` |
| `possession` | Possession Control | `4-3-3`, `3-2-4-1` |
| `gegenpress` | Gegenpress / High-Press | `4-3-3`, `4-2-3-1` |
| `low_block` | Low-Block / Counter | `5-4-1`, `4-5-1` |
| `direct` | Direct / Long-Ball | `4-4-2`, `3-5-2` |

### 1.2 Formation slot orders (11)

`4-3-3` (default slot order, unchanged) — `GK LB CB CB RB CM CM CM RW ST LW`
`4-4-2` — `GK LB CB CB RB RM CM CM LM ST ST`
`4-2-3-1` — `GK LB CB CB RB DM DM RM AM LM ST`
`5-3-2` — `GK CB CB CB LB RB CM CM CM ST ST`
`3-2-4-1` — `GK CB CB CB DM DM RM AM AM LM ST`
`5-4-1` — `GK CB CB CB LB RB LM CM CM RM ST`
`4-5-1` — `GK LB CB CB RB LM CM CM CM RM ST`
`3-5-2` — `GK CB CB CB LB RB CM CM CM ST ST`

All orders use only schema positions; `squad.positionFit`'s family matching
(defensive / midfield / forward, keepers never outfield) validates filling.

### 1.3 Engine block (midpoints of the matrix)

| Style | PossessionShift | ChanceVolume | GoalConv | ConcededConv | CardRate | StaminaDecay |
| --- | --- | --- | --- | --- | --- | --- |
| `balanced` | 0 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| `possession` | +0.20 | 0.90 | 1.20 | 1.20 | 0.80 | 1.00 |
| `gegenpress` | +0.125 | 1.25 | 1.40 | 1.80 | 1.40 | 1.35 |
| `low_block` | −0.175 | 0.70 | 2.20 | 0.60 | 1.15 | 0.85 |
| `direct` | −0.075 | 1.15 | 0.80 | 1.00 | 1.10 | 1.05 |

### 1.4 Style attribute-weight profiles (pre-match rating)

The matrix's "Key Player Attribute Weights" become per-style **category**
weights (the engine rates six categories: technical, physical, mental,
tactical, positional, goalkeeping — that is all the EAV stores). Attack/Defense
per position use the style's profile as the recipe in `BuildSquadRatings`,
replacing the single default recipe table. Only the profiles that *differ* from
`DefaultPositionWeights` are stored as overrides; all non-listed positions fall
back to the default. Implemented weights are the closest category expression of
each style's key attributes (proposal):

- **possession** — attack slightly *technical/mental-led* (passing, vision,
  composure); defense *mental-led* (interceptions/positioning): global
  `Technical ×1.10`, `Mental ×1.10`, `Physical ×0.92` on both sides of every
  position recipe.
- **gegenpress** — attack *physical-led* (stamina, work rate, pressing →
  physical/tactical): attack `Physical ×1.12`, `Tactical ×1.10`; defense
  `Physical ×1.05` (pace coverage). GK/defensive recipes unchanged.
- **low_block** — defense *tactical/technical-led* (positioning, tackling) and
  attack *physical-led* (pace): defense `Tactical ×1.12`, `Physical ×1.05`;
  attack `Physical ×1.08`.
- **direct** — attack *physical/technical-led* (strength, heading, jumping +
  finishing): attack `Physical ×1.10`, `Technical ×1.06`; defense default.
- **balanced** — identity (= `DefaultPositionWeights`).

### 1.5 Style efficacy (tactical familiarity coupling)

A club's `player.player_condition.tactical_familiarity` (aggregated to the XI)
scales its style block: `efficacy = clamp(0.75 + 0.5 × familiarity, 0.75, 1.25)`
is applied to possession shift, chance volume, and conversion modifiers at
kickoff (cards/stamina untouched). Balanced is unaffected (all identity).
MVP keeps this to one coefficient; fine-tuning later is a tuning change.

## 2. Training — weekly Simple-Mode archetypes

One active **training plan per club** (`club.club_training_plans`), chosen from
five archetypes. The worker's **weekly** `WORLD_TICK` branch processes every
club with an active plan: attribute-key deltas, condition updates, and a
`TRAINING_WEEK` event.

### 2.1 Archetype attribute deltas (per week, per player)

Keys are the **existing** `player.player_attributes.attribute_key` catalogue
(`pkg/playergen/attributes.go`) — no new attribute system. New catalogue keys
required by the matrix: `tackling` (technical), `marking` (positional),
`teamwork` (mental); a migration backfills existing players' values with their
category mean so current ratings are unaffected.

| Archetype key | + growth per week | − decay per week | Fatigue step | Injury mult | Sharpness |
| --- | --- | --- | --- | --- | --- |
| `technical` (Technical & Ball Control) | `passing +0.3`, `vision +0.2`, `first_touch +0.3`, `composure +0.2` | `strength −0.1`, `tackling −0.1` | 0.80× | 0.70 | +5pts midfielders/wingers |
| `physical` (Physical & Endurance) | `stamina +0.4`, `natural_fitness +0.3`, `strength +0.3`, `work_rate +0.2` | `composure −0.1` | 1.40× | 1.35 | +2pts |
| `defensive` (Defensive Organization) | `positioning +0.4`, `tackling +0.3`, `marking +0.3`, `concentration +0.2`, `teamwork +0.2` | `off_the_ball −0.1` | 1.00× | 0.85 | +3pts |
| `attacking` (Attacking Movement & Finishing) | `finishing +0.4`, `off_the_ball +0.3`, `pace +0.2`, `anticipation +0.2` | `marking −0.1`, `positioning −0.1` | 1.20× | 1.10 | +6pts strikers |
| `recovery` (Recovery & Tactical Rest) | `decision_making +0.1`; tactical_familiarity (condition) +0.05 | physical decay if 3+ consecutive recovery weeks (below) | −0.40 (removal) | 0.20 | −2pts |

### 2.2 Condition subsystem (`player.player_condition`)

Per player, `[0,1]` each, seeded at squad materialization (lazy defaults on
first read for pre-existing players):

| Column | Start | Weekly rule |
| --- | --- | --- |
| `fatigue` | 0 | `fatigue = clamp(fatigue + 0.05 × FatigueStep, 0, 1)` — recovery uses `−0.40` *removal* (net drop of 0.20 toward the 0.05 baseline step: `clamp(fatigue − 0.20, 0, 1)`). |
| `fitness` | 1.0 | `fitness = clamp(fitness + 0.05 − 0.05 × FatigueStep, 0, 1)` (players recover between weeks; harder plans recover less). |
| `sharpness` | 0.5 | `sharpness = clamp(sharpness + 0.02 + SharpnessBonus, 0, 1)` per archetype table. |
| `injury_risk` | `injury_susceptibility/200` | `risk = clamp(risk + 0.05 × (InjuryMult − 1) × (1 − risk), 0, 1)` (punched toward 0/1 by mult). |
| `tactical_familiarity` | 0.5 | `fam += 0.02` else `+0.05` on recovery; clamp `[0,1]`. |

### 2.3 Age & potential scaling

- **16–21:** positive attribute deltas × **1.8**; decay stays × 1.0.
- **22–29:** × 1.0.
- **30+ :** mental-key growth × **1.2**; and unless the plan is `physical`,
  veterans also decay `pace −0.05`/wk and `stamina −0.05`/wk (the matrix's
  −0.2/month ≈ −0.05/week).
- All attribute values clamp to `[1,100]`. Hidden `potential`
  (`player_hidden_traits.potential`) is **not** enforced yet — dynamic potential
  lands in S08-02; MVP training only respects age.

### 2.4 Matchday coupling (orchestration)

`internal/match/buildTeam` folds condition into the team each matchday:

- `Team.Fitness` = mean `fitness` of the XI (seed of the engine stamina tank).
- Per-player performance factor `×= (0.9 + 0.4 × sharpness) × (1 − 0.3 × fatigue)`
  (multiplied into `BuildSquadRatings` factors, alongside the existing
  `ComputePlayerPerformanceFactor`).
- `tactical_familiarity` → style efficacy (§1.5).

## 3. Command & API surface

| Route | Service | Rules |
| --- | --- | --- |
| `GET /api/clubs/:id/squad` | `internal/squad` read | world-scoped; player + position + attributes + condition |
| `PUT /api/clubs/:id/lineup` | `internal/tactics.SetLineup` | ownership; active world; all 11 slots; `UNIQUE(club_id, player_id)` |
| `GET|POST /api/clubs/:id/tactics` | `internal/tactics` | style ∈ 5; formation ∈ style's allowed set; actor-type recorded |
| `GET|POST /api/clubs/:id/training-plan` | `internal/training` | archetype ∈ 5 |
| `POST /api/matches/:id/tactical` | `internal/match.TacticChange` | live only; minute `[current+1, 90]`; `{"style": …}` validated; kind normalized to `tactic_change` |
| `GET /api/managers/me/club` | resolve current club | actor's own club, world-scoped |

**Deadlines (server-validated):**
- Lineup/tactics: rejected (`409`) if the club's next scheduled fixture is
  already `live` or the world is paused/archived. On change after a fixture
  became live, the change applies from the *next* fixture.
- Training plan: effective from the **next** weekly `WORLD_TICK` for the world
  (stamped `effective_from_tick`); no rebate for a plan swapped mid-week.

## 4. Events (world.events, PublishTx outbox)

`LINEUP_SAVED`, `TACTIC_SET`, `TRAINING_PLAN_SET`, `TRAINING_WEEK` (per club).
`actor_type` ∈ `manager`|`policy_bot` is stamped from the invoking actor's
`manager.managers.is_policy_bot` — the command layer is actor-agnostic
(PolicyBot delegation, tech plan §10).

## 5. Migrations

- `0033`: `club.club_tactics`, `club.club_training_plans`; backfill the new
  attribute keys `tackling`/`marking`/`teamwork` for existing players (at the
  player's category mean, so current ratings are unaffected). `match.match_inputs.kind`
  is free `TEXT` (no `CHECK`), so the `tactical_change` → `tactic_change`
  normalization is pure service logic — no ALTER needed.
- `0034`: `player.player_condition` (per player, five `NUMERIC` columns +
  `updated_at`).