# STORY-03 — Crossroad Dilemmas, Dashboard Integration & PolicyBot Fallback

**Status:** Not started  
**Owner:** Unassigned  
**Sprint:** S31 (Storyline & Emergent Narrative Engine)  
**Source:** User request 2026-10-07, `docs/design/storyline-numerics.md`  
**Depends on:** STORY-02, S07-01 (dashboard), S06-05 (PolicyBot)  

---

## 1. Goal

Build the dilemma decision generator, mechanical consequence resolution engine, dashboard API feeds (`GET /api/dashboard`), decision submission handler (`POST /api/storyline/dilemmas/:id/respond`), and PolicyBot fallback execution for un-responded dilemmas.

---

## 2. Requirements & Acceptance Criteria

### 2.1 Dilemma Consequence Engine (`backend/internal/storyline/dilemma.go`)

- Implement `ResolveDilemmaChoice(ctx context.Context, dilemmaID string, optionID string) (*ConsequenceResult, error)` inside a single Postgres transaction:
  - Validate dilemma `status == 'pending'` and not expired (`expires_at_tick > current_tick`).
  - Update `status = 'resolved'`, `chosen_option_id = optionID`, `resolved_at = NOW()`.
  - Apply mechanical state deltas as specified in `docs/design/storyline-numerics.md`:
    - **Morale**: Modify individual player / dressing room faction morale (`internal/morale`, `internal/dressingroom`).
    - **Board Confidence**: Adjust Board Discipline / Financial factor scores (`internal/board`).
    - **Supporter Sentiment**: Shift sentiment (+/- 5-15 points) (`internal/club`).
    - **Finance**: Post win bonus / contract wage commitments to ledger if applicable (`internal/finance`).
  - Record `world.events` entry (`STORY_DILEMMA_RESOLVED`) with full `Explanation` payload.

### 2.2 Dashboard Feed Integration (`backend/internal/dashboard/`)
- Update `GET /api/dashboard` ([Ch. 26](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/26-dashboard-news-scouting-realtime.md)):
  - Include open pending dilemmas under the **Urgent** section (stable ID prefix `dilemma:`).
  - Item fields: `id`, `priority`, `category: "dilemma"`, `title`, `description`, `created_at`, action link `respond_dilemma`.

### 2.3 HTTP API Handlers (`backend/internal/httpapi/storyline_handlers.go`)
- `GET /api/storyline/active` - List active story arcs and open dilemmas for caller's club.
- `GET /api/storyline/dilemmas/:id` - Return dilemma details and options list.
- `POST /api/storyline/dilemmas/:id/respond {option_id}` - Process choice submission.
- Map errors cleanly: 404 Not Found, 400 Expired/Invalid Option, 403 Manager Scope Violation.

### 2.4 OpenAPI Schema Documentation (`backend/internal/apidocs/openapi.yaml`)
- Add paths and schemas for all storyline routes.
- Ensure `TestDocsCoverRouter` and `TestDocsOpenAPIValid` integration tests pass.

### 2.5 PolicyBot Fallback Execution (`backend/internal/policybot/`)
- Extend PolicyBot world tick execution pass:
  - Query dilemmas where `status == 'pending'` AND `expires_at_tick <= current_tick`.
  - Automatically invoke `ResolveDilemmaChoice` selecting the default staff stance (`Option B` or conservative stance).
  - Emit event `STORY_DILEMMA_EXPIRED_DELEGATED`.

---

## 3. Verification Plan

### 3.1 Unit & Integration Tests
- Write `internal/storyline/dilemma_test.go`:
  - Test choice resolution idempotency and state mutations across morale, board, and finances.
  - Test dashboard feed returns pending dilemmas correctly.
  - Test PolicyBot auto-resolution of expired dilemmas.
- Run Gin router openapi validation tests (`TestDocsCoverRouter`, `TestDocsOpenAPIValid`).

### 3.2 Verification Commands
```bash
gofmt -w internal/storyline/ internal/httpapi/ internal/dashboard/
go build ./...
go vet ./...
go vet -tags integration ./internal/... ./pkg/...
go test ./internal/storyline/... ./internal/httpapi/...
```

---

## 4. Delivery Evidence Checklist (When Done)

- [ ] `ResolveDilemmaChoice` transaction engine built & tested
- [ ] Dashboard `GET /api/dashboard` returns pending dilemma items
- [ ] HTTP routes registered and documented in `openapi.yaml`
- [ ] PolicyBot fallback execution wired for expired dilemmas
- [ ] All verification tests passing
