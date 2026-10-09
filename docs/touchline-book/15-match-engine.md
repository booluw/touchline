# Chapter 15 — The match engine (matchsim)

`pkg/matchsim` is a **pure, seeded, deterministic** match engine: no database,
no network, no wall clock. Identical `(seed, teams, tuning, live inputs)`
produce **byte-identical** results. Persistence, live pacing and result
application live one layer up ([Chapter 16](16-matchday-and-live-matches.md)).

> **Why so strict?** Competitive fairness and anti-cheat (Tech Plan §1.4,
> PRD §68): any match can be re-simulated from its seed and inputs to prove the
> result. A golden replay digest (seed 424242) is pinned in
> `simulate_test.go`; any structural or numeric change must bump
> `EngineVersion` and re-pin it.

Current `EngineVersion`: **`1.6-proposal`** (recorded on every completed
`match.matches.engine_version`).

## 15.1 Spec lineage

The authoritative spec is the versioned addenda in `pkg/matchsim/`, read in
order; later wins where they overlap:

| Addendum | Adds |
| --- | --- |
| v1.1 | two-sided Attack/Defense, possession model, form/morale/motivation factors, tuning versioning |
| v1.2 | approved baseline numbers, marginal referee events, penalty model, variance band |
| v1.3 | canonical replay draw order, red-card/substitution effects |
| v1.4 | **approved** Parts 4–6: final numbers, draw order, penalty/referee scopes (`1.2-approved`) |
| v1.5 | five tactical styles, per-side stamina + post-75' fatigue, live `tactic_change` with a real effect |
| v1.6 | per-player **attribution** pass and **1–10 player ratings** (proposal) |

## 15.2 Inputs

Per side, a `Team`:

- **Attack** and **Defense** ratings (1–100) built from the XI by
  `squad.BuildSquadRatings` ([Ch. 16](16-matchday-and-live-matches.md)).
- **FormFactor** ([Ch. 17](17-form.md)), **Morale** (0.90–1.10),
  **Motivation** (0.85–1.20).
- **Aggression**, **RivalryIntensity** (cards).
- **Style** + efficacy ([Ch. 12](12-squad-tactics-lineups.md)), **Fitness**
  (stamina tank seed).
- Penalty taker conversion (0.65–0.88 around the 78% baseline).
- Optional **Lineups** `{XI, Bench, Taker}` enabling v1.6 attribution.

Plus ordered **LiveInputs** (substitutions, `{"style": …}` changes) and the
`Tuning` block.

## 15.3 The simulation, minute by minute

1. **Kickoff adjustments.** Home side's raw ratings × **1.08**
   (`HomeAdvantageFactor`, approved). Then form, morale, motivation and one
   **triangular variance** draw per team in `[0.85, 1.15]` centred 1.0 (home
   first).
2. **Each minute** (canonical draw order — the replay contract):
   1. **Possession**: `share = p_home^e / (p_home^e + p_away^e)`, `e = 3.0`,
      where `p = (Attack + Defense)/2` after modifiers, shifted by style.
   2. **Chance?** Baseline **13** chances per team per 90' vs an equal
      opponent, scaled by style chance volume.
   3. **Outcome** from weights goal 1 / on-target 2 / off-target 4.5 /
      blocked 2 / foul 0.5. The goal weight scales with
      `(attAttack/defDefense)^0.6`, clamped `[0.2, 3.0]`, and by own/conceded
      conversion style modifiers.
   4. An **in-box foul** (10% of chance-table fouls) → referee penalty decision
      → conversion (taker or 78%).
   5. On-target shots are surfaced as `chance_created` feed events (50%).
   6. **Defensive fouls** for the non-possession side: base **12** per 90',
      scaled by `Aggression × RivalryIntensity`.
   7. **Cards couple to fouls** (no independent card draw): P(yellow | foul)
      0.15, P(red | foul) 0.008, scaled `(0.5 + Aggression × 0.005)` and
      `(1 + Rivalry × 0.005)`. Draws within 0.02 of a boundary are
      "borderline" and resolved by the referee (noise 0.03, small home-crowd
      bias 0.01).
   8. **Injury on foul** fraction 0.02 ([Ch. 14](14-condition-and-injuries.md)).
   9. **Substitutions** at windows 60' and 75' (auto 85% of the time) or when a
      LiveInput says so; a sub restores the tank to `SubFitness 0.5` and boosts
      stamina 0.05.
3. **Half/full time** are synthesised at 45/90 after that minute's draws.

### Red cards and fatigue

- A red card degrades the side's Defense by 0.15 and Attack by 0.25.
- **Post-75' fatigue** (v1.5): from minute 76, if a side's stamina tank lags
  the healthy norm,
  `deficit = max((90 − minute)/90 − stamina, 0)`,
  `eff = 1 − clamp(deficit × 0.5, 0, 0.15)`. Style stamina decay feeds the tank.

### Tactic changes

A live `{"style": key}` input switches that side's style for the rest of the
match. It **re-weights existing draws** and never consumes new RNG, so the
draw order is preserved; `balanced` is the identity block.

### Golden goal

For knockout fixtures level at 90', the engine continues minute by minute in
sudden-death and stops at the first goal (bounded loop). See
[Chapter 8](08-cups.md) §8.4.

## 15.4 Attribution and player ratings (v1.6)

After the canonical pass, a **post-pass** links events to players using an
**independent per-side stream** seeded
`matchSeed ⊕ fnv64a(clubUUID) ⊕ castTag`:

- Goals pick a scorer weighted by attribute weight (attackers boosted ×1.6);
  assists (70% of goals) pick another player; subs link the exact players the
  manager chose (`resolveForced`) or the best bench player.
- Each appearance gets minutes, goals, assists and a **1–10 rating**:

```
rating = Base 6.0 + 0.6·goals + 0.3·assists + 0.05·chances
       − 0.3·yellow − 1.0·red + 0.4·pens scored − 0.6·pens missed
(prorated toward 6.0 for < 45 minutes), rounded, clamped 1..10
```

Sides without lineups are skipped (blank player ids), so lineup-less golden
replays are untouched. These ratings feed `player_appearances.rating`, the
career record ([Ch. 10](10-players.md)) and development's
`season_avg_rating` ([Ch. 13](13-training-and-development.md)).

> Do not confuse this **player** 1–10 rating with the **board's** 0–100
> per-match **manager** rating ([Chapter 23](23-board-and-job-security.md)).

## 15.5 Output and persistence

The engine returns score, possession and a feed of `MatchEvent`s whose types
are an exact subset of the `match.match_events` CHECK constraint. Commentary is
stored as a **template with placeholders** (`{player}`, `{assist}`, `{sub}`) so
replays are byte-identical; the API resolves names at read time
([Ch. 16](16-matchday-and-live-matches.md) §16.5).

**Expected goals (IM58).** Every chance adds its exact goal probability —
the `goalW / total` weight `resolveChance` draws against — to the attacking
side's xG; an awarded penalty adds its conversion rate. No extra draw is
taken, so the golden replay digest is unchanged and a replayed match gives the
same xG. `MatchResult.home_xg/away_xg` (2 dp) are stored on `match.matches`
(migration 0061) by both the instant and the live finalize paths and served
as `stats.home.xg/away.xg` on the match events read. Matches played before
IM58 carry `null`; they are never backfilled. Calibration check: over 4000
seeds mean xG ≈ mean goals (1.39 vs 1.36).

## 15.6 Tuning block

All numbers live in `pkg/matchsim/tuning.go` (`ProposedTuning`,
`DefaultTuning()` returns a safe copy). Fields are marked *approved* (PM-signed)
or *proposal*. Recalibration = data change + `EngineVersion` bump + re-pinned
digest.

| Field | Value |
| --- | --- |
| PossessionExponent | 3.0 |
| ChancesPerMatchMin | 13 |
| GoalWeightBase / AbilityScale | 1.0 / 0.6 |
| Goal multiplier band | 0.2 – 3.0 |
| HomeAdvantageFactor | 1.08 (approved) |
| Variance band | 0.85 – 1.15 |
| Referee noise / bias / source | 0.03 / 0.01 / `home_crowd` |
| PenaltyConversionBase | 0.78 (approved) |
| PenaltyInBoxFraction | 0.10 |
| Fouls per 90 / P(yellow) / P(red) | 12 / 0.15 / 0.008 |
| Red degrade (def / att) | 0.15 / 0.25 |
| Sub windows / auto fraction | 60, 75 / 0.85 |
| Fatigue start / scale / max | 76' / 0.5 / 0.15 |
| AssistFraction | 0.7 |
| LivePacingSecondsPerMinute | 20 s (overridden by `tick.match_cadence`) |

## 15.7 Rules for engine changes

1. Never consume RNG from the canonical stream in a new feature — derive a new
   stream (as injuries and attribution do).
2. Any structural or numeric change bumps `EngineVersion` and re-pins the
   golden digest.
3. New event types must be added to the `match_events` CHECK constraint
   (migration) first.
4. Keep `pkg/matchsim` free of I/O.

IM34 (match pitch simulation) is being planned on branch `feat/simulation` and
is not part of `main`.

## Connections

- Building the teams: [Chapter 16](16-matchday-and-live-matches.md).
- Styles: [Chapter 12](12-squad-tactics-lineups.md); form: [Chapter 17](17-form.md).
- Source: `pkg/matchsim/README.md` and addenda v1.1–v1.6, OPD-21.

---
[← Condition & injuries](14-condition-and-injuries.md) · [Contents](the-touchline-book.md) · [Next: Matchday →](16-matchday-and-live-matches.md)
