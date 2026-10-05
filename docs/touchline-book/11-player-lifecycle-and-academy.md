# Chapter 11 — Player lifecycle: pools, academy, intake and retirement

Players enter a world, age, become eligible, sign, move, and retire. This
chapter follows that arc.

```
 street intake ──┐                       ┌──► club (contract) ──► transfer ──► …
 club academy ───┼──► country pool ──────┤                                      │
 bulk / draft ───┘   (free agents)       └──► AI auto-fill                      ▼
                                                                retire (age curve)
```

## 11.1 Country free-agent pools (OPD-27)

Each country has a shared, permanent **pool** of unsigned free agents
(`playerpool`). Clubs draft and sign from it; it is replenished, never a
one-time lump.

- Target density: **100** per country (`playerpool.PoolTargetSize`), minted
  before the first draft at seeding and topped up after every club
  ([Ch. 5](05-worlds-and-setup.md)).
- A world-level bootstrap pool (`country_id IS NULL`) may also exist; admin
  dashboards surface it explicitly.
- Players keep their origin `country_id` for life (never updated on sign/release).

### Ways in

| Path | Who | Notes |
| --- | --- | --- |
| **Draft** | world seeding | opening squads drafted from the pool (`playerpool.Draft`) |
| **Street intake** (A04) | country, each season | 13–15-year-olds, origin `street` |
| **Club academy intake** (A05) | each club, each season | origin `academy`; unsigned products move toward the pool |
| **Admin bulk** (A10) | `POST /api/admin/worlds/:id/players/bulk` | 1–500 players; quality bands below |

Bulk quality bands: low 35–55 (−10), mid 50–70 (0), high 65–85 (+15),
elite 80–95 (+25).

### Signing and releasing

- `playerpool.SignFreeAgent` (human, A08): eligibility checks, wage commitment,
  contract, `PLAYER_SIGNED`, player leaves the pool.
- `playerpool.ReleasePlayer`: contract voided, back to the pool,
  `PLAYER_RELEASED`.
- **AI auto-fill** (A09, OPD-29): any AI squad below **24**
  (`lifecycle.TargetSquadSize`) with a thin position group is topped up
  deterministically, deepest gap first across GK/DEF/MID/FWD quotas, through the
  **same** `SignFreeAgent` path (no bypass of finance rules). `AI_AUTO_FILL`.
  Youth intake does **not** count against the 24.

## 11.2 Match eligibility (A07, OPD-28)

A player can be selected for a first-team match only if **all** hold:

1. a signed **professional** contract (not on loan, not retired/suspended);
2. origin ≠ `street` **or** age ≥ **18**.

Street is the only origin with an age floor. Academy products are registered on
a youth-grade pro contract and are immediately eligible. The gate is enforced at
squad-selection time (`squad.MatchEligible`), never at intake. Open injuries
also block selection ([Ch. 14](14-condition-and-injuries.md)).

## 11.3 The club academy (S08-01)

### Investment tiers

| Tier | Name | Annual cost | Monthly debit | Youth weekly wage | Cohort | Talent odds J : TP : WK : GEN (/1000) |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Farm system | £250,000 | £20,833 | £500 | 5 | 980 : 18 : 2 : 0 |
| 2 | Youth academy | £600,000 | £50,000 | £1,000 | 7 | 950 : 45 : 5 : 0 |
| 3 | Regional academy | £1,200,000 | £100,000 | £1,500 | 9 | 900 : 90 : 9 : 1 |
| 4 | National academy | £2,400,000 | £200,000 | £2,000 | 11 | 830 : 150 : 18 : 2 |
| 5 | World-class factory | £4,800,000 | £400,000 | £2,500 | 13 | 750 : 210 : 35 : 5 |

(Constants: `academy.AnnualCostByTier`, `TalentOddsForTier`, youth wage table in
`internal/finance/academy.go`.)

- **Maintenance** — `annual_cost / 12` debited on each month boundary, ledger
  category `academy`, dedup key `academy:maintenance:<club>:<tick>`.
- **Upgrade** — one-off `UpgradeCostByTier` debit; downgrades refund nothing.
- **Shutdown** — academy inactive, intake paused, maintenance **stopped**, and
  an immediate supporter sentiment hit of **−15** floored at 15
  ([Ch. 9](09-clubs-dna-supporters.md)). The event reports the delta actually
  applied.
- **Reopen** — intake resumes next season; sentiment is not restored.

### Intake mechanics

Each season, deterministic on `world_id + club_id + season_number` (idempotent
upsert on the intake signature):

1. **Talent class** drawn with the tier's odds; potential is then **floored by
   class** so a Wonderkid can never roll journeyman potential.
2. **Attribute baseline** skewed by `QualityOffsetForTier` (`+tier × 3` plus a
   reputation term), clamped to `[−20, +20]`.
3. A fixed **3-season youth contract** at the tier's wage; contract and wage
   commitment land atomically.

Ages 15–17 (`YouthIntakeMinAge/MaxAge`). Event `ACADEMY_INTAKE`.

> **A busted wonderkid is a feature.** The class floor guarantees the starting
> potential band, not the peak. Growth is minutes- and morale-gated
> ([Ch. 13](13-training-and-development.md)); a benched, unhappy wonderkid
> stagnates below his ceiling.

## 11.4 Street intake (A04, OPD-26)

The country academy discovers **10–20** unaffiliated kids aged **13–15** per
country per season (`StreetMinCount/MaxCount`, `StreetMinAge/MaxAge`),
deterministic on world + country + season. Odds `975 : 22 : 3 : 0` — a 3-in-1000
Wonderkid chance and **Generational impossible**. Street kids are signable
immediately but **match-ineligible until 18**. Event
`COUNTRY_ACADEMY_INTAKE`.

## 11.5 Ageing and retirement (A06)

On each lifecycle rollover:

- Players age +1 season; ages are always measured against the **world date**
  ([Ch. 4](04-world-clock-and-time.md)).
- Retirement probability follows an age curve (`lifecycle.RetirementProbability`)
  with modifiers:

| Factor | Low | Mid | High |
| --- | --- | --- | --- |
| Ability | < 50 → ×1.5 | 50–70 → ×1.0 | > 70 → ×0.6 |
| Ambition | < 30 → ×1.3 | 30–70 → ×1.0 | > 70 → ×0.7 |

- **Aftermath** is mandatory: contract terminated, wage commitment released,
  slot freed, `PLAYER_RETIRED`.

## 11.6 The seasonal sequence

On each country's `SEASON_COMPLETED` ([Ch. 7](07-seasons-and-rollover.md)):

1. Replenish the pool — club-academy products + street intake.
2. Age everyone; run retirement + aftermath — **once per `(world, season)`**,
   under an advisory lock, on whichever country's rollover comes first; later
   countries skip it (IM24, OPD-50).
3. Run the eligibility gate.
4. Re-run AI auto-fill.

Worlds with no leagues run the same pass on the day-364 fallback.

## 11.7 Event catalogue

`PLAYER_CLAIMED_FROM_POOL`, `ACADEMY_INTAKE`, `COUNTRY_ACADEMY_INTAKE`,
`PLAYER_RETIRED`, `PLAYER_SIGNED`, `PLAYER_RELEASED`, `AI_AUTO_FILL`,
`ADMIN_BULK_PLAYER_CREATED`.

## Connections

- Talent classes and potential: [Chapter 10](10-players.md), [Chapter 13](13-training-and-development.md).
- Academy finances: [Chapter 20](20-finance.md).
- Code: `internal/{playerpool,academy,lifecycle}`, `internal/squad`.
- Source: `docs/design/academy-numerics.md`, `backend/docs/design/player-lifecycle.md`, OPD-26–OPD-29, IM24.

---
[← Players](10-players.md) · [Contents](the-touchline-book.md) · [Next: Squad & tactics →](12-squad-tactics-lineups.md)
