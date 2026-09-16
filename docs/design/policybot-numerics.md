# PolicyBot & Absence Mode Numerics (S06-05)

Source of truth for the implemented numbers behind absence mode delegation and
PolicyBot fallback execution: attendance, away thresholds, heartbeat cadence,
and the per-decision-type assistant defaults. The product task
(`docs/tasks/S06-05-absence-mode-and-policybot.md`, PRD §§45–47, 71–73,
technical plan §10) publishes intent; this document fixes the implemented
values and binds them to the `manager.*` schema added by migration `0042`. All
numbers here are **proposal** data until PM tuning sign-off — every block is
config expressed as constants in `internal/policybot` (never freeform in engine
code), like `squad.DefaultPositionWeights`.

## 1. Attendance anchor

A manager is **attended** for a given matchday if the manager ever touched the
game on or after their club's *previous* scheduled fixture kickoff:

```
attended = last_activity_at IS NOT NULL
           AND last_activity_at >= prevClubFixture.scheduled_at
```

- `last_activity_at` lives on `manager.managers` and is written by the
  authenticated-request heartbeat (`TouchActivity`). It is intentionally *not*
  `club.club_lineups.updated_at`: the bot itself writes lineups, so using that
  column would mask absence as attendance (the classic repo-driven bug this
  slice avoids).
- `prevClubFixture.scheduled_at` is the latest scheduled fixture for the club
  before the current one; unknown (first matchday, no fixture yet) → the
  manager is treated as present by default (below-threshold streak is harmless).

## 2. Auto-away activation

- `MissedFixtureThreshold = 3` consecutive unattended fixtures (config const in
  `internal/policybot/model.go`).
- On the `count`-th consecutive miss, `away_since = now()`, `away_auto = TRUE`.
- A single attended fixture resets `consecutive_missed = 0`.
- While unattended, the bot runs the club's delegated handlers (`EnsureMatchInputs`);
  once auto-away flips, training + bid-response delegation also engage.

### 2.1 Explicit away

- PUT `/api/managers/me/absence {away: true}` sets `away_since = now()`,
  `away_auto = FALSE`, resets `consecutive_missed = 0`.
- Toggling **on** performs an immediate change transfer to the bot (delegation
  starts right away, not waiting for the next fixture scan).
- Toggling **off** (`away: false`) clears `away_since`; delegation reverses
  immediately. `away_auto` is also cleared on any explicit toggle (human
  override wins).

## 3. Heartbeat cadence

- `TouchActivity` fires on every authenticated request.
- Persisted at most once per **1 hour** per manager via
  `make_interval(hours => $2)` in the `UPDATE ... WHERE last_activity_at IS NULL
  OR last_activity_at < now() - make_interval(hours => $2)`. This bounds write
  volume without new moving parts; the throttle is *not* a duration of
  refreshment — it is a cheap dedupe. If a manager wants a finer attendance
  clock it is a one-line cadence change here, not a schema change.

## 4. The delegated actor

- One club-less, unemployed, forever-inactive manager per world:
  `manager.managers` row with `is_policy_bot = TRUE`, `current_club_id = NULL`,
  uniquely indexed by `uq_managers_world_policy_bot` (partial unique on
  `(world_id)` where `is_policy_bot AND current_club_id IS NULL`).
- `GetOrCreateAbsenceBot(ctx, worldID)` lazily materialises the row on first
  delegation (`manager.managers.world_id` set; name "PolicyBot").
- Every bot action passes `Actor{ManagerID: botID, IsPolicyBot: true}` so the
  shared cores (`tactics`, `training`, `transfer`) audit `updated_by_actor_type
  = 'policy_bot'` and skip club-scope gating.

## 5. Assistant defaults (no saved policy)

Delegation **always** acts; a saved policy only overrides the default.

| Decision | Mode | Default | Meaning |
| --- | --- | --- | --- |
| Squad rule | best_eleven | `best_eleven` | `SelectStartersWithLineup` on the strongest players |
| | rotate | `rotate` | `SelectStartersRotated` (rotation-aware) |
| | best_fitness | `best_fitness` | `SelectStartersByFitness` |
| Tactics style | keep | `keep` | never touch formation/style |
| | style | (chosen) | set style + `AllowedFormations(style)[0]` |
| Transfer | sell_floor_pct | `100` | reject bids under 100% × ×valuation |
| | accept_above_pct | `120` | accept bids ≥ 120% × valuation |
| | auto_counter | `true` | else issue a counter at midpoint, clamped to accept-above |
| Training | archetype | `dna` | club DNA → archetype (below) |

### 5.1 Transfer math (resolver)

- Only absent-manager clubs acting as **seller** with `status = 'pending'`.
- `fee ≥ accept_above_pct × valuation` → **accept** (`transfer.RespondAccept`).
- `fee < sell_floor_pct × valuation` → **reject** (`RespondReject`).
- otherwise → **counter** (`RespondCounter`). The counter offer is set to the
  accept threshold (`accept_above_pct × valuation`), never below it; if a
  counterpart asking price is supplied it may only *raise* the counter, never
  lower it (`askingPrice` is currently nil, so the bot counters at the accept
  threshold and never over-pays on an anchor).
- All numeric comparisons against `player.players.market_value` per the S06-01
  valuation recipe (see `docs/design/transfer-numerics.md`); pence fixed-point
  throughout (`int64`).

### 5.2 Training archetype (resolver)

`TrainingArchetype(club)` maps `competitive_ambition` (from the club DNA/
board-expectations source the training slice already reads) to an archetype; a
plan is only written when the club has **no** active plan:

| competitive_ambition | archetype |
| --- | --- |
| ≥ 80 | attacking |
| ≥ 60 | physical |
| ≥ 40 | technical |
| ≥ 20 | defensive |
| else | recovery |

## 6. Events

Bot-driven actions emit the same auditable domain events as human commands
(`eventbus.Publish`), payloads tagged with the bot actor. Dedicated absence
events:

- `MANAGER_AWAY_EXPLICIT` (on toggle on), `MANAGER_AWAY_CLEARED` (toggle off).
- `MANAGER_AUTO_AWAY` (streak threshold crossed). Backfill on the manager's
  return summary log surfaces **all** automated actions (`GetAbsenceSummary`).

## 7. Delegation seams

- `match.AbsenceDelegator` (defined in `internal/match`):
  `EnsureMatchInputs(ctx, fixtureID)`. Called at the top of `PlayFixture`
  (single-fixture path) and inside the `KickoffMatchday` loop before
  `kickoffFixture`. Idempotent — skips non-`scheduled` fixtures and clubs with
  no away manager.
- `EnsureTraining(ctx, worldID)` runs on the weekly tick, *before*
  `Training.ApplyWeekly` (see `internal/app/app.go`).
- `RespondToBidsForAbsent(ctx, worldID)` runs on the daily tick, *before*
  `Transfers.DailyTick` (so the bot answers before the market re-opens).
- Club-scoped cores extracted so bots (club-less) can act without ownership:
  `tactics.SetLineupForClub`/`SetTacticsForClub`, `training.SubmitPlanForClub`,
  `transfer.RespondToBidForClub`. Human paths now validate
  ownership/scope then delegate to the same core.

## 8. Explicitly out of scope (S06-05)

- Wage/finance auto-renew, scouting/media/youth delegation — future slices.
- Realtime push of "returning from absence" summaries (polling via
  `GET /api/managers/me/absence-summary` for now).
- AI-club runtime unification (unmanaged clubs still use their own
  personality-weighted path; only away human clubs go through PolicyBot here).
- Buyer-side bid automation (only the away manager as **seller** is delegated).

## 9. Open items

- The 1-hour heartbeat dedupe means a manager active minutes before kickoff
  still counts as attended — cadence likely tightens after observed player
  behaviour.
- Transfer counter currently fixes the offer at the accept threshold (or the
  counterpart's asking price when one is supplied) and ignores the initial bid
  fee as a midpoint anchor; a follow-up may blend the bid fee in once the
  negotiation UX surfaces the asking price to the API.
- Auto-away currently activates only on a fixture-scan (daily kickoff path);
  an RPC "check absence now" could tighten it for the always-on servers.