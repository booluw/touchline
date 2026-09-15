# Social numerics — trust, messaging and rivalries (S06-04)

Source of truth for the S06-04 decision numbers implemented across
`backend/internal/social/` (profiles, direct messaging, auto-tracked rivalries)
and the match-completion rivalry hook. These are **proposal** values awaiting PM
tuning sign-off; recalibration means editing the constants in the social
package (and this doc), never the steering logic.

## Trust score (sourced from `social.trust_events`)

A manager's trust score is the **on-the-fly `SUM(delta)`** of their append-only
`social.trust_events` rows. No materialized score exists; the profile endpoint
composes it from the log on every read (so a recalibrated delta is instantly
reflected across history).

Initial seeding (migration 0041) mirrors the S06-03 player↔manager journal
`social.relationship_events` into `trust_events`, so managers get a baseline on
day one. `reassured` (delta 0) rows are skipped as noise.

| Source event (S06-03) | Seeded trust delta |
|---|---|
| transfer approved | `+15` (journal `sentiment_delta`) |
| transfer denied | `−25` (journal `sentiment_delta`) |
| playing-time promise kept | `+10` |
| playing-time promise broken | `−30` |

| Event (S06-04c, delivered) | Delta |
|---|---|
| Completing a fixture won by the manager's club | `+5` |
| Completing a fixture lost by the manager's club | `−5` |
| Drawn fixture | `0` (no row) |

Future deltas (later sprints):

| Event (planned) | Delta | Sprint |
|---|---|---|
| Politeness / conduct flags on messages | TBD | S06-04b+ |

## Direct messaging limits (S06-04b)

- **Max body**: 2 000 characters after trimming; longer payloads are rejected
  with 413 (handler) — the service enforces the same limit.
- **Sanitization (basic)**: HTML tags stripped, surrounding whitespace trimmed.
  No profanity filter.
- **Rate limits** per sender (rolling, via `idx_messages_sender`):
  - 30 messages per minute → 429 with `Retry-After`.
  - 200 messages per day.

## Rivalry accumulation (S06-04c)

Completed fixtures update graph edges in `social.relationships` inside the
match-completion transaction (the `match.Finalize` live path and the
`PlayFixture` deterministic path; `ReconcileRivalries` backfills anything older,
run weekly by the scheduler). Constants live in
`backend/internal/social/rivalry.go`. Rules:

- **club↔club** rivalry edge (`relationship_type = 'rivalry'`): every completed
  fixture, AI or human managed.
- **manager↔manager** rivalry edge: only when **both** clubs' current managers
  are human (`is_policy_bot = false`). If either side is an AI manager, the
  rivalry stays exclusively at the club level — AI managers never carry personal
  edges.
- Edges are stored **canonically** (`entity_a_id ≤ entity_b_id` by UUID order)
  so the partial-unique constraint never holds both orientations of one pair;
  reads match either side and render the peer.
- Per meeting, strength grows by:

  ```
  delta = (base + 2 × min(goal_difference, 5)) × big_match_multiplier
  ```

  - base: `8` (manager↔manager) or `10` (club↔club);
  - big-match multiplier `2` when the fixture is a league meeting of two
    same-country clubs (both countries non-empty and equal);
  - `+3` repeat bonus whenever the pair has already met (a standing rivalry
    escalates faster than a novelty).
- **Decay**: an edge untouched for more than 45 days loses ~20% (`strength −=
  strength/5`) on the next touch before the new meeting accumulates — so
  long-dormant pairings fade while active ones compound.
- Strength clamps to `[−100, 100]`; `last_interaction_at` = the fixture's
  completion time on every touch.
- Trust: win `+5`, loss `−5` for each **human** side of the fixture (independently,
  including human vs AI clubs); drawn fixtures write nothing. AI managers write
  no trust events.
- Each completion also records a `RELATIONSHIP_CHANGED` world event and, after
  the completion commit, pushes a best-effort `relationship_change` realtime
  envelope (world-scoped; the graph read stays authoritative).

`GET /api/relationships` returns the caller's personal edges plus their active
club's edges — the same surface the profile's rivals tab renders.