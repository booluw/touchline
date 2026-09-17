# Player Lifecycle (A01–A10)

Source of truth for the implemented player lifecycle: how players enter a world,
age, become eligible, get signed, retire, and how worlds replenish thin squads.
Where the product tasks/intents (`docs/tasks/sprints/S08-01-*.md`, OPD-26..29 in
`docs/product_manager.md`) publish intent, this document fixes the **implemented
values** and binds them to the `playerpool.*`, `academy.*`, `lifecycle.*`, and
`squad.*` schemas. All numbers are config constants in the owning packages
(never freeform in handler code), matching how `transfer.Numerics` and
`squad.DefaultPositionWeights` are binned.

Worlds and managers/contracts live in the `manager.managers` / `club.clubs` /
`world.worlds` sprint schemas (S02-01/S03-01); this document is the player-side
counterpart.

## 1. Country-scoped player pools

Every world keeps a **shared country pool** per country — a head list of
free agents the whole world's clubs draft/sign from, rather than per-club
player generation (OPD-27).

- Place: `playerpool.pool` (rows = unsigned free agents of a country).
- Origin: `player.pool_origin` is `street` | `academy` | `generated` | `bulk` |
  `draft` (see `pkg/playergen`,`playerpool · Origin`).

### Replenishment (A01-A03 seasonal intake)
Each `WORLD_LIFECYCLE_SEASON_COMPLETED` rollover replenishes the pool:

1. **Club academy intake** (A05) — products of club academies aged **15–17**
   (`academy.YouthIntakeMinAge`/`MaxAge`) who did not sign a youth offer are
   moved/staged toward the pool.
2. **Country street intake** (A04) — the country academy (admin subsidy)
   discovers **10–20 street kids aged 13–15** each season
   (`academy.StreetMinCount`/`MaxCount`, `StreetMinAge`/`MaxAge`), very young
   free agents for the pool.

### Draft model (A02/A03)
- New worlds draft their opening squads from the country pool via the draft
  flow (`playerpool.Draft`, `world.Seed`), so the launch squad template is
  filled from shared pool players.
- The AI auto-fill gate (OPD-29, A09) backstops it: any squad below
  `lifecycle.TargetSquadSize` (**24**) with a thin position group is topped up
  deterministically (gap-deepest-first across GK/DEF/MID/FWD quotas — see
  `docs/design/player-lifecycle.md` §9).

## 2. Aging and retirement

### Aging (A06)
Players age **+1 season** at each `WORLD_LIFECYCLE_SEASON_COMPLETED` rollover
(`lifecycle.OnSeasonCompleted`, `squad.store` `ComputedAge`). Existing
`ComputedAge`/contract state is re-derived, not stored per-player.

### Retirement (A06)
A player retires (`lifecycle.RetirementProbability` — the age curve) at a
rollover once the probability crosses the roll. Modifiers on the base curve:

| Factor          | <threshold | mid        | >threshold |
|-----------------|-----------|------------|------------|
| Ability         | <50 → **1.5x** | 50–70 → 1.0x | >70 → **0.6x** |
| Ambition        | <30 → **1.3x** | 30–70 → 1.0x | >70 → **0.7x** |

Retirement **aftermath** is mandatory: the player's contract is terminated
(wage commitment released), the squad slot frees, and `PLAYER_RETIRED` is
emitted so the boardroom/UI can show the farewell. No residual wage is carried.

## 3. Match eligibility gate (A07)

A player is match-eligible only when ALL of:

1. **Professional contract** (signed, not on loan).
2. **Street-origin floor**: origin `street` players are match-ineligible until
   age **18**. Academy-origin and other origins have no age floor (OPD-28).
   Implemented as `squad.MatchEligible(origin, age, hasContract, open)`.

Academy products are immediately registered under a pro (youth-grade) contract
able to play; street origin enforces the 18+ gate (OPD-28). Street signings
before 18 hold the player out of matches until the floor is crossed.

## 4. Signing flow

### Human (A08)
`playerpool.SignFreeAgent` signs a free agent from the pool to a club:
- validity/eligibility checks, wage commitment (finance), contract creation,
  `PLAYER_SIGNED` emission, and the player leaves the pool.
`playerpool.ReleasePlayer` releases a contracted player back to the pool
(contract voided, `PLAYER_RELEASED`).

### AI auto-fill (A09)
`lifecycle.AutoFill` tops up AI clubs whose squads fall below TargetSquadSize
**(24)** with quota-aware free-agent signings (deepest gap group first, overall
top-up when no group is thin). AI fill uses the same `SignFreeAgent` path, so
no bypass — wage budget and finance rules hold. Emits `AI_AUTO_FILL`.

### Admin bulk (A10)
`playerpool.BulkCreate` generates **1–500 players** into a country pool under
an admin, with deterministic quality bands (offset applied to generated
overall) and optional position/age constraints. Emits `ADMIN_BULK_PLAYER_CREATED`.

| Quality band | overall band | offset |
|--------------|-------------|--------|
| low          | 35–55       | -10    |
| mid          | 50–70       | 0      |
| high         | 65–85       | +15    |
| elite        | 80–95       | +25    |

## 5. Academy (S08-01, OPD-23)

`academy.Academy` is the club-owned youth factory:

- Investment tiers **1–5** (`academy.AnnualCostByTier`): 250k / 600k / 1.2M /
  2.4M / 4.8M per season, monthly-debited.
- **Youth intake** each season: 15–17yo prospects scaled by tier.
- Products are club-origin players, immediately pro-eligible (no street floor).

The **country/street academy** (A04, public good) is the street-player
pipeline: 13–15yo kids, 10–20 per country per season, signable but
match-ineligible until 18.

## 6. Seasonal rollover sequence

On each `WORLD_LIFECYCLE_SEASON_COMPLETED`:

1. Replenish pool: club-academy products + street intake (A04/A05).
2. Recompute ages; run retirement + aftermath (A06).
3. Run eligibility/registration gate (A07).
4. Re-run AI auto-fill so nobody plays short-handed (A09).

## 7. Event catalogue

| Event | Emitter | Meaning |
|-------|---------|---------|
| `WORLD_LIFECYCLE_SEASON_COMPLETED` | scheduler/world | rollover driver |
| `PLAYER_CLAIMED_FROM_POOL` | playerpool (draft) | draft pick signed |
| `ACADEMY_INTAKE` | academy | club academy youth intake |
| `COUNTRY_ACADEMY_INTAKE` | academy | country street intake |
| `PLAYER_RETIRED` | lifecycle | retirement + contract aftermath |
| `PLAYER_SIGNED` | playerpool | free agent → club |
| `PLAYER_RELEASED` | playerpool | club → free agent |
| `AI_AUTO_FILL` | lifecycle | AI thin-squad top-up |
| `ADMIN_BULK_PLAYER_CREATED` | playerpool | admin bulk create |

Existing eventbus delivery/undo/redelivery semantics apply to all of these.

## 8. Finance interactions

- Youth/street signings carry youth-contract wages (finance `wage_commitment`).
- Academy tier cost is monthly-debited; shutdown hits sentiment.
- Signing/release settle through the finance ledger (club wages, pool
  per-country budget guard for AI fills).

Cross-references: `docs/tasks/sprints/S08-01-*.md`, `S08-02`, `docs/tasks/adhoc/A01–A10`,
`docs/product_manager.md` OPD-26..29.
