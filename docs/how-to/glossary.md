# Glossary: game terms and how they are derived

A single index to every term used across the how-to guides and the API, with
the **implementation derivation** for each. This doc re-centralises the
per-slice design docs; the authoritative per-number sources are still the
`docs/design/*-numerics.md` files and the constants they cite (every value
here is tuning data expressed as constants — never freeform in engine code).

Notation: `clamp(x, lo, hi)` bounds `x`; `round()` is mid-point rounding;
money is in **USD**. At the service boundary money is integer **pence**;
`finance.*` columns store `NUMERIC(14,2)` pounds. The ledger
(`finance.ledger_entries`) is **append-only** — there is no balance column;
cash and every derived metric is `SUM(entries)` computed at read time.

---

## 1. World, time & lifecycle

| Term | Definition | Derivation / source |
| --- | --- | --- |
| **World** | A game server run: one database-scoped ecosystem (clubs, managers, calendars). |
| **World status** | `provisioning` (created) / `active` or `open_beta` (playable, ticks fire) / `paused` (halted) / `archived` (terminal). | Admin `POST /api/admin/worlds/:id/status`. |
| **world_seed** | The world's replay seed, a crypto-random `int64` minted on first successful seed. Per-league name scrambling uses `seed ⊕ leagueID`. | `world.worlds.random_seed`. |
| **current_day** | The world's calendar counter; `worldDate = launched_at (or created_at) + current_day days`. | Only `WORLD_TICK{daily}` advances it (`OPD-24`). |
| **Cadence** | The per-world cron schedule that fires `WORLD_TICK` events. Since IM02 there is exactly one registered granularity — `tick.daily_cadence` (default `0 0 * * *`); the weekly/monthly/hourly/seasonal cadences were retired, and a `WORLD_TICK` carrying a legacy granularity is logged and dropped. `tick.match_cadence` paces live matches and is excluded from the clock. See [cadences-and-time.md](cadences-and-time.md). | Defaults in `internal/world/service.go`. |
| **Daily tick** | Kicks off due matchdays (`KickoffDue`), paces live matches, then PolicyBot bid responses + the transfer-market sweep (`Transfers.DailyTick`). |
| **Week boundary** | Every `calendar.days_per_week` days (`day % days_per_week == 0`; default 7 — days 7/14/21/28…): training archetype deltas + condition, player weekly pass (morale/recovery/share/transfer-request assessment), rivalry reconcile. |
| **Month boundary** | Every `calendar.days_per_month` days (`day % days_per_month == 0`; default 30): wage posting (`4 × weekly_wage` per active commitment) + academy facility maintenance (`annual_cost / 12`) + board review (confidence + sackings). |
| **Hourly / seasonal tick** | Registered + recorded in `world.events`; **no gameplay hook today** (reserved). |

## 2. Competitions, seasons, standings

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **League (competition)** | Admin-declared per country: `tier`, `team_count` (even, ≥4), optional `promotions`/`relegations`. |
| **Membership** | A club's `competition.club_competitions` row; `role='league'` = domestic league seat. **Offers require it**; a club plays in as many leagues as declared. |
| **Season** | Per league, one at a time. Statuses: `in_progress` (season 1 via the start-season endpoint; season 2+ flipped from `upcoming` by the daily `ActivateDueSeasons`), `upcoming` (rollover season 2+; waits out the off-season gap), `completed`. `season_number = MAX + 1`. Label from the boot reference date. | See [seasons.md](seasons.md). |
| **StartSeason** | Admin endpoint `POST /api/admin/worlds/:id/leagues/:leagueID/season` creating season #1 (entries + deterministic double round-robin, one matchday per game-day, fixtures ≥2 game-days apart). Second call → `ErrLeagueAlreadySeeded`. Endpoint maps `404`/`409` for world/league/state errors. | `internal/competition/seeding.go`, `competition_handlers.go`. |
| **Off-season** | The admin-tunable gap after a season's last matchday before the next season's fixtures start: `season.off_season_ticks` (world config, default 30 daily ticks) with per-league override `competition_rules.scheduling_rules ->> 'off_season_ticks'`; read at rollover. Next season's `start_date = lastFixtureDay + gap`; wages, academy, and the transfer market keep running. |
| **Rollover** | When every league in a country applies its final result: `SEASON_COMPLETED` → `CLUB_PROMOTED`/`CLUB_RELEGATED` → `SEASON_CREATED`, all in one atomic transaction; the next season is created `upcoming`, anchored `lastFixtureDay + off-season gap`, and the daily tick flips it `in_progress` with a `SEASON_STARTED` event once its first fixture is due. | `internal/competition/rollover.go`, `season.go`. |
| **Standings position** | Table sorted `points DESC, (goals_for−goals_against) DESC, goals_for DESC, club name`. The `SEASON_COMPLETED` champion is `order[0]`. | `internal/competition/standings.go`. |
| **Six-pointer** | Fixture whose two clubs share a promotion or relegation battle band. |
| **Derby** | A fixture between clubs with a `club.rivalries` row and `intensity ≥ RivalryIntensityThreshold (60)`. Rivalries seed at club creation (city-derby proposal `intensity = 80`). | `internal/squad/store.go`. | [derby-rivalry-determination.md](../design/derby-rivalry-determination.md). |

## 3. Players, attributes & squad

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Attribute** | Per-player value in `player.player_attributes`, `[1,100]`, categorised: technical / physical / mental / tactical / positional / goalkeeping. |
| **Overall (positional)** | The canonical user-facing rating, `clamp(mean(weightedAttack, weightedDefense), 1, 99)` over the six category averages using the per-position `DefaultPositionWeights` recipe matchsim consumes ("one-member XI"). Same recipe everywhere (offers, academy read models, valuation source). | `internal/squad/overall.go`. |
| **Headline keys** | EA-style condensed per-position stat block (e.g. GK: handling, reflexes, diving…; ST: finishing, heading, composure, pace, off_the_ball, first_touch). |
| **Player status** | e.g. `active`, `injured`, `suspended`, `free_agent` — used by squad read models and offer `squad.size` (senior = active/injured/suspended). |
| **Squad role** | On the active contract; expected minutes share per role: `key_player` 0.75 (≈3 of 4 matches), `rotation` 0.45, `squad_player` 0.20, `development` 0.0. |
| **Condition** | Per player `[0,1]`: `fatigue` (0 start), `fitness` (1.0), `sharpness` (0.5), `injury_risk` (`susceptibility/200` start), `tactical_familiarity` (0.5). Weekly rules in the design doc. | [tactics-training-numerics.md](../design/tactics-training-numerics.md) §2.2. |

## 4. Form

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Form rating** | A rolling EWMA multiplier in `[0.85, 1.15]`, centred 1.0 (neutral): `current = clamp((1−α)·prev + α·quality, MinRating, MaxRating)` with `α = 0.2` and quality clamped `[0.5, 1.5]`. ResultQuality = `1 + GD_delta/ExpectedGDDivisor` (`1.0` when a team performs exactly as expected). | `internal/form/form.go`. |
| **form_string** | Display streak, e.g. `W-W`. Rides the same EWMA row (`club.form_state`). |
| **Club form in offers** | Omitted until a `club.form_state` row exists (club has played); `front` block: `rating` + `form_string`. |

## 5. Finance & contracts

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Opening capital** | Genesis credit `$40,000,000` (dedup key `genesis:opening_capital`). |
| **Budgets (per season)** | Transfer `$10M`, wage `$25M`, allocated on `finance.budgets`; `available = allocated − committed`. |
| **Weekly wage** | `positionBase[position] + bump(attrMean)`, `bump = (attrMean−50)²/20` only when `attrMean > 50` (max `125`, so an 100-mean striker ≈ `$9,625/wk`). Bases ~`$7.5–9.5k/wk` by position. |
| **Contract length** | `ContractSeasons(age)`: 4 (≤22), 3 (22–28), 2 (>28); startup deals run from the nearest 1 July a full season ahead. |
| **Wage posting** | Month boundary (default day 30), `4 × weekly_wage` per active commitment; dedup key `wage:<tick>:<contract>` → idempotent under redelivery. `WeeksPerSeason = 52`. |
| **cash** | `SUM(credit) − SUM(debit)` over the club's account (ledger, not a column). |
| **operating_profit** | Same sum for the current season (≥ 1 Jan), **excluding** `genesis:opening_capital`. |
| **Committed spending** | `annualWage + TransferBudget.committed + WageBudget.committed + future_installments` (installments 0 today). |
| **Projected year-end balance** | `cash − committed_spending + projected_revenue` (revenue 0 today; debt 0 — no lending). |
| **Summary invariants** | `factors` (per-category net) **sum exactly to cash**; a club that never touched the ledger returns a zeroed valid summary. |

## 6. Board, confidence & job security

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Persona** | One of `patient_owner`, `demanding_owner`, `financial_conservative`, `academy_owner`, `prestige_owner`, `political_board` (fallback); each carries a 7-weight row + negotiation tolerance (0–3). Seeded from the club's DNA archetype. |
| **DNA** | `competitive_ambition`, `patience` (…). Archetypes (giant, academy_club, moneyball_club, community_club, fallen_giant, investor_club, survival_club) set base ranges, jittered. |
| **Expected finish** | `clamp(round((110 − ambition)/8 [+ 1.5 if patience ≥ 70]), 1, 24)`. |
| **Expected points** | `clamp(round(88 − 3.5 × finish), 10, 95)`. |
| **Mandates** | Four per (club, manager, season): `league_finish` (primary), `points_target` (secondary), `operating_balance` (strategic, break-even 0), `wage_structure` (financial, budget 0). Graded each month-boundary board review (IM02; default day 30) with slacks/allowances (e.g. finish slack `3→0` as the season progresses; points window `−10`; wage tolerance `budget×(1+target+5%)`). |
| **Seven factor scores** | Each 0–100: `performance`, `expectations`, `financial`, `board_relationship`, `club_dna_alignment`, `supporter_sentiment`, `alternatives`. Formulas in the design doc (e.g. `performance = clamp(50 + 8·(expectedFinish − position), 0, 100)`). |
| **Confidence (weighted total)** | `Σ round(wᵢ·factorᵢ)` with persona weights, clamped `[0,100]`; factor deltas **sum exactly** to the total (tested). |
| **Sack threshold** | Weekly weighted total ≤ `SackThresholdTotal = 25` and the club is human-managed → sacked (AI-managed clubs are scored but never sacked). |
| **Mandate negotiation** | Proposals within `±3` finish places / `±8` points and persona tolerance; worsens accepted only within tolerance. |
| **Supporter sentiment (board lens)** | `sentiment += 0.20·(performance − sentiment)`, clamped `[15, 95]`, persisted on `club.supporter_groups`. |

## 7. Offers & careers

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Offer status** | `proposed → accepted | declined | expired`. `expired` has **no producer today**. |
| **League gate** | Offers only for clubs with a `club_competitions role='league'` membership (`ErrClubNotInLeague`). |
| **Offer blocks** | Nested `league` / `board` / `squad` / `form` / `supporters` / `finance` — see [job-offers-and-decisions.md](job-offers-and-decisions.md). League `position` uses points with an **id tiebreak** (simpler than the standings' GD/GF/name ordering). |
| **Career reputation** | Append-only log (`manager.manager_reputation_events`); world total = `SUM(delta)`. Deltas: accept `+5`, sack `−10`, resign `0`, decline `0`. |
| **Career span** | `manager.manager_history` row opened on accept, closed on resign/sack; **append-only** (never deleted). |
| **Manager status** | `unemployed` / `active`; one active club per manager (partial unique indexes + advisory lock). |
| **Reputation** | Also seen on `club.clubs` (world reputation) — used by the board's `alternatives_score` (`50 + 5·worldReputation`, clamped `[10, 90]`). |

## 8. Morale & playing time

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Playing-time share** | `player_minutes / (90 × club_completed_matches)`, clamped `[0,1]`, current club only (resets to 0 on transfer). |
| **Satisfaction** | Per completed match: `ratio = share/expected`; ≥1 → satisfied target (0.65); ≥0.5 → neutral (0.50); <0.5 → unhappy (0.35, personality-adjusted). `development` always satisfied. |
| **Morale swing** | `next = current + α·(target − current)`, `α = 0.35 × vol × pro` (vol 1.0→1.5 by emotional volatility; pro 1.0→0.5 by professionalism), clamped `[0,1]`. |
| **Weekly recovery** | `next = current + 0.10·(0.5→2.0 by professionalism)·(0.5 − current)`, clamped `[0,1]`. |
| **Transfer-request trigger** | Weekly, human clubs only, all of: morale ≤ 0.35 (after recovery); `share < 0.5 × expected`; no open pending request; cooldown lapsed. Reasons: playing_time / wage / ambition / homesickness. |
| **Request lifecycle** | Approve → listed at market value (sentiment +15); Deny → morale −0.10, cooldown 28d, sentiment −25; Reassure → 28d promise, cooldown 28d, sentiment 0; Unaddressed → **auto-listed after 21 world days**. |
| **Promises** | `increase_playing_time`: `share ≥ expected` → fulfilled (+10); older than 4 weeks (`PromiseEvaluationWeeks`) → broken (−30), cooldown cleared. |
| **Fresh start** | On transfer: morale → 0.85, share → 0, cooldown cleared, open request → withdrawn. |
| **Relationship memory** | Player↔manager deltas: approved +15 / denied −25 / reassured 0 / promise kept +10 / broken −30 on the canonical `social.relationships` row. |

## 9. Training, tactics & condition

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Style** | Five Simple-Mode styles: `balanced`, `possession`, `gegenpress`, `low_block`, `direct`; each has allowed formations and an engine block (possession shift, chance volume, own/conceded conversion, card rate, stamina decay). |
| **Formations** | 9 slot orders (11 roster slots), e.g. `4-3-3` (default), `4-2-3-1`, `3-2-4-1`, `5-4-1`…; filled via position-family matching. |
| **Style efficacy** | `clamp(0.75 + 0.5 × tactical_familiarity, 0.75, 1.25)` scales the style block at kickoff. |
| **Training archetypes** | `technical`, `physical`, `defensive`, `attacking`, `recovery` — weekly per-player attribute deltas (e.g. attacking: `finishing +0.4`, `off_the_ball +0.3`, `pace +0.2`, `anticipation +0.2`, decay `marking −0.1`, `positioning −0.1`) with fatigue step, injury multiplier, and sharpness bonuses. |
| **Age scaling** | 16–21: positive deltas ×1.8; 22–29: ×1.0; 30+: mental ×1.2 + `pace`/`stamina −0.05`/wk unless physical plan. All attributes clamp `[1,100]`. |
| **Matchday coupling** | Team fitness = mean XI fitness (stamina tank seed); per-player factor `×= (0.9 + 0.4·sharpness) × (1 − 0.3·fatigue)`; familiarity → efficacy. |
| **Deadlines** | Lineup/tactics rejected `409` if the club's next fixture is live or world paused; training plans take effect from the **next week boundary** (`day % days_per_week == 0`; `effective_from_tick`). |

## 10. Transfers

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Market value** | `round10k(overall³ × 0.08 × positionMult × ageFactor × contractFactor)`, recomputed daily; `overall` is the mean attack/defence rating from the matchsim recipe. Position multipliers ~0.85–1.05; age peaks 24–28 (1.0); expiring deal (≤45 days left) ×0.5; contract factor capped 1.10. |
| **Listing status** | `active` (withdrawn closes it). **Listings never expire.** |
| **Bid statuses** | `pending → accepted | rejected | countered | withdrawn | expired`. |
| **Bid TTL** | `BidTTLWorldDays = 3` — expired by the daily sweep (`ExpireStale`) and lazily on respond. Withdrawing/completing cascades. |
| **AI seller** | `target = max(asking, valuation × 1.10)`; fee ≥ target accept; ≥ `0.90 × valuation` counter at target; else reject. |
| **AI buyer** | deterministic fee in `[0.75, 0.95] × valuation`, pay cap `0.95×`, funds guard `cash ≥ 1.25×fee`, top-2 shortlist, no bid when an open bid exists; counter reaction cap `1.10×`; `aiMaxRounds = 3`. |
| **Completion** | Atomic flip: ownership, seller contracts end, buyer contract, `BID_ACCEPTED`, `completed_transfers`, dedup-keyed ledger (debit `transfer_fee` / credit `player_sale`), clauses, history row, cascade close. |
| **Delegated seller** | Away manager's PolicyBot: fee ≥ `120% × valuation` accept; < `100%` reject; else counter at the accept threshold; `sell_floor`/`accept_above` are per-policy overrides. |

## 11. Social, trust & rivalries

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Trust score** | On-the-fly `SUM(delta)` of `social.trust_events` (no materialised score). Delivered deltas: fixture win `+5`, loss `−5`, draw 0. |
| **DM limits** | 2,000 chars (413 over), 30 msgs/min + 200/day (429 + Retry-After), HTML stripped. |
| **Rivalry strength** | `delta = (base + 2 × min(goal_difference, 5)) × big_match_multiplier`; base 8 (manager↔manager) or 10 (club↔club); ×2 big-match (same-country league); `+3` repeat bonus. Decay: >45 days untouched loses ~`1/5` on next touch. Clamped `[−100, 100]`. |
| **Manager rivalry edges** | Only when **both** clubs are human-managed; edges stored canonically (`a ≤ b`). |
| **Derby threshold** | `intensity ≥ RivalryIntensityThreshold (60)` unlocks engine derby mechanics (motivation floor, card, temperament divergence); intensity seeds at 80 for city/regional derbies. |

## 12. Academy

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Investment tier** | 1–5; annual cost `£250k–£4.8M`, debited monthly (`/12`, dedup key `academy:maintenance:<club>:<tick>`); youth weekly wage `£500–£2,500` by tier; 3-season youth contract. |
| **Club intake** | Per season, deterministic on `world+club+season`: cohort size 5–13 by tier; talent-class odds (Journeyman : TopProspect : Wonderkid : Generational) e.g. tier 5 `750:210:35:5`/1000; potential floor by class; quality offset `+tier×3` clamped `[−20,+20]`. Tier 1 never rolls a Generational. |
| **Street intake** | Country-level, cohort `[min, max]`; odds `975:22:3:0` — Wonderkid 3/1000, **Generational impossible**. |
| **Canonical OVR** | Every overall-driven read uses `squad.PositionalOverall` (never a stored composite). |

## 13. PolicyBot & absence

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Attended** | `last_activity_at ≥` the club's previous scheduled fixture kickoff (heartbeat is the auth request; 1-hour dedupe on persistence). |
| **Auto-away** | 3 consecutive unattended fixtures (`MissedFixtureThreshold`) → `away_since`, `away_auto = TRUE`. One attended fixture resets the streak. |
| **Delegation** | Always acts while the manager is away; a saved policy overrides defaults (squad `best_eleven`, tactics `keep`, transfers `accept_above 120 / sell_floor 100`, training `dna`). Training plan from ambition: ≥80 attacking, ≥60 physical, ≥40 technical, ≥20 defensive, else recovery. |
| **Delegated actor** | Per-world club-less "PolicyBot" manager that executes shared cores (`SetLineupForClub`, `SubmitPlanForClub`, `RespondToBidForClub`), audited as `policy_bot`. |

## 14. Dashboard

| Term | Definition / derivation | Source |
| --- | --- | --- |
| **Item priority** | `urgent | important | interesting`, aggregated read-only after each tick. | [dashboard-numerics.md](../design/dashboard-numerics.md). |
| **Thresholds** | contract expiry 30d; fixture 48h; board total ≤ 25 (urgent) / −15 drop since the last board review (important); morale ≤ 0.35; newest 5 market events; 12 items/section cap. |

## 15. Events & invariants

| Term | Definition | Source |
| --- | --- | --- |
| **world.events** | The append-only, world-scoped event log carrying `world_tick`, actor (`system`/`manager`/`policy_bot`), payload, and often an `Explanation`. Domain events e.g. `WORLD_TICK`, `SEASON_CREATED`, `BOARD_REVIEWED`, `MANAGER_SACKED`, `JOB_OFFER_ACCEPTED`, `BID_ACCEPTED`, `WAGE_POSTED`. |
| **Outbox (OPD-23)** | Event publication and the write it describes land in the **same transaction** — a business write can't happen without its event, and redelivery is idempotent (dedup keys, `ON CONFLICT DO NOTHING`, `FOR UPDATE` relocks). |
| **Explanation (OPD-12)** | Structured "why" (subject + factors) that **sum exactly** to the score it explains — rendered by clients, never recalculated (e.g. `board_confidence` factors sum to the weighted total; `monthly_wages` factors sum to the wage bill; `transfer_value` = valuation + fee). |

---

## Sources

Each row's authority (same numbers, richer rationale):

- World/time: [cadences-and-time.md](cadences-and-time.md), `internal/scheduler`, `internal/world`, `backend/internal/app/app.go`.
- Seasons/standings: [seasons.md](seasons.md), [derby-rivalry-determination.md](../design/derby-rivalry-determination.md).
- Board & job security: [board-numerics.md](../design/board-numerics.md).
- Finance: [finance-numerics.md](../design/finance-numerics.md).
- Morale/playing time: [morale-numerics.md](../design/morale-numerics.md).
- Tactics/training/condition: [tactics-training-numerics.md](../design/tactics-training-numerics.md).
- Transfers: [transfer-numerics.md](../design/transfer-numerics.md).
- Social/trust/rivalries: [social-numerics.md](../design/social-numerics.md).
- Academy: [academy-numerics.md](../design/academy-numerics.md).
- PolicyBot/absence: [policybot-numerics.md](../design/policybot-numerics.md).
- Dashboard: [dashboard-numerics.md](../design/dashboard-numerics.md).
- Offers/careers: [job-offers-and-decisions.md](job-offers-and-decisions.md), `internal/manager`, `internal/manager/offercontext.go`.