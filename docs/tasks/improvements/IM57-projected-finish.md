# IM57 — Projected finish + "Why" factors

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM56 (same endpoint), IM58 (xG chance-quality factor)

## What to do

Add `projection` to the IM56 `outlook` response.

1. **Projected finish**: Monte Carlo, 1000 runs of the remaining league fixtures. Each result drawn from the two clubs' home/away points-per-game rates. Return `position` (median) and `range` [p10, p90].
2. **Deterministic**: seed = season id + matchday, so the same day always gives the same answer. Cache per (league, matchday).
3. **Factors** (`[{label, delta, detail}]`, delta in points vs a neutral baseline, same shape as finance/board factors):
   - Remaining fixtures vs current top 6 (fixture list + table).
   - Home form (W/D/L at home, season).
   - Injuries to key players (player injury tables, weighted by rating, expected return vs games left).
   - Goal-difference trend: last 5 vs season average.
   - Chance-quality trend: xG for − xG against, last 5 vs season (needs IM58; omitted until it has data).
4. Mirror in `openapi.yaml`.

## Recorded decisions

- As built: results drawn from home PPG (home side) vs away PPG (away side), each shrunk toward the league average by 3 games; draw rate fixed at 26%; seed = season id + completed fixture count. **No cache** — a 1000-run projection is milliseconds; add one only if profiling says so.
- Factor deltas are signed ints (positive helps us) with the unit in `detail`: fixtures = games vs an even schedule; home form = points vs league home average; injuries = best-XI players unavailable for the next match; GD trend = goals; xG trend = expected goals (omitted until our league matches carry xG).

- Both GD trend and xG trend ship (product owner, 2026-10-09).
- Simulation lives in the read path; no new tables. If profiling shows cost, persist per matchday instead.

## Tests

Unit: same seed → identical output; a finished season → range collapses to the actual position; a stronger remaining schedule → negative fixtures factor.

## Delivery evidence

### Files

`internal/competition/outlook.go` (`projectFinish`, `projectionFactors`), `outlook_load.go` (`splitFixtures`, `ourTrends`, `xgTrend`, `bestXIUnavailable`). Tests: `outlook_test.go` (`TestProjectFinishDeterministicAndFinal`, `TestProjectionFactorsSigns`), integration determinism check.

### Verification (2026-10-09, embedded Postgres 16 on :55432)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. golangci-lint not installed locally (not run).
Integration, serial (`-p 1`), packages competition, match, scout, httpapi, pkg/matchsim, compared with a clean HEAD worktree on the same DB: match, httpapi, matchsim pass on both; competition and scout fail on both with the same existing failures (detail/scheduling/cup fixtures, `TestNextFixtureScout`). The only extra branch failure, `TestListClubFixtures`, is flaky on HEAD too (2 of 4 HEAD runs fail): it picks the first club by name, which is sometimes in a league with no started season.
