# IM55 — Standings: last-5 form per club

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** —

## What to do

1. `GET /api/competitions/:id/standings`: each row gains `form: ["W","D","L",…]`, newest first, at most 5, from the active season's completed league fixtures.
2. One query (window function over completed fixtures, partitioned by club), not N+1. Reuse the IM39 form query if its shape fits.
3. Mirror in `openapi.yaml` (StandingRow.form).

## Recorded decisions

- Table is about clubs: **no manager handle / "You" marker** on rows (product owner, 2026-10-09). Only the form dots.
- GD is derived client-side from goals_for − goals_against; no new field.

## Tests

Integration: a club with >5 results returns exactly 5, newest first; a club with none returns `[]` (not null).

## Delivery evidence

### Files

`internal/competition/service.go` (StandingRow.position, .form), `standings.go` (`attachForm`: one windowed query, season start-scoped), `detail.go` (admin detail rows get position + form too), `apidocs/openapi.yaml` (StandingRow). Test: `outlook_integration_test.go` (`TestCompetitionScreenReads`: positions 1..N, one result each, leader `[W]`).

### Verification (2026-10-09, embedded Postgres 16 on :55432)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. golangci-lint not installed locally (not run).
Integration, serial (`-p 1`), packages competition, match, scout, httpapi, pkg/matchsim, compared with a clean HEAD worktree on the same DB: match, httpapi, matchsim pass on both; competition and scout fail on both with the same existing failures (detail/scheduling/cup fixtures, `TestNextFixtureScout`). The only extra branch failure, `TestListClubFixtures`, is flaky on HEAD too (2 of 4 HEAD runs fail): it picks the first club by name, which is sometimes in a league with no started season.

### Notes

Every caller of `GetStandings` (public table, club overview, scout, outlook) now carries form.
