# IM58 — Match engine: expected goals (xG)

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** —

## What to do

1. `pkg/matchsim`: for every chance, add its goal probability to the attacking side's xG. In `resolveChance` that probability is already exact: `goalW / total`. Penalties add `penaltyConversionRate`. Result gains `HomeXG`, `AwayXG` (float, 2 dp).
2. **No extra RNG draws**: xG is computed from values already present, so seed + inputs replay gives the same results and same xG. Golden/replay tests must stay green unchanged.
3. Migration `0061_match_xg`: `home_xg`, `away_xg NUMERIC(4,2)` nullable on the match result row (null = played before IM58); add to `migrations/README.md` matrix.
4. Expose on the fixture/match result response (`GET /fixtures/:id`, IM45 match stats) so the manager sees their xG at full time.
5. Mirror in `openapi.yaml`; Touchline Book match chapter.

## Recorded decisions

- As built: exposed on `GET /api/matches/:id/events` → `stats.{home,away}.xg` (the IM45 post-match panel), null while live and for pre-IM58 matches. Columns live on `match.matches`.

- Needed now for IM57's chance-quality factor and later for the post-match screen (product owner, 2026-10-09).
- Pre-IM58 matches stay null; no backfill (cannot be recomputed without re-sim).

## Tests

Unit: same seed → identical goals and xG; xG ≥ 0; over many seeded sims, mean xG ≈ mean goals (within tolerance) — the calibration check.

## Delivery evidence

### Files

`pkg/matchsim/simulate.go` (`chanceWeights`, `chanceGoalProb`, `addXG`; `resolveChance` shares `chanceWeights`), `team.go` (MatchResult.home_xg/away_xg), `migrations/0061_match_xg.{up,down}.sql` + README row, `internal/match/{persist,live,read,service,match}.go` (both finalize paths write xG; `Match.home_xg/away_xg`; `stats.home.xg/away.xg`), openapi (`MatchSideStats.xg`). Tests: `pkg/matchsim/xg_test.go` (deterministic; 4000 seeds mean xG 1.386 vs mean goals 1.355), the golden replay digest is unchanged (proves no extra draw).

### Verification (2026-10-09, embedded Postgres 16 on :55432)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. golangci-lint not installed locally (not run).
Integration, serial (`-p 1`), packages competition, match, scout, httpapi, pkg/matchsim, compared with a clean HEAD worktree on the same DB: match, httpapi, matchsim pass on both; competition and scout fail on both with the same existing failures (detail/scheduling/cup fixtures, `TestNextFixtureScout`). The only extra branch failure, `TestListClubFixtures`, is flaky on HEAD too (2 of 4 HEAD runs fail): it picks the first club by name, which is sometimes in a league with no started season.
