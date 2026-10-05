# Chapter 19 — Dressing-room dynamics and factions

The dressing room is a **graph**, not a stored structure: the player↔player
subgraph of `social.relationships`, read at request time and fed to a pure,
RNG-free engine in `internal/faction` (S09-02, OPD-30/31).

## 19.1 Relationship generation

For each unordered pair in a squad, additive edges are written (canonical
`entity_a_id < entity_b_id`, so upserts are idempotent):

| Condition | Edge kind | Strength | Trust |
| --- | --- | --- | --- |
| same nationality | `national_team` | 45 | 20 |
| both academy products | `academy` | 35 | 15 |
| age gap ≥ 8, same primary position, elder leadership ≥ 65 | `mentorship` | 40 | 25 |
| pair affinity ≥ 65 | `friendship` | 20 + (affinity − 65) | ½ strength |
| pair affinity ≤ 25 | `rivalry` | −(20 + (25 − affinity)) | −½ strength |

`affinity = clamp(base + (mean(sociability) − 50)/2, 0, 100)`, `base` a
deterministic 0–100 draw from `PairStream(world_seed, a, b)` — so squad changes
never re-roll an existing bond.

When a player is sold, `former_teammate` edges (strength 20, trust 10) are
written to every remaining squad member **before** the ownership flip
(`OnPlayerSold`).

## 19.2 Hierarchy

```
centrality(player) = Σ (strength + trust) over incident edges
```

| Rank by centrality (ties → lower id) | Tier |
| --- | --- |
| 0 | `team_leader` |
| 1–2 | `highly_influential` |
| 3–5 | `influential` |
| 6+ | `other` |

The read (`IM12`, OPD-31) returns **influencers only** (`other` is dropped) and
embeds each influencer's full `PlayerProfile` so one request draws the panel.

## 19.3 Factions

A faction is a **connected component** of the player graph (undirected
recursive CTE `componentRoots`; union-find fallback in the pure engine). Its
leader is the highest-centrality member.

- **Cohesion** = `clamp(50 + w/2, 10, 95)` with `w` the mean internal
  `(strength + trust)`; an isolated member is 10.
- **Label** by dominant edge kind: `national_team` → `foreign_cohort`,
  `academy` → `youth_alliance`, `mentorship` → `veteran_core`; else mean
  leadership ≥ 70 → `veteran_core`; else `neutral_room`.

## 19.4 Contagion and unrest

A management action affecting a player spreads by deterministic BFS from that
epicentre. Confidence starts at **24**, gains `max(1, 10 − (depth − 1) × 2)`
per newly reached node, capped at **84** (never 100, never jumps).

| Confidence | Unrest |
| --- | --- |
| < 60 | none |
| ≥ 60 | `demand = board_meeting` |
| ≥ 80 | `demand = en_masse_transfer_requests` |

Unrest emits `SQUAD_UNREST_TRIGGERED` with an explanation chaining epicentre,
affected count and demand. **Bonding** actions (`contract_approved`,
`team_talk_motivational`) never spread; they add **+6** cohesion per faction
(cap 95).

## 19.5 Club aggregates

| Aggregate | Derivation |
| --- | --- |
| `dressing_room_mood` | mean player morale → 0–100 (50 if none) |
| `manager_support` | mean player→manager sentiment (−100..100) → 0–100 (50 if none) |
| `cohesion` | member-weighted mean faction cohesion, 10–95 |
| `unrest` | latest `SQUAD_UNREST_TRIGGERED` within 14 days |

## 19.6 Reserved seams

The full action matrix is unit-tested, but most live management actions
(`ActionInspect`, missed promise, dropped from XI, long-term bench, wage cut,
bid query/blocked, training load, critical talk) have **no emitting call site
yet**; the transfer path (sale) is wired. The hierarchy UI diagram is deferred.

## Connections

- The same graph table powers rivalries and player↔manager memory: [Chapter 24](24-social-and-rivalries.md), [Chapter 18](18-morale-and-transfer-requests.md).
- Personality traits used here: [Chapter 10](10-players.md).
- Code: `internal/faction/{engine,generate,model,service,store,stream}.go`.
- Source: `backend/docs/design/squad-dynamics-numerics.md`, OPD-30 (dressing room), OPD-31, IM12.

---
[← Morale](18-morale-and-transfer-requests.md) · [Contents](the-touchline-book.md) · [Next: Finance →](20-finance.md)
