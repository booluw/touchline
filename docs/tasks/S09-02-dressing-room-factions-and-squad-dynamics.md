# S09-02 — Implement graph-derived squad dynamics and dressing room factions

**Status:** Implemented  
**Sprint:** 09 — Relationship-driven football world  
**Source:** PRD §15; technical plan §§6, 16; OPENCODE.md  
**Depends on:** S06-04, S09-01

## What to do

Build the squad dynamics engine using graph CTE queries over `social.relationships`. Group players into dressing room hierarchy tiers (Team Leaders, Highly Influential, Influential, Other) and social factions (e.g. core veterans, foreign cohort, youth alliance). Model morale contagion across connected player graph edges, so selling a team leader's best friend or mistreating an influential player triggers dressing room unrest.

## Acceptance criteria

- Squad hierarchy and social groups are dynamically computed from `social.relationships` graph edges without redundant denormalization.
- Team leaders exert strong morale influence over their social cluster; mistreating a leader degrades morale across their connected faction.
- Managers can inspect squad hierarchy diagrams in the Nuxt UI showing social groups, dressing room cohesion, and overall manager support.
- Severe squad unrest (e.g. selling a team leader) can trigger a delegation of players demanding a board meeting or requesting transfer en masse.
- Morale contagion events emit auditable `SQUAD_UNREST_TRIGGERED` events with `Explanation` objects tracing causal relationship chains.

## Delivery evidence

- **AC1 — hierarchy/factions from the graph, no denormalization:** `internal/faction`
  computes tiers from graph centrality and factions as connected components of
  the player↔player `social.relationships` subgraph via an undirected recursive
  CTE (`store.componentRoots`, root = `MIN(node)`), with a pure union-find
  fallback in the engine. No faction/hierarchy column is persisted; reading
  self-heals clubs whose graph predates the feature. Migration
  `0047_player_relationship_graph` only adds partial forward/reverse player-edge
  indexes. Covered by `TestGenerateEdgesDeterministic`,
  `TestFactionsFromRootsAndFallback`, `TestHierarchyLeaderAndTiers`,
  `TestGetDynamicsGeneratesIdempotentCanonicalGraph`.
- **AC2 — leader influence / contagion degrades the faction:** `Engine.Contagion`
  runs a deterministic bounded BFS from the action epicentre over the graph and
  `Engine.UnrestFrom` escalates severe contagion (≥60 board meeting, ≥80
  en-masse transfer requests). `Service.OnPlayerSold` is invoked BEFORE the
  ownership flip so a sale is evaluated against the pre-sale room, and
  `FormerTeammateEdges` records the bonds. Covered by
  `TestReleasedFromSquadBoundedContagion`, `TestUnrestOnlyAfterThreshold`,
  `TestEnMasseDemandAtTopSeverity`, `TestOnPlayerSoldEmitsUnrestAndFormerTeammates`.
- **AC3 — Nuxt hierarchy diagram (DEFERRED):** not built. The backend read model
  `GET /api/clubs/:id/dynamics` already returns `tiers`, `factions`, `cohesion`,
  `manager_support` and `dressing_room_mood`, so the UI is a pure follow-on.
- **AC4 — severe unrest → delegation:** `SQUAD_UNREST_TRIGGERED` carries the
  demand (`board_meeting` / `en_masse_transfer_requests`), severity and affected
  players, surfaced back through the read model's `unrest` field.
  `TestOnPlayerSoldEmitsUnrestAndFormerTeammates` +
  `cmd/api/faction_integration_test.go` assert the round trip.
- **AC5 — auditable events with `Explanation`:** every engine outcome, faction,
  contagion and unrest carries a `pkg/explanation.Explanation`; the event stores
  it in `world.events.explanation`.
- **Numerics:** `backend/docs/design/squad-dynamics-numerics.md` (proposal
  constants; recalibration is a data-only change).
- **Reserved seams:** live (non-transfer) management-action triggers are
  unit-tested through the full action matrix but have no emitting call sites
  yet; the UI (AC3) is deferred.
- **Verification:** `go build ./...`, `go vet ./...`, `go vet -tags integration
  ./...`, `go test -race ./...` and `TestDocsCoverRouter` green; the DB
  integration suites run in CI (`./internal/faction/...` added to the CI
  integration job) as local Docker is absent.
