# Chapter 25 — PolicyBot and absence mode

Touchline never waits for a manager. **PolicyBot** is the assistant that acts
for AI-run clubs and for human managers who are away — always through the
**same command handlers** a human uses (Tech Plan §10).

## 25.1 Two kinds of bot rows

| Bot | Row | Purpose |
| --- | --- | --- |
| **Club policy bot** | `manager.managers` with `is_policy_bot = TRUE` and a `current_club_id` | runs an AI club; retires (`status = 'retired'`) when a human accepts that club ([Ch. 22](22-managers-and-job-offers.md)) |
| **Absence bot** | one per world, `is_policy_bot = TRUE`, club-less, unemployed (`uq_managers_world_policy_bot`) | executes delegated actions for away humans; created lazily (`GetOrCreateAbsenceBot`, name "PolicyBot") |

Every bot action passes `Actor{ManagerID: botID, IsPolicyBot: true}`, so events
and audit columns record `policy_bot`.

## 25.2 Attendance

```
attended = last_activity_at IS NOT NULL AND last_activity_at ≥ previous club fixture's scheduled_at
```

- `last_activity_at` is written by the authenticated-request heartbeat
  (`TouchActivity`), persisted at most **once per hour** per manager.
  (Lineup timestamps are deliberately *not* used — the bot writes lineups too.)
- No previous fixture → treated as present.

## 25.3 Becoming "away"

| Path | Effect |
| --- | --- |
| **Auto** | 3 consecutive unattended fixtures (`MissedFixtureThreshold`) → `away_since = now()`, `away_auto = TRUE`, `MANAGER_AUTO_AWAY`. One attended fixture resets the streak. |
| **Explicit** | `PUT /api/managers/me/absence {"away": true}` → immediate delegation, `MANAGER_AWAY_EXPLICIT`; `{"away": false}` clears it, `MANAGER_AWAY_CLEARED`. Explicit toggles also clear `away_auto`. |

While unattended (even before auto-away) the bot fills match inputs; once
away, training and bid responses are delegated too.

## 25.4 What the bot does, and when

| Hook | When | Action |
| --- | --- | --- |
| `EnsureMatchInputs(fixtureID)` | top of `PlayFixture` and before each kickoff | lineup + tactics for absent managers |
| `EnsureTraining(worldID)` | weekly, before `Training.ApplyWeekly` | write a plan only if the club has none |
| `RespondToBidsForAbsent(worldID)` | daily, before the market sweep | answer pending bids where the away club sells |

### Defaults (a saved policy overrides any of them)

| Decision | Default | Options |
| --- | --- | --- |
| Squad | `best_eleven` | `rotate`, `best_fitness` |
| Tactics | `keep` | set a style (+ its default formation) |
| Transfers | `accept_above 120%`, `sell_floor 100%`, `auto_counter` | per-policy percentages |
| Training | `dna` | a fixed archetype |

**Bid resolver** (seller side only): fee ≥ accept_above × valuation → accept;
< sell_floor × valuation → reject; else counter **at the accept threshold**
(never lower).

**Training from DNA** (`competitive_ambition`): ≥ 80 attacking, ≥ 60 physical,
≥ 40 technical, ≥ 20 defensive, else recovery.

## 25.5 Returning

`GET /api/managers/me/absence-summary` lists every automated action taken while
away (polling; no realtime summary yet).

## 25.6 Shared cores

Club-scoped cores let a club-less bot act without ownership checks:
`tactics.SetLineupForClub` / `SetTacticsForClub`,
`training.SubmitPlanForClub`, `transfer.RespondToBidForClub`. Human routes
validate ownership, then call the same core.

## 25.7 Out of scope / open items

Contract renewals, scouting, youth and buyer-side delegation; unifying AI-club
runtime with PolicyBot; a tighter attendance clock than the 1-hour heartbeat;
blending the bid fee into counters.

## Connections

- Bot-run clubs are where job offers come from: [Chapter 22](22-managers-and-job-offers.md).
- Code: `internal/policybot/{model,resolver,service,store}.go`.
- Source: `docs/design/policybot-numerics.md`, S06-05, migration 0042, 0060.

---
[← Social](24-social-and-rivalries.md) · [Contents](the-touchline-book.md) · [Next: Dashboard & surfaces →](26-dashboard-news-scouting-realtime.md)
