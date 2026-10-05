# Chapter 24 — Social: trust, messaging and rivalries

The social layer makes a world *multiplayer*: managers have public profiles and
a trust score, message each other, and accumulate rivalries — between clubs and,
when both are human, between managers.

## 24.1 The relationship graph

`social.relationships` is a polymorphic graph (player, manager, club nodes).
Edges carry `relationship_type`, `strength` (−100..100), `trust`, `sentiment`,
`last_interaction_at`, and are stored **canonically** (`entity_a_id ≤
entity_b_id`) so one pair never holds both orientations. Uses across the book:

| Edge | Chapter |
| --- | --- |
| club↔club `rivalry` | this chapter, [9](09-clubs-dna-supporters.md), [23](23-board-and-job-security.md) |
| manager↔manager `rivalry` | this chapter |
| player↔manager (respect/dislike) | [18](18-morale-and-transfer-requests.md) |
| player↔player (national_team, academy, mentorship, friendship, rivalry, former_teammate) | [19](19-dressing-room.md) |

## 24.2 Rivalry accumulation (S06-04c)

Updated inside the match-completion transaction (live and quick-play); weekly
`ReconcileRivalries` backfills older fixtures.

- **club↔club** edge on **every** completed fixture (AI or human).
- **manager↔manager** edge only when **both** current managers are human (AI
  managers never carry personal edges).
- Per meeting:

```
delta = (base + 2 × min(goal_difference, 5)) × big_match_multiplier  (+3 if the pair has met before)
base  = 10 club↔club · 8 manager↔manager
big_match_multiplier = 2 for a league meeting of two same-country clubs
```

- **Decay**: an edge untouched > 45 days loses ~20% (`strength −= strength/5`)
  on the next touch, before the new meeting is added.
- Clamp `[−100, 100]`. Each completion records `RELATIONSHIP_CHANGED` and,
  after commit, a best-effort `relationship_change` realtime push.

A club edge with strength ≥ **50** makes the fixture a **rivalry game** for the
board: supporter sentiment moves twice as fast ([Ch. 23](23-board-and-job-security.md)).

## 24.3 Derbies (engine-side)

Separately, `club.rivalries` holds seeded rivalries with a fixed `intensity`
(proposal **80** for city/regional derbies, otherwise derived or seeded
1–99). A fixture is a **derby** when a row links the clubs (either direction)
with `intensity ≥ 60`. Derby status feeds the motivation floor, card scaling and
high-stakes temperament divergence ([Ch. 16](16-matchday-and-live-matches.md)).
Rivalries are intra-world (clubs are world-scoped).

## 24.4 Trust

A manager's trust score is the **on-the-fly `SUM(delta)`** of
`social.trust_events` — no materialised score, so a recalibrated delta is
instantly reflected across history.

| Source | Delta |
| --- | --- |
| fixture won by the manager's club (human side) | +5 |
| fixture lost | −5 |
| draw | none |
| seeded from the player↔manager journal (migration 0041): approved / denied / promise kept / broken | +15 / −25 / +10 / −30 |

AI managers write no trust events. Message-conduct deltas are planned.

## 24.5 Direct messaging (S06-04b)

- Body ≤ **2,000** characters after trimming (`413` over).
- HTML stripped, whitespace trimmed; no profanity filter yet.
- Rate limits per sender: **30/minute** (`429` + `Retry-After`) and
  **200/day**.
- Delivered realtime as `social_message`.

Moderation policy (reporting/blocking/commissioner bans) is open decision
**OPD-06**.

## 24.6 Profiles and reads

- Manager profiles show career, reputation, trust and rivals.
- `GET /api/relationships` returns the caller's personal edges plus their
  club's edges (the profile's rivals tab).

## Connections

- Supporter amplification: [Chapter 9](09-clubs-dna-supporters.md).
- Dashboard "rivals" items: [Chapter 26](26-dashboard-news-scouting-realtime.md).
- Code: `internal/social/{rivalry,message,profile,service,store}.go`.
- Source: `docs/design/social-numerics.md`, `docs/design/derby-rivalry-determination.md`, OPD-03 (social), S06-04a–c.

---
[← The board](23-board-and-job-security.md) · [Contents](the-touchline-book.md) · [Next: PolicyBot →](25-policybot-and-absence.md)
