# S04-03 — Ship the MVP matchday event-feed experience

**Status:** Done  
**Sprint:** 04 — Deterministic football competition  
**Source:** OPENCODE.md; technical plan §§5, 9, 11, 18; PRD §55  
**Depends on:** S04-02, S02-04

## What to do

Render the server-produced match event list as the approved text/live-commentary feed. Stream/replay the same events over the single socket for live viewing while retaining quick-result access to the same simulation outcome.

## Acceptance criteria

- The match screen renders ordered key events, including goals, cards, injuries, substitutions, and half/full time when produced by the engine.
- Live updates use the single multiplexed WebSocket and remain correct when replayed at controlled pacing.
- Quick result and live viewing consume the same persisted simulation event list.
- The client never calculates match outcomes or events.
- No 2D pitch/canvas implementation is included in MVP scope.

## Technical plan

Live pushes use the S02-04 realtime envelope `match_tick` (reserved per matchsim
addendum v1.1 §8). The authoritative event list is `match.match_events`; REST and
live push both serve those exact rows.

### Live push (worker side)

- `pkg/realtime/event.go` — add `EventMatchTick = "match_tick"` constant.
- `internal/match/service.go` `PaceMinute` — return the minute's freshly
  persisted casted rows (`[]*MatchEventRow`) so the publisher never re-reads or
  fabricates events (the envelope's events ≡ the DB rows).
- `internal/matchday.Runner.WithRealtime(broker)` — optional fan-out fan (same
  pattern as `WithStandingsContext`). `RunLive` publishes `match_tick` after each
  `PaceMinute` and after `Finalize`.
- `match.MatchTickPayload` JSON shape:
  `match_id, fixture_id, minute, status, home_club_id, away_club_id,
  home_score, away_score, events[{sequence, minute, event_type, club_id,
  player_id, related_player_id, detail}]`.
- Running score is server-computed (`match.Service.ScoreLine` — aggregates
  `goal`/`penalty_scored` per club from `match.match_events` while live; the
  stamped `matches.home_score/away_score` once completed). The client never
  calculates outcomes.
- `MatchEventRow.Detail` becomes `json.RawMessage` so `detail.commentary` is
  emitted as a JSON object (not base64).

### REST (quick result + live catch-up)

- `GET /api/fixtures/:id` (auth, world-scoped) — fixture header + `match{id,
  status, minute, home_score, away_score}`. Resolves the match by fixture and
  verifies the fixture is in the caller's world.
- `GET /api/matches/:id/events` (auth, world-scoped) — ordered `match_events`
  rows (reuse `GetMatchEvents`).
- Wire `matchSvc` into the API server struct (`main.go` + test helper) and the
  two routes in `router.go`.

### Frontend (Nuxt)

- `stores/match.ts` — `load(fixtureId)` = header GET then events GET
  (catch-up); `connect(fixtureId)` registers one idempotent `match_tick`
  handler on the shared `useSocket()` session, appends + dedupes by `id`,
  updates minute/score/status. Dedupe makes replay at controlled pacing and
  reconnect safe.
- `pages/matches/[fixtureId].vue` — header (clubs, running minute, score) +
  ordered feed rendered by `event_type` from `detail.commentary`. No 2D pitch.
- `competitions.vue` fixture rows link to the match screen.

## Delivery evidence

- **Live push:** `internal/matchday/runner_feed_integration_test.go` →
  `TestRunnerPublishesMatchTickFeed` drives `RunLive` on a `LocalBroker`
  recorder and asserts every envelope is `match_tick` scoped to the runner's
  world; one tick per paced minute plus a completion tick (91 per match at the
  10ms test cadence); the first tick is minute 1 `in_progress`, the last is
  minute 90 `completed`; each tick's events match `GetMatchEvents` on
  id/sequence/minute/event_type; the final tick's score equals `ScoreLine`
  (server-computed running score, never client-derived). `PaceMinute` now
  returns the exact persisted rows it inserted, so the envelope's events ≡ the
  DB rows (no read-back, no fabrication).
- **REST:** `cmd/api/match_feed_integration_test.go` →
  `TestMatchFeedFixtureHeader` (club names + match view `completed`/minute
  90/score 2-1), `TestMatchFeedEventsEndpoint` (9 ordered events: goals, cards,
  sub, injury, half/full time; `detail.commentary` as a JSON object; 401
  unauthenticated), `TestMatchFeedScopedToCallerWorld` (fixture/match outside
  the caller's world → 404 on both endpoints). Served by `handleGetFixture`
  (`GET /api/fixtures/:id`) + `handleGetMatchEvents` (`GET /api/matches/:id/events`).
- **Frontend:** `pnpm lint` ✓ + `pnpm run typecheck` ✓. `stores/match.ts`
  (`load`/`connect`/`watch`, dedupe-by-`id` catch-up then live append),
  `pages/matches/[fixtureId].vue` (header score/minute/status badge + ordered
  feed from `detail.commentary`), `competitions.vue` fixture rows link to the
  match screen. No 2D pitch (out of MVP scope).
- **Green:** full backend suite (tag `integration`, `-p 1`) ✓; `go build` +
  `go vet` ✓. Worker wires the runner via `WithRealtime(realtimeBroker)`;
  worker→browser fan-out crosses pods on the shared Redis broker exactly like
  `world_tick` (S02-04).
