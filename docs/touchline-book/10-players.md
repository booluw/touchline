# Chapter 10 — Players: attributes, overall, hidden traits and personality

A player is four layers of data: **visible attributes** (what they can do),
**hidden traits** (what they could become and how they behave under stress),
**personality** (how they react to you), and **state** (condition, morale,
contract, injuries — covered in later chapters).

## 10.1 Generation (`pkg/playergen`)

Players are procedurally generated, DB-free:

- **Names & nationality** from the `ref` pools weighted by
  `ref.nationalities.generation_weight` (21 nationalities).
- **Attributes** per position from base profiles (e.g. `ST: technical 66,
  physical 62, mental 60, tactical 48, positional 56`; `GK: goalkeeping 68 …`),
  with per-key spreads.
- **Talent class** (`talent.go`): `Journeyman` / `TopProspect` / `Wonderkid` /
  `Generational`. Default world odds per 1000: **940 : 55 : 4 : 1**. The class
  raises the **floor** of the hidden potential roll (`potentialBonus`: +0 /
  +12 / +22 / +32) without widening it. Academy and street intakes use their own
  odds ([Ch. 11](11-player-lifecycle-and-academy.md)).
- **Origin** (`pool_origin`): `street`, `academy`, `generated`, `bulk`, `draft`.

## 10.2 Visible attributes (EAV)

`player.player_attributes` stores one row per `(player, attribute_key)` in
`[1, 100]`, grouped into six categories:

| Category | Keys |
| --- | --- |
| technical | finishing, passing, dribbling, crossing, long_shots, heading, free_kicks, penalties, first_touch, tackling |
| physical | pace, acceleration, strength, jumping, agility, stamina, balance, natural_fitness |
| mental | composure, anticipation, vision, work_rate, concentration, decision_making, positioning, off_the_ball, teamwork |
| tactical | pressing, creativity, tempo_control, defensive_awareness |
| positional | versatility, positional_instinct, space_reading, man_awareness, marking |
| goalkeeping | handling, reflexes, diving, one_on_ones, aerial_control, kicking, throwing, penalty_stopping |

Outfielders have no goalkeeping keys; keepers have no technical keys.
`tackling`, `marking`, `teamwork` were added in migration 0033 and backfilled at
each player's category mean so ratings didn't shift.

The only writer of attribute values after generation is the **weekly training
sweep** ([Ch. 13](13-training-and-development.md)); every change is journalled
in `player.player_attribute_changes`.

## 10.3 The overall rating — one recipe everywhere

There is exactly **one** user-facing overall: `squad.PositionalOverall`
(`internal/squad/overall.go`, S08-01). It replaced three ad-hoc overalls that
drifted.

1. Compute the six **category means**.
2. Apply the position's two-sided recipe from `squad.DefaultPositionWeights`
   (the same table the match engine's team ratings use) to get an **attack**
   and **defence** score — a "one-member XI".
3. `overall = clamp(round(mean(attack, defence)), 1, 99)`. 99 is a display cap
   (`OverallCap`); raw attributes still run to 100.

Every read model — roster, player card, offers' top player, scouting, free
agents, academy, admin dashboards, market valuation — uses this recipe, so two
screens can never disagree. `OverallDelta` gives the signed position-weighted
change since the last weekly update.

> The development engine uses its own blend (`development.Overall`: outfield
> technical 0.35 / physical 0.20 / mental 0.25 / tactical 0.10 / positional
> 0.10; GK goalkeeping 0.55 …) purely as the comparator against hidden
> potential ([Ch. 13](13-training-and-development.md)). It is never displayed.

**Headline keys** (`HeadlineKeysForPosition`) are an EA-style condensed stat
block per position (e.g. ST: finishing, heading, composure, pace, off_the_ball,
first_touch).

## 10.4 Hidden traits

`player.player_hidden_traits` (all 1–100 unless noted):

| Trait | Effect |
| --- | --- |
| `potential` | ceiling the development engine grows toward ([Ch. 13](13-training-and-development.md)) |
| `potential_ceiling_locked` (bool) | stops potential flexing (age 27 or budget spent) |
| `consistency` | width of the per-match performance variance ([Ch. 16](16-matchday-and-live-matches.md)) |
| `injury_susceptibility` | injury severity steps & starting injury risk ([Ch. 14](14-condition-and-injuries.md)) |
| `adaptability` | reaction engine; future relocation effects |
| `professionalism` | morale damping & recovery ([Ch. 18](18-morale-and-transfer-requests.md)), development discipline |
| `ambition`, `loyalty` | reaction engine |
| `temperament`, `pressure_handling` | performance divergence in high-stakes fixtures only |
| `learning_speed` | reveal source; future training efficacy |

### What managers can see (OPD-59, OPD-60)

- **Own players** carry `hidden_attributes` — exact values of every hidden-trait
  column **except** `potential` / `potential_ceiling_locked` — on
  `GET /api/players/:id`, `GET /api/clubs/:id/players`,
  `GET /api/clubs/:id/players/:pid` and `.../development` (IM35, IM37). Other
  clubs' players never carry the block (same rule as `weekly_wage`).
- **Potential is never exposed**, to anyone.
- **Scouting tags** (`squad.ScoutingTags`) translate traits into fixed labels by
  tunable thresholds, and **lineup warnings** flag key players projected at or
  below 95% (`LineupWarningThreshold`) with an explanation of which trait
  dragged them ([Ch. 12](12-squad-tactics-lineups.md)).
- IM36 (*not started*) will make hidden attributes trainable.

## 10.5 Personality

`player.player_personality` (1–100): `professionalism`, `ambition`, `loyalty`,
`ego`, `sociability`, `adaptability`, `patience`, `leadership`,
`emotional_volatility` (PRD §12).

Where personality acts today:

| Trait | System |
| --- | --- |
| patience, loyalty, ambition, ego | the "unhappy" morale target ([Ch. 18](18-morale-and-transfer-requests.md)) |
| emotional_volatility, professionalism | morale swing & recovery speed |
| leadership | squad morale weighting, captain choice, mentorship edges, faction labels ([Ch. 16](16-matchday-and-live-matches.md), [Ch. 19](19-dressing-room.md)) |
| sociability | pair affinity in the dressing room |
| ambition | retirement probability ([Ch. 11](11-player-lifecycle-and-academy.md)) |

### The reaction engine (`internal/personality`, S09-01)

A **pure, RNG-free** engine: `Engine.React(action, traits)` returns
deterministic reactions, each with an `Explanation` naming the trait and value
that drove it, plus any **hidden-trait reveal** the interaction leaks. The
engine works on a 1–10 projection of the traits.

Action catalogue: missed promise, dropped from XI, long-term bench, wage cut,
contract structure approved, transfer bid query, transfer bid blocked, released,
training load raised, motivational/critical team talk.

**Gradual reveals** (`reveal.go`): an interaction source can leak only the
hidden trait it credibly evidences, at a starting confidence below 100 that
hardens by `revealBump = 15` with consistent repeat evidence:

| Source | Can reveal |
| --- | --- |
| high-stakes fixture | pressure_handling |
| sustained training overload | learning_speed |
| recovery window | injury_susceptibility |
| transfer window | adaptability / consistency (via ego) |
| team talk | temperament / volatility |

Many action triggers are unit-tested but not yet wired to live management
actions ([Ch. 19](19-dressing-room.md) §reserved seams).

## 10.6 Player reads

| Route | Contents |
| --- | --- |
| `GET /api/clubs/:id/players` (IM17) | per active player: role, morale, playing time, transfer request, **six category means**, **overall**; own club adds hidden attributes + dossier |
| `GET /api/players/:id` (IM20) | identity, current club or free agent, means + overall, **career record** (appearances, goals, assists, average rating from `player.player_appearances` — completed matches only), weekly wage only for own players |
| `dossier` (IM37) | everything stored except potential. **Public** for any world player: `bio`, `attribute_values` (category → key → value), `development`, `condition`, `personality`. Single-player reads add `history` (10 most recent appearances / attribute changes / injuries / events). **Private** (own players, single reads): contracts, emotional states, preferences, transfer requests |

Reads are scoped to the caller's **world**, not club: an opponent or target is
readable; another world's player is a 404 identical to a non-existent one.

### Status

`player.players.status`: `active`, `injured`, `suspended`, `free_agent`,
`retired` … Senior headcount (offers) = active/injured/suspended.

### Roster columns (IM40, OPD-61)

Roster rows (`GET /api/clubs/:id/players`) also carry `nationality` (code + name), `date_of_birth`, `age` (completed years on the world calendar) and `contract` (active weekly wage + end date; null without an active contract). The roster still lists `status = 'active'` players only, so injured or loaned players do not appear in it. Potential stays hidden (OPD-60).

IM63 adds `recent_ratings`: the last five **rated** appearances (1–10), oldest first, empty when none. It backs the Squad screen's Form column (the mean of those ratings plus five bars).

### Squad screen (IM63, OPD-65)

`/play/squad` shows the roster as a sortable table (Simple/Standard density; no Advanced mode because potential stays hidden). Filters: position group, search, and status (Wants out, Unhappy < 55 morale, Expiring ≤ 12 months, Injured). On desktop the selected player opens in a side panel. On mobile a tap opens `/play/players/:id`, which shows the same panel with tabs for the caller's own players. The panel reads `GET /api/clubs/:id/players/:playerID` (morale, role expectation, latest unexpired emotional state, condition, 1–20 attributes = ceil(stored/5), personality, contract with release clause, manager relationship history) plus the player's faction from `/dynamics`. The morale "Why?" lists the exact terms of the morale target (`explanation.why`, IM64). A pending request offers approve (asking price 0.8 / 1.0 / 1.25 × value), reassure (`POST .../transfer-request/reassure`, which pauses the request with a promise) or deny. The confirm step shows `GET .../transfer-request/preview`: the constants each action applies (IM65).

## Connections

- Lifecycle (intake, ageing, retirement, eligibility): [Chapter 11](11-player-lifecycle-and-academy.md).
- Training and development: [Chapter 13](13-training-and-development.md).
- Condition & injuries: [Chapter 14](14-condition-and-injuries.md).
- Market value: [Chapter 21](21-transfer-market.md).
- Code: `pkg/playergen`, `internal/squad/{overall,ratings,scouting}.go`, `internal/player/{store,profile,dossier}.go`, `internal/personality`.
- Source: PRD §§11–13, academy-numerics §6, OPD-43, OPD-46, OPD-59, OPD-60, IM17, IM20, IM35, IM37.

---
[← Clubs](09-clubs-dna-supporters.md) · [Contents](the-touchline-book.md) · [Next: Player lifecycle →](11-player-lifecycle-and-academy.md)
