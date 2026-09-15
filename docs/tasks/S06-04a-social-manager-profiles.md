# S06-04a — Manager profile pages

**Status:** Implemented  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §48; technical plan §§6, 16; OPENCODE.md  
**Depends on:** S06-03

## What to do

Assemble the manager profile page for any manager in the caller's world: career stats, active club, trophy cabinet, head-to-head history against the viewing manager, trust score, and the relationship edges attached to the manager and their club.

## Acceptance criteria

- A profile read assembles every piece from existing world data: career W-D-L from `manager_history` × completed fixtures, trophies from `club.club_history`, trust from the append-only `social.trust_events` log, head-to-head from completed fixtures between the two clubs, rivals from `social.relationships`.
- Profiling is strictly world-scoped: cross-world and unknown managers read as 404.
- Every manager already has a trust baseline credited the day the surface ships.

## Delivery evidence

- **Migration `0041`** (`social`): `idx_messages_sender` (messaging rate limiting, S06-04b) + `social.trust_events` backfill from the S06-03 journal (`backfilled:` reason prefix; 0-delta reassures skipped) so all managers start with a seeded trust baseline.
- **`internal/social` build-out** — replaces the S06-02-era stub with a real service (`NewService(pool, bus)`), dependency-light (no internal imports → the S06-04c match hook can call back in without cycles):
  - `profile.go` (`Profile`, `ClubRef`, `CareerSummary`, `Trophy`, `H2HRecord`, `RivalEdge`), `store.go` (queries: manager base + person name, club ref, career summary with date-overlap attribution, trophies, `SUM(delta)` trust, head-to-head, both-direction rival edges), `service.go` (`GetManagerProfile(ctx, worldID, viewerID, targetID)`, world boundary → `ErrManagerNotInWorld`).
  - `social.go` rewritten: shared `Relationship`/`Message`/`Promise` models aligned to the DB; the old stub `Service` interface removed.
- **HTTP surface** (documented in `internal/apidocs/openapi.yaml`, docs-coverage green, 58 routes): `GET /api/managers/:id/profile` → `ManagerProfile` (unknown/cross-world 404, malformed 400).
- **Wiring**: `internal/app` builds the social service; `internal/httpapi` gains `Options.Social`; the integration harness mirrors it.
- **Harness hygiene**: the `testdb` truncate list now covers the social schema + `player_condition`/appearances/transfer-requests (previously untruncated tables bled between tests).
- **Tests** (compile-checked; need Postgres to run): `internal/social/social_integration_test.go` (full profile, h2h vs other, world boundary, no-h2h) on the transfer-market world fixture; `cmd/api/social_integration_test.go` HTTP round-trip (200 full profile, self-profile, 401/404/400, cross-world isolation).
- **Docs**: `docs/design/social-numerics.md` (source of truth for trust/messaging/rivalry numbers, proposal awaiting PM tune sign-off); `product_manager.md` OPD-03 social-trust slice row; migrations README 0041 row; umbrella S06-04 task split into 4a/4b/4c.

## Notes

- Career stats use date-overlap attribution (`scheduled_at ∈ [start_date, COALESCE(end_date, ∞))`) over `manager_history`, so rehires can double-count legacy fixtures in rare edge cases — acceptable for a profile read, noted for a future denormalized career table.
- AI-managed manager profiles resolve with a generic "AI Manager" label (no person record). Rival edges for AI clubs are club-level only (S06-04c).