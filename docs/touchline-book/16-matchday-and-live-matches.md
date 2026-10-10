# Chapter 16 — Matchday orchestration and live matches

This chapter covers everything around the pure engine: turning a club into a
`Team`, kicking matches off on time, pacing them live, finishing them, and the
cascade of consequences each result triggers.

## 16.1 The flow of one fixture

```
scheduled ──(scheduled_at ≤ worldNow)──► KickoffDue ──► live (snapshot frozen)
     ▲                                                      │ RunLive: 1 simulated minute
     │                                                      │ per tick.match_cadence,
     │                                                      │ one transaction per minute
     │                                                      ▼
 next round materialises (cups) ◄── ApplyResult ◄── Finalize (full time)
                                        │
                                        ├─ standings / cup bracket / rollover
                                        ├─ form, appearances, ratings, morale
                                        ├─ rivalries + trust            (social)
                                        ├─ manager match rating + sentiment + fan news (board)
                                        └─ injuries
```

Two entry paths exist and share the same hooks:

- **Live** — `internal/matchday.Runner` + `internal/match/live.go` (the normal
  path).
- **Quick-play** — `match.Service.PlayFixture` (single fixture, tests and
  tools).

## 16.2 Kickoff (`Runner.KickoffDue`)

Called after every daily tick and by the worker's ~15 s kickoff poll. For each
due fixture (`scheduled_at ≤ worldNow`, status `scheduled`):

1. Group due fixtures into per-competition open rounds; apply the
   **round-order gate** and the **simultaneity cap** (`max_simultaneous_matches`,
   default 3, staggered competitions only) ([Ch. 6](06-leagues-and-scheduling.md) §6.5).
2. PolicyBot `EnsureMatchInputs` fills lineup/tactics for absent managers
   ([Ch. 25](25-policybot-and-absence.md)).
3. Build both teams (§16.3), derive the fixture seed, freeze a **simulation
   snapshot** on `match.matches` (status `live`, `pacing_millis` from
   `tick.match_cadence`).

Idempotent: only still-`scheduled` fixtures kick off on redelivery.

## 16.3 Building a team (`match.buildTeam`)

The orchestration pipeline (`internal/squad`, pure, seeded per club):

1. **XI selection** — saved lineup or auto-selection; bench = top 5 available.
2. **Squad morale** `ComputeSquadMorale` → [0.90, 1.10]: starters' sentiment
   weighted by leadership × starter status (starters ≈ 2× fringe).
3. **Motivation** `ComputeMotivation` → [0.85, 1.20]:
   - low-ambition clubs suffer an ambient drag in low-stakes games;
   - high-stakes games lift by `1 + 0.06 × (0.5 + 0.5 × ambition)`;
   - a **rivalry floor** of up to `1 + 0.08` at derby intensity 100 (nobody
     phones in a derby);
   - the approved **10% giant-killing roll**: a low-ambition (≤ 0.7) underdog
     facing an opponent ≥ 15 reputation higher may jump to 1.20.
4. **Key players** `SelectKeyPlayers` — top 3 by attribute weight, plus the
   anchors (GK, captain, primary striker, playmaker) up to 5, **plus every
   starter with |sentiment| ≥ 20** (uncapped).
5. **Per-player performance factor** (key players) → [0.85, 1.15]: a
   consistency-width draw (width 0.02–0.32 as consistency falls), temperament
   and pressure handling divergence **only in high-stakes fixtures**, and the
   player's own sentiment.
6. **Condition coupling** — `× (0.9 + 0.4·sharpness) × (1 − 0.3·fatigue)`
   ([Ch. 14](14-condition-and-injuries.md)).
7. **Ratings** `BuildSquadRatings` — position-weighted category means (with the
   style's profile) → one Attack/Defense pair, clamped [1, 100].
8. **Taker** and penalty conversion; form factor; style + efficacy.

### Fixture context (stakes)

`squad.FixtureContext` summarises stakes: derby (`club.rivalries` intensity ≥
60), six-pointer, cup tie, dead rubber, league tier. `IsHighStakes` is what lets
temperament/pressure matter at all. Standings context is supplied by the
competition service (`StandingsContext`).

## 16.4 Live pacing (`Runner.RunLive`)

- One goroutine per world (claim guard) paces every in-progress match: one
  simulated minute per `pacing_millis`, **one transaction per minute**, events
  persisted and pushed as `match_tick` envelopes over `/ws`.
- **Resumable by design** (IM18, OPD-44): returning early never loses a match.
  The worker re-enters `RunLive` for every playable world on every poll; a
  failing step retries in place (0 s, 2 s, 10 s) before handing back; a resumed
  match logs `resuming … from minute N`. It also reconciles the crash window
  (fixture live + match completed + result not yet applied).
- `Finalize` is idempotent.
- Diagnose slow matches by comparing `live_seconds` with
  `pacing_millis × 90 / 1000` (queries in `docs/how-to/cadences-and-time.md`
  §7).

## 16.5 Commentary and the feed (IM19, OPD-45)

`match.match_events.detail.commentary` stores templates (`{player}`,
`{assist}`, `{sub}`). Both the REST feed (`GET /api/matches/:id/events`) and the
live `match_tick` envelope resolve names through one hook
(`resolveEventRefs`) from `player_id` / `related_player_id`, falling back to
"the player", "a teammate", "a substitute". A raw placeholder never reaches the
client.

## 16.6 The completion transaction — consequences

Inside the match-completion transaction (live `Finalize` or `PlayFixture`):

| Consequence | Owner | Chapter |
| --- | --- | --- |
| standings or cup advance; possible country rollover | competition | [7](07-seasons-and-rollover.md), [8](08-cups.md) |
| `MATCH_PLAYED` event | match | [3](03-events-and-explanations.md) |
| player appearances (minutes, goals, assists, 1–10 rating) | match | [15](15-match-engine.md) |
| club form EWMA | form | [17](17-form.md) |
| per-match morale swing (playing-time satisfaction) | player | [18](18-morale-and-transfer-requests.md) |
| injuries resolved from engine injury events | injury | [14](14-condition-and-injuries.md) |
| rivalry edges + trust events (`social.RecordCompletedMatch`) | social | [24](24-social-and-rivalries.md) |
| **manager match rating, supporter sentiment, fan news** (`board.RecordCompletedMatch`) | board | [23](23-board-and-job-security.md) |

After commit, best-effort pushes: `relationship_change`, dashboard updates.

## 16.7 API

| Route | Purpose |
| --- | --- |
| `GET /api/matches/:id/events` | feed (names resolved) |
| `POST /api/matches/:id/tactical` | live style change / sub |
| `/ws` `match_tick` | live minute envelopes |

## 16.8 Match stats (IM45, OPD-61)

`GET /api/matches/:id/events` also returns `stats.home`/`stats.away`: goals (the score line: stamped when completed, else counted from goal/penalty events), chances created, yellow and red cards, substitutions, penalties awarded and injuries. Events persist minute by minute, so a live match counts only played minutes. Possession, shots and xG are not emitted.

## 16.9 Crowd attendance (IM66, OPD-66)

Every match row stores `attendance`, decided when the row is created (live kickoff or `PlayFixture`) from the home club's state before the match. Capacity: `club.clubs.stadium_capacity`, filled at the club's first home match from its league's reputation (`3000 + 400 × rep`, ±30% jitter fixed per club). Fill rate: base 0.55, supporter `current_sentiment` (±0.20) and `loyalty` (±0.10), derby +0.15, six-pointer +0.05, dead rubber −0.10, cup tie −0.10, then ±0.05 noise from the fixture seed; clamped to [0.10, 1]. Same fixture, same crowd. Matches played before migration 0063 have `attendance = NULL`. Constants: `internal/match/crowd.go`. Ticket revenue from it is planned (IM68).

## Connections

- Engine internals: [Chapter 15](15-match-engine.md).
- Timing: [Chapter 4](04-world-clock-and-time.md), [Chapter 6](06-leagues-and-scheduling.md).
- Code: `internal/matchday/runner.go`, `internal/match/{service,live,live_inputs,team,persist}.go`, `internal/squad/squad.go`.
- Source: `docs/how-to/cadences-and-time.md` §§7–8, OPD-21, OPD-44, OPD-45, IM18, IM19, IM22, matchsim addenda v1.2–v1.4 Part 7/8.

---
[← Match engine](15-match-engine.md) · [Contents](the-touchline-book.md) · [Next: Form →](17-form.md)
