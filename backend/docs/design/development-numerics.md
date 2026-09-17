# Development Engine Numerics (S08-02)

Proposal numbers for the weekly player-development engine. All of these are
**data, not logic** (mirroring the training matrix in
`tactics-training-numerics.md` §2): the engine holds the arithmetic; this doc
owns the values and can be retuned without touching engine code.

## 1. Scope and shape

The training sweep (`internal/training`) remains the **single attribute writer**
— the development engine (`internal/development`) never writes skills directly.
It reads one player's weekly context and returns:

1. a per-key **growth multiplier** the sweep folds into its positive plan
   deltas (decay always stays ×1.0);
2. a possible hidden **potential expansion** (and/or ceiling lock) that the
   sweep persists to `player.player_hidden_traits`;
3. an auditable `explanation.Explanation` (PRD §54), stored on the weekly
   `DEVELOPMENT_WEEK` club event.

Age-curve base growth already lives in the training matrix §2.3
(`growthScale`: 16–21 → ×1.8, 22–29 → ×1.0, 30+ mental → ×1.2, others ×1.0)
and §2.3 veteran decay (`pace`/`stamina` −0.05/week at 30+, unless physical).
The development engine **compounds** on those, so a 19-year-old starter with a
good academy, professional habits and headroom grows fastest; a benched
veteran stagnates.

## 2. Current ability (the comparator to potential)

`Overall(skills, position)` = weighted means of the per-category catalogue,
renormalised over the categories the player actually possesses.

- Outfield: `technical 0.35, physical 0.20, mental 0.25, tactical 0.10, positional 0.10`
- Goalkeeper: `goalkeeping 0.55, mental 0.20, physical 0.10, tactical 0.10, positional 0.05`

## 3. Weekly growth multiplier

```
multiplier(key) =
    ageFactor(age, key)
  × minutesFactor(playing_time_pct, consecutive_stagnant_weeks)
  × disciplineFactor(professionalism)
  × facilityFactor(academy_level)
  × potentialFactor(potential, overall, expandedThisWeek)
```

### 3.1 Age curve `ageFactor`

| Band      | Mental keys | Other keys |
|-----------|-------------|------------|
| 16–20     | ×1.8        | ×1.6       |
| 21–29     | ×1.0        | ×1.0       |
| 30+       | ×1.15       | ×0.55      |

Veterans' physical decline is the existing training `veteranDecay`; their
technical/other growth slows to a crawl so the net curve trends down except
for their on-field psychology (mental keys keep rising modestly).

### 3.2 Playing time `minutesFactor`

```
0.5 + 1.0 × playing_time_pct        (0.5 … 1.5)
```

`playing_time_pct` is the whole-season share maintained by the weekly player
pass (S06-03). A player below 20% for **≥4 consecutive weeks** is *stagnating*
and the factor drops to `×0.85` on top.

### 3.3 Discipline `disciplineFactor`

```
0.75 + 0.55 × professionalism / 100        (0.75 … 1.30, 50-neutral ≈ 1.03)
```

### 3.4 Facility `facilityFactor`

Composite academy/training level `L` ∈ [1,10], neutral 5:

```
0.80 + 0.04 × L        (1 → 0.84, 5 → 1.00, 10 → 1.20)
```

Resolved in the sweep from `club.academies.coaching_level` and the
`club.facilities` `training_ground`/`youth_facility` rows; 5 when nothing is
on disk.

### 3.5 Potential ceiling `potentialFactor`

- **No ceiling data** (legacy player without a `player_hidden_traits` row):
  `×1.0` — unmanaged, grows at plan rate forever.
- **Headroom** `= potential − overall`: **> 2 points** → `×1.0`; **≤ 2
  points** (reached the ceiling) → `×0.25` *trickle*.
- **Ceiling flexed this week**: `×1.2` — the fresh headroom window reopens.

## 4. Hidden dynamic potential (S08-02 core)

The ceiling lives only in `player.player_hidden_traits`; it is **never
surfaced in squad read models**.

### 4.1 Flex expansion

Triggers when **all** hold (per appraisal week):

- player is ≤ 26 years old;
- ceiling exists and is **not** locked;
- headroom ≤ 2 (ability has caught the ceiling);
- **elite form**: `season_avg_rating ≥ 7.0` **and** `playing_time_pct ≥ 0.4`;
- expansion budget remains (`player_development.potential_expansions_remaining > 0`,
  lifetime cap **3**, yours to spend).

Bump (deterministic): `+1` base; `+2` when the recent rating ≥ 9.0; an extra
`+1` for players ≤ 20. Capped at 100. Consuming the **last** budget point locks
the ceiling this week (`potential_locked_week`).

`season_avg_rating` = mean `rating` over the player's last 10 rated
appearances (0 when unanswered).

### 4.2 Lock

- Spending the last expansion,
- or turning 27 the ceiling locks permanently (`potential_ceiling_locked`).

After locking, no further flex; the §3.5 trickle governs.

## 5. Stagnation

`consecutive_stagnant_weeks` increments when `playing_time_pct < 0.20`,
resets to 0 once minutes return, caps at 12. The §3.2 ×0.85 atrophy engages at
≥4 and the weekly explanation reports it. `playing_time_pct` is cumulative,
so *season* bench-warming (never entered the club's minutes denominator) is
correctly flagged once the counter passes 4 — early-season, before the first
match days, the counter holds for everyone but the multiplier only bites when
it crosses 4 and self-corrects.

## 6. Persistence

- `player.player_development` upserted per player per applied week
  (idempotent: rides inside the club's `last_applied_week` training
  transaction; the seeded reads are deterministic so redelivery cannot drift).
- `player.player_hidden_traits.potential`/
  `potential_ceiling_locked` updated only on change/lock.
- One `DEVELOPMENT_WEEK` system event per club per week carrying that club's
  per-player `explanation.Explanation` array (subject `player_development`,
  narrative score 0; the numeric attribution is the
  `player_attribute_changes` rows the sweep already writes).

## 7. Intended emergent behaviour

- A 19-year-old on an attacking plan at a level-8 academy, 80% minutes,
  professionalism 90, headroom 5 pts: multiplier ≈ `1.6 × 1.3 × 1.25 ×
  1.12 × 1.0 ≈ 2.9` → most growth weeks land +2 points instead of +1.
- A 24-year-old rotation piece (25% minutes, pro 45, level 3 academy): ≈
  `1.0 × 0.75 × 0.99 × 0.92 ≈ 0.68` → growth stutters.
- A 32-year-old unrestricted on recovery: mental keys ≈ `1.15 × (0.5+p) ×
  1.0 × 1.0 ≈ 0.9×`, others ≈ `0.43×` plus the §2.3 veteran `pace/stamina`
  decay → net physical decline, steady mental floor.
- A wonderkid who maxes out early and keeps starring sees "+2 potential —
  elite form" scout flashes, up to 3 times, then the ceiling locks.