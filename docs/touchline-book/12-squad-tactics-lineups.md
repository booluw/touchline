# Chapter 12 — Squad, tactics and lineups

Simple Mode gives the manager three levers before a match: **who plays**
(lineup), **how they play** (style + formation), and **in-match changes**
(substitutions and style switches).

## 12.1 Tactical styles (OPD-25)

The manager picks **one of five styles**. Formation is an optional refinement
inside the style's allowed set (the first is the default).

| Style | Name | Allowed formations (default first) |
| --- | --- | --- |
| `balanced` | Balanced | 4-3-3, 4-4-2, 4-2-3-1, 5-3-2 |
| `possession` | Possession Control | 4-3-3, 3-2-4-1 |
| `gegenpress` | Gegenpress / High-Press | 4-3-3, 4-2-3-1 |
| `low_block` | Low-Block / Counter | 5-4-1, 4-5-1 |
| `direct` | Direct / Long-Ball | 4-4-2, 3-5-2 |

### Engine block per style

Consumed by `matchsim.StyleSpec` ([Ch. 15](15-match-engine.md)):

| Style | Possession shift | Chance volume | Goal conv. | Conceded conv. | Card rate | Stamina decay |
| --- | --- | --- | --- | --- | --- | --- |
| balanced | 0 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| possession | +0.20 | 0.90 | 1.20 | 1.20 | 0.80 | 1.00 |
| gegenpress | +0.125 | 1.25 | 1.40 | 1.80 | 1.40 | 1.35 |
| low_block | −0.175 | 0.70 | 2.20 | 0.60 | 1.15 | 0.85 |
| direct | −0.075 | 1.15 | 0.80 | 1.00 | 1.10 | 1.05 |

### Rating profiles per style

Each style also skews the pre-match team rating recipe (category multipliers
over `DefaultPositionWeights`):

- **possession** — Technical ×1.10, Mental ×1.10, Physical ×0.92 (both sides).
- **gegenpress** — attack Physical ×1.12, Tactical ×1.10; defence Physical ×1.05.
- **low_block** — defence Tactical ×1.12, Physical ×1.05; attack Physical ×1.08.
- **direct** — attack Physical ×1.10, Technical ×1.06.
- **balanced** — identity.

### Style efficacy (tactical familiarity)

```
efficacy = clamp(0.75 + 0.5 × tactical_familiarity_XI, 0.75, 1.25)
```

applied at kickoff to possession shift, chance volume and conversion modifiers
(not cards/stamina). Familiarity grows each training week
([Ch. 13](13-training-and-development.md)).

## 12.2 Formations (11 slots)

| Formation | Slot order |
| --- | --- |
| 4-3-3 | GK LB CB CB RB CM CM CM RW ST LW |
| 4-4-2 | GK LB CB CB RB RM CM CM LM ST ST |
| 4-2-3-1 | GK LB CB CB RB DM DM RM AM LM ST |
| 5-3-2 | GK CB CB CB LB RB CM CM CM ST ST |
| 3-2-4-1 | GK CB CB CB DM DM RM AM AM LM ST |
| 5-4-1 | GK CB CB CB LB RB LM CM CM RM ST |
| 4-5-1 | GK LB CB CB RB LM CM CM CM RM ST |
| 3-5-2 | GK CB CB CB LB RB CM CM CM ST ST |

Slots are filled by **position-family matching** (`squad.positionFit`:
defensive / midfield / forward; keepers never outfield).

## 12.3 Lineups

- `PUT /api/clubs/:id/lineup` — all 11 slots, unique players
  (`UNIQUE(club_id, player_id)`), ownership checked.
- Every slot must be **available**: eligible ([Ch. 11](11-player-lifecycle-and-academy.md))
  and not injured ([Ch. 14](14-condition-and-injuries.md)). An unavailable
  slot is a hard error; nothing is written.
- With no saved lineup (or for AI/absent clubs) the engine selects one:
  `best_eleven` (strongest), `rotate` (rotation-aware) or `best_fitness`
  ([Ch. 25](25-policybot-and-absence.md)).
- The **bench** is the top 5 available non-starters by attribute weight.
- The **penalty taker** is the XI's best `penalties` attribute, else the
  captain, else none (engine baseline 78%). The **captain** is the highest
  leadership (professionalism tie-break).

## 12.4 Deadlines

| Command | Rule |
| --- | --- |
| Lineup / tactics | `409` if the club's next fixture is already `live` or the world is paused/archived; a change after kickoff applies from the next fixture |
| Training plan | effective from the **next week boundary** (`effective_from_tick`); no rebate mid-week |
| Live tactical input | only while live; minute `[current+1, 90]` |

## 12.5 In-match controls

`POST /api/matches/:id/tactical` with `{"style": "<key>"}` switches the side's
style for the rest of the match (kind normalised to `tactic_change`), and
substitutions name the exact players in/out. Inputs are stored in
`match.match_inputs` and the engine **replays seed + ordered inputs** to the
identical outcome ([Ch. 15](15-match-engine.md), OPD-21).

## 12.6 Pre-match transparency

- **Scouting tags** — hidden traits as fixed labels via tunable thresholds.
- **Lineup warnings** — any key player whose projected performance factor is
  ≤ **0.95** gets a `lineup_warning` explanation decomposing consistency,
  high-stakes temperament/pressure, and his own sentiment.

## 12.7 API surface

| Route | Purpose |
| --- | --- |
| `GET /api/clubs/:id/squad` | players + positions + attributes + condition |
| `PUT /api/clubs/:id/lineup` | save XI |
| `GET/POST /api/clubs/:id/tactics` | style + formation |
| `GET/POST /api/clubs/:id/training-plan` | archetype ([Ch. 13](13-training-and-development.md)) |
| `POST /api/matches/:id/tactical` | live change |
| `GET /api/managers/me/club` | the caller's club |

Events: `LINEUP_SAVED`, `TACTIC_SET`, `TRAINING_PLAN_SET`; `actor_type` is
`manager` or `policy_bot` (the command layer is actor-agnostic so PolicyBot
calls the same cores: `tactics.SetLineupForClub`, `SetTacticsForClub`).

## Connections

- How the XI becomes engine ratings: [Chapter 16](16-matchday-and-live-matches.md).
- Style maths inside the engine: [Chapter 15](15-match-engine.md).
- Code: `internal/tactics`, `internal/squad/{lineup,ratings,scouting}.go`.
- Source: `docs/design/tactics-training-numerics.md` §1, §3; OPD-25.

---
[← Player lifecycle](11-player-lifecycle-and-academy.md) · [Contents](the-touchline-book.md) · [Next: Training →](13-training-and-development.md)
