# Squad dynamics numerics (S09-02 — dressing-room factions)

The dressing-room dynamics engine follows the same discipline as the other
tuning surfaces: every number below is a **proposal constant** in
`internal/faction` and recalibration is a **data-only change** (constants are
referenced, never re-derived, by callers). The engine is pure — no I/O, no wall
clock, no RNG — so the same `(action, SquadSnapshot)` always produces the same
outcome. Relationship *generation* is deterministic too: a pair's affinity is
seeded only by `(world_seed, player_a, player_b)` via `PairStream`, so squad
changes never re-roll an existing bond, and the generator consumes no other
subsystem's RNG stream (the matchsim replay contract is untouched).

## Sources

- PRD §15 (dressing-room factions, hierarchy, leaders, cohesion) and §13
  (morale).
- Technical plan §6 (social graph) and §16 (explanation / causal chains).
- S09-02 task ACs: hierarchy from `social.relationships` with no
  denormalization; leader influence; severe unrest → delegation demanding a
  board meeting or en-masse transfer requests; `SQUAD_UNREST_TRIGGERED` events
  with full `Explanation`.

## Hierarchy (centrality)

`centrality(player) = Σ(strength + trust)` over that player's incident
relationship edges, both directions. Tiers by centrality rank (descending,
ties broken by ascending player id):

| rank | tier |
|---|---|
| 0 | `team_leader` |
| 1–2 | `highly_influential` |
| 3–5 | `influential` |
| 6+ | `other` |

Constants: `TierHighlyEnd = 3`, `TierInfluentialEnd = 6`.

## Factions (graph connected components)

A faction is one connected component of the player↔player graph. Components are
resolved by an undirected recursive CTE (`componentRoots`, root = minimum
reachable player id); the pure engine falls back to union-find when a snapshot
carries no roots. Each faction's leader is its highest-centrality member
(ties → lowest id).

### Cohesion

With `n` internal edges and mean edge weight `w = mean(strength + trust)` in
`−100..100`:

```
cohesion = clamp(50 + w/2, 10, 95)
```

An isolated member (no internal edges) has cohesion 10 (`CohesionMin`). Bounded
strictly below 100. Constants: `CohesionNeutral = 50`, `CohesionMin = 10`,
`CohesionMax = 95`.

### Label

A component's label is chosen from its dominant edge kind, falling back to
mean leadership:

1. dominant `national_team` → `foreign_cohort`
2. dominant `academy` → `youth_alliance`
3. dominant `mentorship` → `veteran_core`
4. else mean leadership ≥ `VeteranLeadership` (70) → `veteran_core`
5. else `neutral_room`

## Relationship generation

For each unordered pair, edge kinds are emitted (all additive; canonical
`entity_a_id < entity_b_id`, so upsert is idempotent):

| condition | kind | strength | trust |
|---|---|---|---|
| shares a nationality | `national_team` | 45 | 20 |
| both academy products | `academy` | 35 | 15 |
| age gap ≥ 8, same primary position, elder leadership ≥ 65 | `mentorship` | 40 | 25 |
| pair affinity ≥ 65 | `friendship` | 20 + (affinity−65) | half strength |
| pair affinity ≤ 25 | `rivalry` | −(20 + (25−affinity)) | −half strength |

Pair affinity is `clamp(base + sociabilityAdj/2, 0, 100)`, where `base` is a
deterministic 0..100 draw from `PairStream(world_seed, a, b)` and
`sociabilityAdj = mean(sociability) − 50`. Constants: `FriendshipThreshold =
65`, `RivalryThreshold = 25`, `MentorshipAgeGap = 8`, `MentorshipLeadership =
65`.

On sale, `former_teammate` edges (strength 20, trust 10) are written from the
departing player to every remaining squad member before the ownership flip, so
the bond is recorded against the pre-sale room.

## Contagion

A deterministic BFS from the action's epicentre, over the adjacency of the
current snapshot. Confidence starts at `ContagionStart = 24` (well under 100)
and gains `hopBump(depth)` per newly reached node:

```
hopBump(1)  = 10                       (ContagionHopBump)
hopBump(d)  = max(1, 10 − (d−1)×2)     (ContagionHopDecay = 2)
```

Confidence is capped at `ContagionCap = 84`, so it **never reaches 100** and
never jumps. The affected list is the BFS visitation order, epicentre first.

## Unrest

Unrest forms only when contagion crosses the attention threshold:

| confidence | unrest |
|---|---|
| < 60 | none |
| ≥ 60 (`UnrestThreshold`) | `demand = board_meeting` |
| ≥ 80 (`EnMasseThreshold`) | `demand = en_masse_transfer_requests` |

Severity equals the contagion confidence (bounded < 100). Unrest always carries
an `explanation.Explanation` chaining the epicentre, affected count and demand.

## Bonding

`contract_approved` and `team_talk_motivational` never produce contagion. They
raise each faction's cohesion by `BondingCohesionBump = 6`, clamped at
`CohesionMax = 95`.

## Club aggregates (read model)

- `dressing_room_mood`: mean `player.player_condition.morale` `[0,1]` mapped to
  `[0,100]`; 50 when no condition rows exist.
- `manager_support`: mean player→manager `social.relationships.sentiment`
  `[−100,100]` mapped to `[0,100]`; 50 when no player has an opinion.
- `cohesion`: member-weighted mean faction cohesion, bounded `[10,95]`.
- `unrest`: latest `SQUAD_UNREST_TRIGGERED` for the club within 14 days.

## Reserved seams

- Live (non-transfer) management-action triggers are unit-tested through the
  full action matrix but have no emitting call sites yet; `ActionInspect`,
  `ActionMissedPromise`, `ActionDroppedFromStartingXI`, `ActionBenchedLongTerm`,
  `ActionWageCutOffered`, `ActionTransferBidQuery`, `ActionTransferBidBlocked`,
  `ActionTrainingLoadIncreased` and `ActionTeamTalkCritical` await their
  management-action pipeline wiring.
- The UI hierarchy diagram (S09-02 AC3) is deferred to a follow-up.
