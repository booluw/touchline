# Chapter 14 — Condition and injuries

## 14.1 Condition (`player.player_condition`)

Five numbers in `[0, 1]` per player, seeded at squad creation (lazy defaults
for older players):

| Column | Start | Moves with |
| --- | --- | --- |
| `fatigue` | 0 | training load ([Ch. 13](13-training-and-development.md)) |
| `fitness` | 1.0 | training load; seeds the engine's stamina tank |
| `sharpness` | 0.5 | training archetype bonus |
| `injury_risk` | `injury_susceptibility / 200` | training injury multiplier |
| `tactical_familiarity` | 0.5 | weekly training; drives style efficacy ([Ch. 12](12-squad-tactics-lineups.md)) |

### Matchday coupling

When a team is built ([Ch. 16](16-matchday-and-live-matches.md)):

- `Team.Fitness` = mean XI fitness.
- Each player's contribution `×= (0.9 + 0.4 × sharpness) × (1 − 0.3 × fatigue)`.
- Familiarity → style efficacy.

## 14.2 The injury engine (S08-03)

`internal/injury` is **pure and deterministic**. Responsibility is split:

- **Who** is injured: the match engine's existing `injury` events (its frozen
  attribution stream) — or the weekly training check.
- **What** the injury is: this engine, from a **separate** stream, so the match
  replay digest is never touched.

### Type and severity

Types (match `player.injuries.injury_type`): `muscle`, `ligament`, `bone`,
`concussion`, `illness`, `recurring`.

Base severity is a seeded draw in `[2, 9]`, plus additive steps, clamped `1..10`:

| Condition | Step |
| --- | --- |
| effective fatigue ≥ 0.75 / ≥ 0.90 | +1 / +1 more |
| injury susceptibility ≥ 60 / ≥ 80 | +1 / +1 more |
| recurrence history load ≥ 0.30 / ≥ 0.60 | +1 / +1 more |
| foul / contact | +1 |

`effective fatigue = clamp01(fatigue + 0.001 × minutes_played)`. A recurrence
load ≥ 0.60 forces type `recurring`.

### Recovery days

| Severity | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Days | 3 | 7 | 14 | 21 | 30 | 45 | 60 | 90 | 120 | 180 |

Type multipliers: muscle 1.0, ligament 1.5, bone 2.0, concussion 0.5, illness
0.4, recurring 1.3.

```
medical_factor = max(1 − 0.05 × (medical_level − 1), 0.55)     // club.facilities 'medical', default 5
days_out = round(severity_days × type_mult × medical_factor × (1 + 0.15 × (2u − 1)))   // ±15%, ≥1
expected_recovery_date = occurred_at + days_out
```

Medical staff shorten recovery; they never reduce severity.

### Recurrence risk

```
recurrence = type_base (muscle .15, ligament .25, bone .12, concussion .10, illness .05, recurring .60)
           + 0.20 × severity / 10
           + 0.25 × clamp01(prior_recurrence_load)
           − 0.035 × (medical_level − 1)                 → clamp [0, 1]
```

Stored on `player.injuries.recurrence_risk` and fed into the next injury as
`RecurrenceLoad`.

### Rushed return

`rush-return` ends an injury early and stamps
`recurrence = max(recurrence, 0.65)` (`RushRecurrenceFloor`), so the next injury
is likelier to be severe and `recurring`. Event `PLAYER_RUSHED_RETURN`.

## 14.3 Weekly recovery scan and setbacks

- Once an open injury passes **55%** of its planned recovery, each weekly scan
  rolls a **35%** setback chance; a hit adds **3–7 days** to
  `expected_recovery_date` and emits `INJURY_UPDATE`. Keyed
  `(injury_id, week)` so redelivery can't double-apply.
- Recovery fires once the (post-setback) date is reached:
  `actual_recovery_date` stamped, `PLAYER_RECOVERED`, `player_history`
  `injury_return`.

Recovery clocks run on real days, which per OPD-57 equal world days.

## 14.4 Training injuries

Weekly, per player:

```
P = 0.004 + 0.08 × injury_risk + (risk > 0 ? 0.05 × intensity : 0)
```

`intensity` = plan volume in attribute points / 0.20 (saturating at 1.0). The
roll uses `WeekStream(tick, player, "training")`.

## 14.5 Selection blocking

`squad.MatchEligible` marks a player unavailable while an open injury's
`expected_recovery_date ≥ matchday`. Human lineups with an unavailable slot are
rejected; AI and bench selection filter them out.

## 14.6 Events

| Event | Payload highlights |
| --- | --- |
| `PLAYER_INJURED` | type, severity, expected date, recurrence (+ resolved factors) |
| `PLAYER_RECOVERED` | injury id, recovery date |
| `INJURY_UPDATE` | `kind: setback`, days |
| `PLAYER_RUSHED_RETURN` | dates, recurrence |

Pitch condition is a documented neutral factor (no stadium state yet).

## Connections

- Engine-side injury events: [Chapter 15](15-match-engine.md).
- Medical facility levels: [Chapter 9](09-clubs-dna-supporters.md).
- Code: `internal/injury/{evaluate,store,stream}.go`.
- Source: `backend/docs/design/injury-numerics.md`, `docs/design/tactics-training-numerics.md` §2.2.

---
[← Training](13-training-and-development.md) · [Contents](the-touchline-book.md) · [Next: Match engine →](15-match-engine.md)
