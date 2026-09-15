# Morale, playing time and transfer-request numerics (S06-03)

Source of truth for the S06-03 decision numbers implemented in
`backend/internal/player/numerics.go`. These are **proposal** values awaiting
PM tuning sign-off; recalibration means editing the constants there (and this
doc), never the steering logic.

## Whole-season playing-time share

```
share = player_minutes / (90 × club_completed_matches)   clamped to [0, 1]
```

- `player_minutes` = the sum of the player's `player_appearances.minutes` in
  matches of the club they **currently** play for (a transferred player's
  prior-club minutes never count — the share resets to 0 at transfer).
- `club_completed_matches` = `COUNT(DISTINCT match)` over the club's completed
  fixtures (home or away), regardless of whether this player appeared.
- Scope: completed matches only. In active play these span the current season
  (the season is materialized match-by-match as it is simulated).

## Agreed squad roles and their expected shares

`squad_role` lives on the active `player.contracts` row and is lazily
backfilled from `player_preferences.playing_time_expectation` on the weekly
pass (migration 0040 ships a keyword backfill for pre-existing contracts).

| Role | Constant | Expected share | Expected label |
|---|---|---|---|
| `key_player` | `RoleExpectedShareKeyPlayer` | 0.75 | Plays ~3 of 4 matches |
| `rotation` | `RoleExpectedShareRotation` | 0.45 | Plays every other match |
| `squad_player` | `RoleExpectedShareSquadPlayer` | 0.20 | Occasional minutes |
| `development` | `RoleExpectedShareDevelopment` | 0.0 | No expectation, no pressure |

## Satisfaction bands and morale targets

A match evaluates the player once at completion:

```
ratio = share / expected
ratio ≥ 1      → satisfied target   (0.65)
ratio ≥ 0.5    → neutral target     (0.50)
ratio <  0.5   → unhappy target     (0.35, adjusted by personality)
```

The unhappy target moves with personality (step × (value − 50)/50, floored at
0.05 and capped below the neutral band):

| Trait | Constant | Effect on unhappy target |
|---|---|---|
| Patience | `PatienceMoraleStep` 0.06 | patient players tolerate a quiet spell (raises target → less punishment) |
| Loyalty | `LoyaltyMoraleStep` 0.06 | loyal players stay content longer (raises target) |
| Ambition | `AmbitionMoraleStep` 0.08 | ambitious players punish a bench harder (lowers target) |
| Ego | `EgoMoraleStep` 0.08 | star ego amplifies unhappiness when benched (lowers target) |

`development` has no expectation → always the satisfied target.

## Post-match swing (morale update)

```
next = current + alpha × (target − current)        alpha = swingAlpha
swingAlpha = 0.35 × vol × pro
vol factor: 1.0 → 1.5  as emotional_volatility 0 → 100
pro factor: 1.0 → 0.5  as professionalism 0 → 100
```

Volatile players swing harder; professional players dampen the swing. Result is
clamped to [0, 1].

## Weekly recovery

```
next = current + alpha_recovery × (0.5 − current)   clamped to [0, 1]
alpha_recovery = 0.10 × (0.5 → 2.0 as professionalism 0 → 100)
```

More experienced pros recover faster toward the neutral 0.5 baseline. The
weekly pass also clears `squad_role` backlogs and grades promises (below).

## Transfer-request trigger (weekly, deterministic, human-managed clubs only)

A player files a `pending` request when **all** hold:

1. `morale ≤ UnhappyMoraleThreshold = 0.35` (evaluated **after** the weekly
   recovery step),
2. `DeepShortfall`: `share < 0.5 × expected` (`TransferRequestDeepShare`,
   `expected > 0`),
3. no open `pending` request on record (partial unique index),
4. the cooldown has lapsed (`transfer_request_cooldown_until` is NULL or past),
5. the club is human-run (`is_ai_controlled = false`) and has a current
   manager.

The trigger emits `PLAYER_TRANSFER_REQUESTED` with an `Explanation` built from
morale, satisfaction and share-vs-expectation. AI clubs never request (they
have no manager to reassure/deny/approve).

## Request lifecycle numbers

| Action | Effect |
|---|---|
| Approve | status `approved`, player listed at `market_value` (`ListingOpenToOffers`), sentiment +15 |
| Deny | status `denied`, morale −0.10 (`DenyMoraleDrop`), cooldown 28 d (`DenyCooldownDays`), sentiment −25 |
| Reassure | status `reassured`, promise created (28 d, `PromiseEvaluationWeeks×7`), cooldown 28 d (`ReassureCooldownDays`), sentiment 0 |
| Unaddressed | auto-listed after 21 d (`AutoListTTLWorldDays`), status `auto_listed` |

## Promise grading (weekly)

`increase_playing_time` promises are graded each weekly pass:

- `share ≥ expected` → `fulfilled`, sentiment +10.
- else if promise older than `PromiseEvaluationWeeks = 4` weeks → `broken`,
  sentiment −30, cooldown cleared (the player is free to ask again).

## Fresh start on transfer

`OnPlayerTransferred` (the transfer-completion hook, step 1b):

- morale → `FreshStartMorale = 0.85`,
- `playing_time_pct` → 0,
- cooldown cleared,
- any open request → `withdrawn`.

## Relationship memory deltas

| Event | Sentiment delta | Relationship type |
|---|---|---|
| transfer approved | +15 | professional_respect |
| transfer denied | −25 | dislike |
| reassured | 0 | professional_respect |
| promise kept | +10 | professional_respect |
| promise broken | −30 | dislike |

Deltas accumulate on the single canonical `player↔manager` row
(`social.relationships`, partial unique `uq_relationship_player_manager`),
flipping its `relationship_type` at < 0. History rows land on
`social.relationship_events` regardless of sentiment.

## Inputs that intentionally do NOT score

- Match **results** and trophies (deferred to S06-04/S09-03 media),
- wage demands and bow/capped/loan clauses (S08-01/S10),
- squad-mates, dressing-room factions (S09-02),
- manager-visible "why" detail is served read-only from the weekly policy, not
  from a live interaction loop (contract renegotiation lands with S10-01
  agents).