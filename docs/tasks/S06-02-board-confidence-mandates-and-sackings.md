# S06-02 — Implement board confidence, structured mandates, and manager sackings

**Status:** Implemented (proposal numerics pending PM sign-off)
**Sprint:** 06 — Multiplayer market and board consequences
**Source:** PRD §§9, 54; technical plan §§6, 8, 16; OPENCODE.md
**Depends on:** S04-01, S05-02
**Proposal source of truth:** `docs/design/board-numerics.md` (constants live in `internal/board`)

## What to do

Build the board confidence scoring engine and `board_mandates` processing in the `club` and `manager` schemas. Model mandates as structured negotiable rows (primary/secondary/strategic/financial targets) with status tracking. Calculate job security scores based on mandate fulfillment, league standing, financial performance, and fan sentiment. Generate `Explanation` objects for all confidence score changes. Trigger automatic sacking events when confidence falls below the sacking threshold, terminating the manager's assignment and resetting the club to hiring mode.

## Acceptance criteria

- `club.boards` and `club.board_mandates` tables store structured targets (category, target_value, weight, status, deadline) per club.
  - **Done.** Migration 0039 (partial unique index, one open mandate per club/manager/season/category); bootstrap seeds DNA + persona + supporter sentiment; four target types with a target_value are generated lazily per (club, manager, season).
- Periodic world ticks evaluate board confidence across league performance, financial health, and strategic mandate progress.
  - **Done.** Weekly-tick hook in `internal/app` → `board.Service.WeeklyReview` (per-manager, idempotent per tick).
- Job security score is updated world-scoped and stored in `manager.job_security_snapshots` with full `Explanation` factor breakdowns.
  - **Done.** `writeReviewSnapshot` persists the seven factor columns + weighted total; factor deltas sum to the total (tested).
- Managers can view their board confidence level (0-100%) alongside clear explanation breakdowns detailing exactly why confidence gained or lost points.
  - **Done.** `GET /api/managers/me/board` returns `BoardView` (confidence, snapshot, explanation, mandates).
- If board confidence breaches the sacking threshold, a `MANAGER_SACKED` event is emitted, the manager's club assignment is removed, and the club enters PolicyBot/interim management.
  - **Done.** Threshold 25, human-managed clubs only (`is_policy_bot = FALSE`); `manager.Service.Sack` carries the explanation, club flips to AI control.
- Fired managers retain their global career history log in `manager.manager_history` while becoming available for new job assignments.
  - **Done.** Reuses the existing endAssignment/career-history path (unchanged semantics).

## Delivery evidence

- Unit: `backend/internal/board/numerics_test.go` (`go test ./internal/board/`).
- Integration (needs Postgres): `backend/internal/board/service_integration_test.go`, `backend/cmd/api/board_integration_test.go` (compile-check `go vet -tags integration ./...`).
- HTTP + docs coverage: `go test ./internal/httpapi/` (openapi.yaml covers both routes).
- Full verification: `go build ./...`, `go vet ./...`, `go test ./internal/... ./pkg/...`.

### Deferred

- Richer club-DNA alignment scoring → S10-03 (MVP: ambition/patience clamp).
- Realtime `board_update` fan-out to the frontend → S07-01.