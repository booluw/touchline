# S01-03 — Establish the explanation-object contract

**Status:** Done  
**Owner:** opencode agent  
**Sprint:** 01 — World foundation and event spine  
**Source:** PRD §§9, 16–18, 54, 81; technical plan §8  
**Depends on:** S01-02

## What to do

Make the existing explanation model the shared contract for scored or consequential decisions. Define its event/API serialization and the rendering boundary so UI and news consume stored reasons instead of recreating them.

## Acceptance criteria

- The common Explanation and factor types are stable, documented, and serializable in event payloads and state-changing API responses.
- A decision can identify its subject, score, and contributing labeled deltas.
- Explanations are persisted with their causing event; a consumer can render them without recalculating the decision.
- Tests demonstrate round-trip persistence/serialization for an explanation.
- No endpoint or client contract requires the client to derive why a server decision occurred.

## Delivery evidence

- **Shared contract package** `backend/pkg/explanation`: `Explanation{Subject, Score, Factors[]Factor{Label, Delta}}` (moved out of `internal/world` and canonicalized). Package doc is the contract spec: pinned wire shape `{"subject","score","factors":[{"label","delta"}]}` with all fields always emitted (additive-only), scoring semantics (factors authoritative, not required to sum to score), persistence seam (`world.events.explanation` JSONB), and the rendering boundary (consumers render stored reasons, reference `Render()`).
- **Helpers:** `New(subject, score)` + `Add(label, delta)` (order-preserving), opt-in `Validate()` (reports a summed-deltas/score mismatch when called — not auto-enforced per confirmed decision), `Render() []string` producing the PRD §54 canonical lines (`Playing time: -18`).
- **Wire contract pinned** by `explanation_test.go`: exact-JSON golden test (key order + always-emitted fields), `encoding/json` round-trip, embedding inside an event payload struct and an API-response envelope struct (so clients receive the reason directly), order preservation, empty/zero/narrative cases, `Validate()`, and `Render()` goldens. `go test ./pkg/explanation/` passes.
- **Typed round-trip through the event log**: `TestPublishConsumeRoundTrip` (integration) now publishes an `Event` whose `Explanation` carries the shared type, verifies the dispatched event decodes back to the same `explanation.Explanation`, and verifies the persisted `world.events.explanation` column decodes to the identical type — a consumer can render it without recomputing. Passes against real Postgres (`go test -tags integration ./pkg/eventbus/`).
- **Decision recorded**: resolution logged as OPD-12 in `docs/product_manager.md` (package ownership, pinned wire shape, no score-sum enforcement, render boundary, S02+ obligations).
- **Verification:** `go vet ./...`, `go build ./...`, `go test ./...`, `go test -race ./...`, and `-tags integration -race ./pkg/eventbus/` all green. No schema changes (column already exists), no new CI job needed.
- **Moved file:** `internal/world/explanation.go` deleted (was unreferenced outside its own definition).
