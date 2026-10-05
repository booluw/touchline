# Chapter 18 — Morale, playing time and transfer requests

Players want to play. This chapter covers how minutes become satisfaction,
satisfaction becomes **morale**, and sustained unhappiness becomes a formal
**transfer request** the manager must answer. All numbers are proposal
constants in `internal/player/numerics.go` (OPD-03 morale slice).

## 18.1 Playing-time share

```
share = player_minutes / (90 × club_completed_matches)    clamped [0, 1]
```

- Minutes count only at the **current** club; a transfer resets the share to 0.
- The denominator counts every completed club match, whether or not the player
  appeared.

## 18.2 Squad roles and expectations

`squad_role` lives on the active contract (lazily backfilled weekly from the
player's strongest `playing_time_expectation` preference — OPD-56).

| Role | Expected share | Meaning |
| --- | --- | --- |
| `key_player` | 0.75 | ~3 of 4 matches |
| `rotation` | 0.45 | every other match |
| `squad_player` | 0.20 | occasional minutes |
| `development` | 0.0 | no expectation, no pressure |

## 18.3 Per-match morale swing

After each completed match:

```
ratio = share / expected
ratio ≥ 1    → target 0.65 (satisfied)
ratio ≥ 0.5  → target 0.50 (neutral)
ratio < 0.5  → target 0.35 (unhappy), personality-adjusted
development  → always satisfied

next = clamp(current + α · (target − current), 0, 1)
α    = 0.35 × vol × pro
vol  : 1.0 → 1.5 as emotional_volatility 0 → 100
pro  : 1.0 → 0.5 as professionalism 0 → 100
```

The **unhappy** target moves with personality (step × (value − 50)/50, floored
at 0.05, kept below neutral):

| Trait | Step | Direction |
| --- | --- | --- |
| patience | 0.06 | gentler (raises target) |
| loyalty | 0.06 | gentler |
| ambition | 0.08 | harsher (lowers target) |
| ego | 0.08 | harsher |

Match **results** do not move individual morale (by design for this slice).

## 18.4 Weekly recovery

```
next = clamp(current + 0.10 × (0.5 → 2.0 by professionalism) × (0.5 − current), 0, 1)
```

Professionals drift back toward neutral faster.

## 18.5 Transfer-request trigger (weekly)

A player files a `pending` request when **all** hold:

1. morale ≤ **0.35** (`UnhappyMoraleThreshold`), after recovery;
2. deep shortfall: `share < 0.5 × expected` (expected > 0);
3. no open pending request (partial unique index);
4. cooldown lapsed;
5. the club is **human-managed** (AI clubs never request — there is no one to
   answer).

Emits `PLAYER_TRANSFER_REQUESTED` with an explanation (morale, satisfaction,
share vs expectation). Reasons: playing_time / wage / ambition / homesickness.

## 18.6 Answering a request

`POST /api/clubs/:id/players/:playerID/transfer-request/{approve|deny|reassure}`

| Action | Effect | Player↔manager relationship |
| --- | --- | --- |
| **Approve** | listed at market value (`open_to_offers`) ([Ch. 21](21-transfer-market.md)) | +15 (professional_respect) |
| **Deny** | morale −0.10, cooldown 28 days | −25 (dislike) |
| **Reassure** | creates an `increase_playing_time` promise (28 days), cooldown 28 days | 0 |
| *Unaddressed* | **auto-listed after 21 world days** (`AutoListTTLWorldDays`), status `auto_listed` | — |

### Promises

Graded each weekly pass: `share ≥ expected` → **fulfilled** (+10); older than 4
weeks → **broken** (−30) and the cooldown clears (the player may ask again).

### Fresh start on transfer

`OnPlayerTransferred`: morale → **0.85**, share → 0, cooldown cleared, any open
request → `withdrawn`.

## 18.7 Relationship memory

All deltas above accumulate on the single canonical `player↔manager` row in
`social.relationships` (flipping `relationship_type` to `dislike` below 0), with
history in `social.relationship_events`. The constants are named `Sentiment*`
for historical reasons — they are **not** supporter sentiment. The same journal
seeded managers' initial **trust** scores ([Ch. 24](24-social-and-rivalries.md)).
The dressing room reads the mean player→manager sentiment as
`manager_support` ([Ch. 19](19-dressing-room.md)).

## 18.8 Surfaces

- Roster shows morale, share, role and any open request per player.
- Dashboard **Important → morale** for players at ≤ 0.35 or with an open
  request ([Ch. 26](26-dashboard-news-scouting-realtime.md)).
- Squad morale feeds match strength ([Ch. 16](16-matchday-and-live-matches.md)).

## 18.9 Deferred

Results/trophies, wage demands, contract renegotiation (S10-01 agents) and
squad-mate influence do not score here yet.

## Connections

- Minutes also drive development: [Chapter 13](13-training-and-development.md).
- Code: `internal/player/{numerics,morale,weekly,transfer_requests}.go`.
- Source: `docs/design/morale-numerics.md`, OPD-03 (morale), OPD-56.

---
[← Form](17-form.md) · [Contents](the-touchline-book.md) · [Next: Dressing room →](19-dressing-room.md)
