# Academy Numerics (S08-01)

Source of truth for the decision numbers behind academy investment tiers and
procedural youth intake, mirroring the format of the other sprint numerics
docs. Prompts, tables, and formulas here are the **proposal** values awaiting PM
tuning sign-off; the implemented constants live in `internal/academy/model.go`,
`internal/academy/_numerics.go`, `internal/finance/academy.go`,
`internal/squad/overall.go`, and `pkg/playergen` (never freeform in engine
code). Recalibration is a data-only edit against those tables — the steering
logic in the services does not change.

## 1. Investment tiers and annual operating cost

A club's `investment_tier` (1..5) fixes its fiscal footprint. Annual cost is
debited month by month (`annual_cost / 12`) as a recurring academy maintenance
entry, appended deductively to the club's ledger with a per tick idempotency
key (`academy:maintenance:<club>:<tick>`).

| Tier | Name | Annual cost | Monthly debit | Weekly facility budget |
|---|---|---|---|---|
| 1 | Farm system | £250,000 | £20,833 | — |
| 2 | Youth academy | £600,000 | £50,000 | — |
| 3 | Regional academy | £1,200,000 | £100,000 | — |
| 4 | National academy | £2,400,000 | £200,000 | — |
| 5 | World-class factory | £4,800,000 | £400,000 | — |

## 2. Youth contracts and wages

Intake prospects are offered a fixed **3-season academy youth contract**
(`YouthContractYears`, `YouthContractTermSeasons`). Their weekly wage is
tier-scaled; finance derives the youth weekly wage from the tier so the wage
_commitment_ mirrors the on-paper contract for budget calibration.

| Tier | Youth weekly wage |
|---|---|
| 1 | £500 |
| 2 | £1,000 |
| 3 | £1,500 |
| 4 | £2,000 |
| 5 | £2,500 |

The signing lands three rows atomically in one transaction: the
`player.contracts` youth row plus its `finance.wage_commitments` twin
(annualized), mirroring how `BootstrapClub` seeds first-team wage commitment.

## 3. Procedural intake — club academies

Each season a club academy runs a deterministic youth intake keyed on
`world_id + club_id + season_number` (street intake additionally keys
`country_id`), so replays are stable and retries are idempotent (the season's
academy rows are upserted on the intake signature).

Cohort size and talent profile follow the tier:

| Tier | Prospects | Talent pool odds (J : TP : WK : GEN) | Potential floor bias |
|---|---|---|---|
| 1 | 5 | 980 : 18 : 2 : 0 | journeyman-leaning |
| 2 | 7 | 950 : 45 : 5 : 0 | low ceiling |
| 3 | 9 | 900 : 90 : 9 : 1 | mid ceiling |
| 4 | 11 | 830 : 150 : 18 : 2 | high ceiling |
| 5 | 13 | 750 : 210 : 35 : 5 | top ceiling, rare generational |

Odds sum to 1000 per tier; tier 5 only ever yields one generational per
season in expectation (35 per 1000 rolls), and tier 1 never yields one.

### Numerics behind a prospect

Two independent draws run per player:

1. **Talent class** (`rollTalent`) — rarity with tier-shifted weights
   (`TalentOddsForTier`). Potential is then **floored by class**: a
   TopProspect always starts the season inside its rolling development band,
   a Wonderkid never below it, a Generational gets the highest guaranteed
   floor — so high-ceiling prospects cannot roll journeyman potential. The
   floor is `PotentialFloor(class)` applied after the raw `generateAttributes`
   roll and only raises the value (talent cannot lower a raw roll).
2. **Attribute baseline** (`QualityOffsetForTier`) — a deterministic tier
   skew `+tier*3` offset plus `reputation/10` clamp, applying a visible
   everyday-quality advantage at higher tiers without leaking a full
   ceiling: `offset ∈ [−20, +20]`.

### Career potential model

Potential is a per-season constant stored on the prospect's persona
(`potential_floor` on the player row); the rolling floor rises each season by
`PotentialGrowthPerSeason` until the ceiling, so Wonderkid/Generational
players spend several seasons improving into their ceiling rather than
peaking at intake.

> **Outcome note — the floor guarantees the *starting* band, not the
> realized peak.** The talent floor is applied once at intake: it lifts and
> floors the hidden `potential` roll so a Wonderkid cannot roll journeyman
> potential (`pkg/playergen/talent.go` `potentialBonus`). It does **not**
> march the week-by-week attributes up to that ceiling. Weekly growth is
> minutes- and morale-gated (`internal/training/deltas.go`): a Wonderkid who
> is benched, gets poor morale and negative net deltas week after week
> stagnates or declines **below** his potential. A busted wonderkid is a
> first-class outcome, not a bug — talent fixes the floor of the dice, it
> does not force the career.

## 4. Street academy intake (A04/A05)

Countries run a secondary **street intake** for unaffiliated 13–15s into the
free-agent pool, separate from club academies:

- cohort size between `StreetMinProspectsPerCountry` and
  `StreetMaxProspectsPerCountry` (deterministic per world+country+season);
- same quality offset machinery (tier .src from country average reputation);
- same deterministic intake key with a `country_id` component so multiple
  world tick deliveries never double-count.

Street prospects are judged by the same talent machinery, so a street
discovery **can** roll a Wonderkid ceiling:

> **Wonderkid odds 3/1000; Generational is impossible.** `streetTalentOdds`
> (`internal/playerpool/street_intake.go`) is `975 : 22 : 3 : 0` — the same
> rarity bands as world odds, but with no Generational weight. A street kid
> has a 3-in-1000 chance of rolling the Wonderkid ceiling (full wonderkid
> floor via `potentialBonus`), but the Generational class is structurally
> closed to the street route. Street discoveries therefore skew heavily to
> journeymen: a street wonderkid is a genuine story worth preserving.

## 5. Academy maintenance, investment and shutdowns

| Action | Effect |
|---|---|
| Monthly maintenance | debits `annual_cost / 12`, ledger category `academy`, idempotent per world tick |
| Upgrade to tier N | one-off `UpgradeCostByTier` debit + `annual_cost` switches to tier N |
| Shutdown | academy inactive, intake paused, sentiment penalty applied, ongoing cost **stopped** (no maintenance debit) |
| Reopen | academy active again, intake resumes next season intake |

Upgrade costs are upgrade-only: moving **into** tier N charges its cost,
moving **down** refunds nothing (downshifts simply reduce annual cost).
Shutdowns give immediate budget relief but carry a boardroom/supporter
sentiment cost (`AcademyShutdownSentimentHit`), so emergency cash relief is
not free.

## 6. Read models (canonical OVR surface)

Anywhere an overall-driveable figure is surfaced (free-agent listing,
club roster, player detail, dashboard hints), it goes through the single
canonical calculation `squad.PositionalOverall` over the category averages —
never a stored or ad-hoc composite. Weekly deltas land on
`player.player_attribute_changes` (keyed by the morally-relevant pseudo key)
and feed the same signed delta aggregation for morale/attribute read models,
so intake players and first-team reads agree on the same numerics.
