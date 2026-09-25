# IM12 — Squad dynamics read: influencers + full profiles, and a faster, no-op-exploding read

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (squad dynamics / dressing room)
**Source:** Product decision (manual session) — follow-on to the S09-02
dressing-room / squad-dynamics engine (OPD-30).
**Depends on:** S09-02 (the graph + `internal/faction` engine and
`GET /api/clubs/:id/dynamics`), migration 0047 (player-edge indexes), S09-01
(player personalities), S07-01/S05-01 roster reads (attribute means + squad
role via `player.contracts`). Claims the reserved OPD-31 slot.

## Delivery evidence

- Wire-change: `internal/faction/model.go` — `TierView` gains
  `Profile *PlayerProfile json:"profile,omitempty"`; `PlayerProfile` (id, name,
  position, age, nationalities, squad_number, status, squad_role,
  is_academy_product, leadership, sociability, emotional_volatility, loyalty,
  `PlayerAttributes`, overall) built by pure `profileOf(MemberProfile)`, overall
  = `squad.PositionalOverall` capped 99. Faction→squad import is cycle-safe.
- `internal/faction/generate.go` — `MemberProfile` extended (squad number,
  status, squad role, six attribute means).
- `internal/faction/store.go` —
  - extended `memberProfiles` query: lateral squad role + the six attribute
    category means via `AVG(...) FILTER` in one GROUP BY (no per-category
    round-trips); `player.players` rows deduped.
  - `upsertEdges` rewritten as a **batch unnest**: one insert statement per
    recipient over parallel `from/to/kinds/strengths/trusts` arrays, preceded
    by one stale-edge delete per recipient (`sentiment = strength` preserved).
  - `ensureSquadRelationships` is now a **no-op guard**: `memberHash` (FNV-1a
    64 over the world seed + each member's generation inputs — player id,
    nationality(s), age, position, academy product, personality — sorted by
    player id) is compared against `social.squad_graph_state.member_hash`;
    a match skips all upserts.
- `internal/faction/service.go` — `GetDynamics`/`OnPlayerSold` feed precomputed
  profiles into `squadSnapshot` (single identity read per club); `influencerTiers`
  filters `TierOther` and attaches the profile to each returned tier.
- Migration `0053` (world/social slice) created `social.squad_graph_state`
  (`world_id, club_id, member_hash, updated_at`, PK `(world_id, club_id)`).
- Tests — `hash_test.go` (hash determinism, seed/membership sensitivity,
  enrichment-insensitivity, profileOf, influencer filtering); store +
  `cmd/api` integration tests extended (influencers-only tiers, profile +
  overall checks, first tier = team leader) plus `TestGetDynamicsSecondReadIsNoOp`
  (fingerprint freshness: a second read within the window leaves the graph and
  `last_interaction_at` untouched).
- Docs: `backend/internal/apidocs/openapi.yaml` — `SquadTier.tier` enum now
  `[team_leader, highly_influential, influential]` (no `other`), `profile` ref,
  and new `PlayerProfile` / `PlayerAttributes` schemas; `docs/product_manager.md`
  OPD-31.
- Verify: `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` all
  green (docs-coverage tests included); touched files `gofmt`-clean.
  Integration tests are compile-gated locally (live Postgres required).

## Recorded decisions

- **Influencers only on the wire.** The dynamics read returns exactly the
  dressing-room hierarchy the frontend's influencers panel renders —
  `team_leader` / `highly_influential` / `influential`; the `other` tail is
  dropped server-side, so the client never ships or masks a hoard of
  filler rows.
- **Full profile rides with the tier.** Each returned tier embeds the player's
  complete read-model (identity, uniform, personality, attribute means,
  position-weighted overall), so one ownership-gated request draws the whole
  panel — no per-player roster follow-ups.
- **A fingerprint makes regeneration advisory, not blind.** The stored FNV-1a
  hash covers exactly the inputs that determine the edge set (seed +
  membership composition + personality); name, uniform, attributes, loyalty,
  and role are excluded, so cosmetic or form changes don't churn the graph. A
  hit skips the (potentially ~250k-row) upsert; a miss regenerates
  deterministically and updates the fingerprint in the same transaction —
  correctness never depends on the cache.
- **Batch, don't loop.** Recipient-by-recipient write loops were replaced with
  one unnest insert per recipient (arrays over a single statement) plus one
  stale-edge delete; the roster query aggregates the six attribute means and
  the squad role in a single pass. The reads were already covered by
  migration 0047's partial indexes — no new relationship index is needed
  (the `squad_graph_state` PK is the only new index).
- **The one-write-per-request norm holds.** Skip-on-fingerprint only avoids
  idempotent writes; every actual regeneration still upserts canonically and
  publishes nothing.