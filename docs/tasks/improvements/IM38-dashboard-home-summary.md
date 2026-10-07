# IM38 — Home dashboard: counts, board factors and summary panel

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline.dc.html`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S07-01 dashboard

## What to do

1. `GET /api/dashboard` adds `counts` (`urgent`, `important`, `interesting`, `by_category`) so the shell can show inbox and nav badges.
2. Board items (`board:*`, `board-drop:*`) carry `explanation` (the stored job-security snapshot explanation: subject, score, factors).
3. Add `summary` for the right rail: board confidence and change since the previous snapshot; squad morale average and unhappy count; cash, weekly wage bill and wage budget; league position and competition.

## Open questions (design needs data the engine does not model)

- "N since your last visit": `last_activity_at` is overwritten on activity, so the previous visit time is not stored.
- Action button labels ("Talk to player") are client copy; the API keeps `action.kind`.
- Which nav item each category counts towards is a client mapping (counts are given by category).
- Nav counts for Training/Scouting/Social have no source items.

## Recorded decisions

Summary values come only from tables already read by the dashboard store (job_security_snapshots, player_condition, ledger/budgets, standings).

## Delivery evidence

### Files

internal/dashboard/{model,service,store}.go, openapi DashboardSnapshot/DashboardItem, dashboard_integration_test.go (TestDashboardSummaryCountsAndBoardFactors).

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.

### Notes

Bug fixed on this path: `UnhappyPlayers` read `player_condition.club_id`, which does not exist (`ERROR: column pc.club_id does not exist` in test logs), so morale items never appeared. Now joins `players.club_id`. Known limit: `BoardSnapshots` takes 4 rows across all clubs (fine for one club).
