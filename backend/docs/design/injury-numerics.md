# Injury numerics (S08-03, OPD-03 injury probability curve)

The injury engine mirrors the other tuning surfaces in this codebase: every
number below is a **proposal constant** living in `internal/injury` and
recalibration is meant to be a **data-only change** (constants are referenced,
never re-derived, by callers). The engine itself is pure — no I/O, no wall
clock — so a redelivered match or weekly tick reproduces the exact same
outcome. The matchsim **replay contract is never touched**: matchsim still
decides *who* gets injured (its frozen v1.6 attribution of the existing
`injury` feed events); this engine decides *what* (type, severity, duration,
recurrence) from a **separate** deterministic stream.

## Sources

- PRD §27 (injury types, severity, expected recovery, uncertainty, recurrence
  risk, medical staff quality, rushed recovery, setbacks).
- S08-03 task AC: deterministic match triggers from match seed + player fatigue
  + physical tackle interactions; medical-staff quality reduces duration and
  recurrence; injured players blocked from selection; rushed return carries
  high recurrence risk; `PLAYER_INJURED` / `PLAYER_RECOVERED` events carry full
  `Explanation` context.

## Injury type and severity

Types match the `player.injuries.injury_type` CHECK constraint:
`muscle | ligament | bone | concussion | illness | recurring`.

Base (no-modifier) severity is a seeded draw in `[2, 9]` (`SeverityDrawMin`,
`SeverityDrawSpan`), clamped to `1..10`. Severity steps are additive:

| Condition | Step |
|---|---|
| effective fatigue ≥ 0.90 (2nd step) | +1 |
| effective fatigue ≥ 0.75 | +1 |
| injury susceptibility ≥ 80 (2nd step) | +1 |
| injury susceptibility ≥ 60 | +1 |
| recurrence history load ≥ 0.60 (2nd step) | +1 |
| recurrence history load ≥ 0.30 | +1 |
| foul/contact (FoulContext) | +1 |

`effective fatigue = clamp01(fatigue + 0.001 × minutes_played)` — minutes fold
the "congestion / late-match wear" factor into the stored fatigue without
needing a separate input.

A recurrence-history load ≥ 0.60 (`RecurringMinLoad`) forces the type to
`recurring` outright, expressing repeat-injury risk.

## Recovery duration

Base recovery days come from the severity bucket scaled by a per-type
multiplier:

| severity | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
|---|---|---|---|---|---|---|---|---|---|---|
| days | 3 | 7 | 14 | 21 | 30 | 45 | 60 | 90 | 120 | 180 |

| type | multiplier |
|---|---|
| muscle | 1.0 |
| ligament | 1.5 |
| bone | 2.0 |
| concussion | 0.5 |
| illness | 0.4 |
| recurring | 1.3 |

Examples: a `muscle` severity-3 strain ≈ 14 days; a `ligament` severity-9 tear
(ACL) ≈ 120 × 1.5 = 180 days ≈ 6 months.

### Medical staff quality

`club.facilities` rows with `facility_type = 'medical'` supply level `1..10`
(default **5** when a club has no row — an `ensure`-on-demand insert keeps a
palpable neutral). Level enters twice:

```
medical_recovery_factor = max(1 − 0.05 × (level − 1), 0.55)
days_out = round(severity_days[sev] × type_multiplier × medical_recovery_factor × uncertainty)
```

Level 1 keeps the full duration (factor 1.0); level 10 cuts recovery to 55%.
Medical staff reduce severity neither in storage nor in narrative — the
damage is the damage — only recovery time.

### Uncertainty (PRD §27)

Every injury has **uncertainty**: recovery days are spread ± 15%
(`UncertaintySpread`) by a seeded draw:

```
days_out = round(base × (1 + 0.15 × (2·u − 1)))   // u = seeded U(0,1), floored at 1
```

`expected_recovery_date = occurred_at + days_out`. **Setbacks** (below) push the
expected date later on top of this spread.

## Recurrence risk

Stored in `player.injuries.recurrence_risk` (a `NUMERIC` in `[0,1]`) and read
by the next `Evaluate` as `RecurrenceLoad`. It builds from:

```
recurrence = type_base        (muscle .15, ligament .25, bone .12,
                               concussion .10, illness .05, recurring .60)
          + 0.20 × severity / 10
          + 0.25 × clamp01(prior_recurrence_load)
          − 0.035 × (medical_level − 1)
```

Clamped to `[0,1]`. A good medical team simultaneously shortens recovery and
lowers the chance of a repeat.

### Rushed return

Ending an injury early (manager `rush-return`) stamps its recurrence risk to
**at least 0.65**:

```
rushed_recurrence = max(recurrence, 0.65)   // RushRecurrenceFloor
```

That stored value feeds the next `Evaluate`'s `RecurrenceLoad`, so rushing has
actual consequences: the next injury is more severe, more likely to be
`recurring`, and more likely to recur again.

## Setbacks (weekly recovery scan)

While an injury is open and past `SetbackWindowStart` (55%) of its planned
recuperation, each weekly scan rolls once:

```
setback_days = Setback(SetbackStream(injury_id, week_tick))   // 0 = none
```

- Rate `SetbackRate` = 0.35 per week; when it hits, `days ∈ [3, 7]`.
- Idempotency: `injury_setbacks` is keyed `(injury_id, week)`; a redelivered
  weekly tick cannot apply the same setback twice (the scan only adds when the
  key is absent).
- On a hit, `player.injuries.expected_recovery_date += setback_days` and an
  `INJURY_UPDATE` event carries the explanation. Recovery only fires once the
  (post-setback) expected date is reached; `actual_recovery_date` then stamps
  the row and `PLAYER_RECOVERED` fires.

## Determinism and stream separation

- Match injuries: `MatchStream(match_seed, player_id)` =
  `fnv64a("injury-match:" + seed + ":" + player_id)`. Each injured player gets
  an independent stream; no draw touches matchsim's canonical stream, so the
  v1.6 golden digest is byte-identical (verified by the matchsim replay tests).
- Training injuries: `WeekStream(week, player_id, "training")`.
- Setbacks: `SetbackStream(injury_id, week)`.
- Recovery eligibility is snapshot-derived (`actual_recovery_date IS NULL` and
  `expected_recovery_date ≤ today`), so scans are idempotent under at-least-once
  delivery.

## Selection blocking

`squad.MatchEligible` already returns `Available = false` while an open injury's
`expected_recovery_date ≥ matchday`. The human lineup command now rejects any
`!Available` slot (hard error, nothing written); AI squads and bench selection
filter on `Available` as before.

## Pitch condition

The PRD lists pitch condition among injury factors, but no surface tracks
pitch/stadium condition yet (`club.facilities` `stadium` rows exist without
state). Pitch is therefore a documented **neutral (1.0)** factor until stadium
condition data lands; the engine stays free of dead inputs.

## Worked examples

1. **Congested run, poor medical.** Fatigue 0.85, susceptibility 40, medical 2,
   minutes 88, foul context — seed S.
   - effective fatigue = 0.85 + 0.088 → 0.938 → +2 severity.
   - foul → +1. Base draw e.g. 4 → severity 7.
   - muscle: 60 × 1.0 × (1 − 0.05) ≈ 57 days, ±15% → 49–66 days.
   - recurrence ≈ 0.15 + 0.20×0.7 + 0 = 0.29 − 0.035 → 0.26.
2. **Fresh legs, elite medical.** Fatigue 0.10, susceptibility 30, medical 9,
   minutes 55, no contact — seed S′.
   - no severity steps; base draw 3 → severity 3.
   - muscle: 14 × 1.0 × (1 − 0.40) ≈ 8 days, ±15% → 7–10 days.
   - recurrence ≈ 0.15 + 0.06 − 0.28 → ~0.0.
3. **Rushed ACL.** A ligament sev-8 tear (90 × 1.5 = 135 days) is ended early:
   stored recurrence jumps to ≥ 0.65 and the next `Evaluate` receives it as
   `RecurrenceLoad`, adding severity steps and probably flipping the type to
   `recurring` (load ≥ 0.60).

## Event contract

| Event | Payload (JSON `world.events.payload`) | Explanation |
|---|---|---|
| `PLAYER_INJURED` | `{player_id, club_id?, match_id?, injury_type, severity, expected_recovery_date, recurrence_risk}` | resolved `Evaluate` factors |
| `PLAYER_RECOVERED` | `{player_id, injury_id, recovery_date}` | recovery summary |
| `INJURY_UPDATE` | `{player_id, injury_id, kind: "setback", days}` | setback trigger |
| `PLAYER_RUSHED_RETURN` | `{player_id, injury_id, expected_recovery_date, actual_recovery_date, recurrence_risk}` | rush decision |

`player.player_history.event_type = 'injury_return'` is written on recovery
alongside `PLAYER_RECOVERED`. Explanations are persisted with the event rows and
never recomputed (PRD §54).