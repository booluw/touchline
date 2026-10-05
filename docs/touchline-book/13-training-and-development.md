# Chapter 13 — Training and player development

Two packages cooperate on every week boundary:

- **`internal/training`** — what a plan *provides*: per-key attribute deltas
  and condition changes. It is the **single writer** of attribute values.
- **`internal/development`** — what a player *becomes*: a pure engine that
  returns per-key growth multipliers, potential flex/lock decisions and an
  explanation, which the training sweep folds in.

Both run inside `Training.ApplyWeekly` on `day % days_per_week == 0`
([Ch. 4](04-world-clock-and-time.md)), after PolicyBot's `EnsureTraining`.

## 13.1 Training plans (S05-01)

One active plan per club (`club.club_training_plans`), one of five archetypes,
effective from the **next** week boundary.

| Archetype | Growth / week / player | Decay / week | Fatigue step | Injury mult | Sharpness |
| --- | --- | --- | --- | --- | --- |
| `technical` | passing +0.3, vision +0.2, first_touch +0.3, composure +0.2 | strength −0.1, tackling −0.1 | 0.80× | 0.70 | +5 pts mids/wingers |
| `physical` | stamina +0.4, natural_fitness +0.3, strength +0.3, work_rate +0.2 | composure −0.1 | 1.40× | 1.35 | +2 pts |
| `defensive` | positioning +0.4, tackling +0.3, marking +0.3, concentration +0.2, teamwork +0.2 | off_the_ball −0.1 | 1.00× | 0.85 | +3 pts |
| `attacking` | finishing +0.4, off_the_ball +0.3, pace +0.2, anticipation +0.2 | marking −0.1, positioning −0.1 | 1.20× | 1.10 | +6 pts strikers |
| `recovery` | decision_making +0.1; tactical familiarity +0.05 | physical decay after 3+ consecutive recovery weeks | −0.40 (removal) | 0.20 | −2 pts |

### Training age scaling

- 16–21: positive deltas ×1.8 (decay ×1.0)
- 22–29: ×1.0
- 30+: mental growth ×1.2; unless on `physical`, veterans also lose `pace` and
  `stamina` −0.05/week.

All attributes clamp `[1, 100]`. Each week emits `TRAINING_WEEK` per club and
journals changes in `player.player_attribute_changes`.

## 13.2 The development multiplier (S08-02)

For each positive plan delta:

```
multiplier(key) = ageFactor(age, key)
                × minutesFactor(playing_time_pct, consecutive_stagnant_weeks)
                × disciplineFactor(professionalism)
                × facilityFactor(level)
                × potentialFactor(potential, overall, expandedThisWeek)
```

Decay is never multiplied (stays ×1.0).

| Factor | Formula | Range |
| --- | --- | --- |
| **Age** | mental keys: 16–20 ×1.8, 21–29 ×1.0, 30+ ×1.15; other keys: 16–20 ×1.6, 21–29 ×1.0, 30+ ×0.55 | |
| **Minutes** | `0.5 + 1.0 × playing_time_pct`; × 0.85 more when stagnating (≥ 4 consecutive weeks under 20% share) | 0.5–1.5 |
| **Discipline** | `0.75 + 0.55 × professionalism / 100` | 0.75–1.30 |
| **Facility** | `0.80 + 0.04 × L`, L ∈ [1,10] from `club.academies.coaching_level` and `club.facilities` (`training_ground`, `youth_facility`); 5 when absent | 0.84–1.20 |
| **Potential** | no ceiling data → 1.0; headroom > 2 → 1.0; headroom ≤ 2 → **0.25** trickle; flexed this week → 1.2 | |

`playing_time_pct` is the whole-season share from the weekly player pass
([Ch. 18](18-morale-and-transfer-requests.md)). The stagnation counter
increments while share < 0.20, resets when minutes return, caps at 12.

Worked examples (from the numerics ledger):

- 19-year-old, attacking plan, level-8 facility, 80% minutes, pro 90,
  headroom 5: ≈ 1.6 × 1.3 × 1.25 × 1.12 × 1.0 ≈ **2.9×**.
- 24-year-old rotation player, 25% minutes, pro 45, level 3: ≈ **0.68×**.
- 32-year-old on recovery: mental ≈ 0.9×, others ≈ 0.43× plus veteran decay.

## 13.3 Hidden dynamic potential

`potential` lives only in `player.player_hidden_traits` and is never shown.

**Flex expansion** — the ceiling rises when **all** hold in a week:

- age ≤ 26 and ceiling not locked;
- headroom ≤ 2 (ability has caught the ceiling);
- elite form: `season_avg_rating ≥ 7.0` (mean of last 10 rated appearances)
  **and** `playing_time_pct ≥ 0.4`;
- expansion budget left (`potential_expansions_remaining`, lifetime **3**).

Bump: +1; +2 when the rating ≥ 9.0; +1 extra for players ≤ 20; capped at 100.

**Lock** — the ceiling stops flexing permanently when the last expansion is
spent or the player turns **27** (`LockAge`). After that the 0.25 trickle
governs.

## 13.4 Condition updates

Training also moves `player.player_condition`
([Ch. 14](14-condition-and-injuries.md)):

| Column | Weekly rule |
| --- | --- |
| fatigue | `+0.05 × FatigueStep` (recovery: −0.20) |
| fitness | `+0.05 − 0.05 × FatigueStep` |
| sharpness | `+0.02 + SharpnessBonus` |
| injury_risk | `+0.05 × (InjuryMult − 1) × (1 − risk)` |
| tactical_familiarity | +0.02 (+0.05 on recovery) |

All clamp `[0, 1]`. Training can itself cause injuries
([Ch. 14](14-condition-and-injuries.md) §14.4).

## 13.5 Persistence and events

- `player.player_development` upserted per player per applied week, inside the
  club's `last_applied_week` transaction (idempotent under redelivery).
- `potential` / `potential_ceiling_locked` written only on change.
- One `DEVELOPMENT_WEEK` event per club per week with each player's
  explanation (narrative, score 0; the numbers are the attribute-change rows).
- Reads: `GET /api/clubs/:id/players/:pid/development`.

## 13.6 Planned

IM36 (*not started*): make hidden attributes trainable. Individual training and
development programmes from PRD §26 are not built.

## Connections

- Plans chosen by PolicyBot from DNA: [Chapter 25](25-policybot-and-absence.md).
- Minutes feeding development: [Chapter 18](18-morale-and-transfer-requests.md).
- Code: `internal/training/{deltas,dev_pass}.go`, `internal/development/development.go`.
- Source: `docs/design/tactics-training-numerics.md` §2, `backend/docs/design/development-numerics.md`.

---
[← Squad & tactics](12-squad-tactics-lineups.md) · [Contents](the-touchline-book.md) · [Next: Condition & injuries →](14-condition-and-injuries.md)
