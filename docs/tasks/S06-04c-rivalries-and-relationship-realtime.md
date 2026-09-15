# S06-04c — Rivalries and relationship-change realtime

**Status:** Implemented  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§48, 66; technical plan §16; OPENCODE.md  
**Depends on:** S06-04a

## What to do

Auto-track rivalry and trust graph edges from completed head-to-head fixtures, and push relationship-change alerts over the realtime socket.

## Plan

- **Rivalry hook** — `internal/social` `RecordCompletedMatch(ctx, tx, worldID, fixtureID, homeClubID, awayClubID, homeGoals, awayGoals)` invoked inside `match.Finalize`'s completion tx (mirrors `RecordMatchAppearances`):
  - **club↔club** `rivalry` edge: every completed fixture (AI or human managed).
  - **manager↔manager** `rivalry` edge: only when **both** clubs' current managers are human (`is_policy_bot = false`). AI managers never carry personal edges; the match hook stays club-level for them.
  - Strength accumulates by result decisiveness × derby/big-match modifier, repeated encounters escalate, clamp ±100; `last_interaction_at` on every touch.
  - Trust: win `+5` / loss `−5` for a human-managed side.
  - `world.events` row + return changed → caller publishes `relationship_change`.
  - `ReconcileRivalries(ctx, worldID)` — lazy backfill scan for completed fixtures missed by the hook.
- **Wiring**: `match.Service.WithSocial`; no-op when nil; `app.go` sets it. `pkg/realtime` gains `EventRelationshipChange`.
- **HTTP**: `GET /api/relationships` for the manager's full edge set (personal + active club), documented in openapi.
- **Numerics**: `docs/design/social-numerics.md` rivalry accumulation — constants live in `internal/social/rivalry.go`.
- **Tests** (integration): same-world fixtures produce escalating club↔club always + manager↔manager only for human↔human; AI never gets personal edges; reconcile backfill; service + match-side realtime publish; HTTP round-trip.

## Delivery evidence

- **`internal/social/rivalry.go`**: `RecordCompletedMatch(ctx, tx, …)` (tx-scoped, called with the completion tx) grows the **club↔club `rivalry` edge on every fixture** and the **manager↔manager edge only when both clubs' current managers are human** (`is_policy_bot = false`); edges stored **canonically** (`entity_a_id ≤ entity_b_id` by UUID) so the partial-unique constraint never holds both orientations; `strength = (base + 2·min(gd,5)) × big-match-mult + repeat-bonus`, stale-touch decay (−~20% past 45 days), clamp ±100, `last_interaction_at` stamped every touch. Constants: `RivalBaseStrengthClub=10`, `RivalBaseStrengthManager=8`, `RivalBigMatchMultiplier=2` (league + same non-empty country), `RivalRepeatBonus=3`, `RivalMaxStrength=100`, `RivalStaleDays=45`.
- **Trust**: each human side independently gets `+5`/`−5` (`social.trust_events`, reasons `match_win`/`match_loss`); AI managers and draws write nothing. `trust_score` stays the on-the-fly `SUM(delta)`.
- **Outbox + realtime**: every completion records `RELATIONSHIP_CHANGED` (`RelationshipPush` payload) via `eventbus.WriteTx` inside the completion tx; `pkg/realtime` gains `EventRelationshipChange = "relationship_change"`; after its commit the match caller triggers `PublishRelationshipChange` (best-effort, world-scoped, nil-safe) through the `Service.WithRealtime` broker.
- **Match hook**: `match.Service.WithSocial(soc)` wired in `internal/app` and the integration harness; invoked in BOTH completion paths — `Finalize` (live runner) and `PlayFixture` — after the players hook, inside the completion tx, with the post-commit push.
- **Backfill**: `ReconcileRivalries(ctx, worldID)` reruns missed completed fixtures in per-fixture transactions, driven weekly by the scheduler (`app.go` weekly pass); idempotent — a fixture whose club edge already carries `last_interaction_at ≥ ended_at` is never replayed.
- **HTTP surface** (documented in `internal/apidocs/openapi.yaml`, docs-coverage green, 62 routes): `GET /api/relationships` returns `{world_id, edges}` — the caller's personal edges + their active club's edges, via the same orientation-agnostic two-sided read the profile rivals tab now shares (`relationshipEdges`). 401 unauthenticated; 404 on a cross-world/unknown manager boundary.
- **Tests** (integration): `internal/social/rivalry_integration_test.go` (club edge always + human pair manager edge; strength accumulation + big-match escalation + repeat bonus; stale decay; ±100 clamp; two-sided `rivalEdges` resolution for both actors; `ListRelationships` boundary; realtime `relationship_change` via `LocalBroker`; `ReconcileRivalries` backfill + idempotency); `internal/match/rivalry_hook_integration_test.go` (PlayFixture → edges + trust + outbox row + post-commit push); `cmd/api/social_relationships_integration_test.go` (HTTP round-trip incl. 401, two-sided read over the wire, sibling-world isolation/empty edge list).

## Notes

- **Big-match predicate**: `competition_type = 'league'` + both clubs' country non-empty and equal — league fixtures boost escalation today. Cup semi/final stages are not yet seeded (only leagues; fixtures carry `matchday` but no round) — the predicate is parameterized so knockout stages activate automatically once a `round` column + cup seeding land in a later scheduling sprint.
- **Reconciality**: once a pair's club edge reflects a fixture (`last_interaction_at ≥ ended_at`), later manager changes only start personal tracking from their next fixture (no retroactive personal edges) — replaying would double-count strength.
- Delivery is world-scoped (OPD-19), like messaging: the push is best-effort and the graph read (profile / `GET /api/relationships`) stays authoritative.