# IM59 — Next 5 fixtures with difficulty

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Competitions screen follow-up, product-owner request 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM55 (form query), IM39/scout (opponent rating + form)

## What to do

1. Extend `GET /api/clubs/:id/fixtures` with optional query params `upcoming=true` (scheduled only, kickoff ascending) and `limit` (1–30, default 30, the current value). With no params, the response is unchanged.
2. When `upcoming=true`, each fixture gains `difficulty` relative to the club in the path:
   `{ level: 1..5, label: "Very easy"|"Easy"|"Even"|"Hard"|"Very hard", factors: [{label, delta}] }`
   - **Strength gap**: opponent team rating − ours (the same rating the scout report already reads, `CurrentRating`).
   - **Venue**: home / away / neutral (cup finals).
   - **Opponent form**: their last 5 results (IM55 query).
   - Score = sum of factor deltas, bucketed into 5 levels; the thresholds are a tuning table, set from the simulated distribution so "Even" is the middle bucket.
3. All competitions (league + cups), so cup ties against clubs from other leagues work: the comparison uses ratings, not league positions.
4. The competitions screen calls `/clubs/{own}/fixtures?upcoming=true&limit=5`; the dashboard can reuse it.
5. Mirror in `openapi.yaml` (query params + Fixture.difficulty).

## Recorded decisions

- As built: **strength** = mean positional overall of each club's best 11 *available* players on the fixture date (`squad.LoadSquad`, so injuries and suspensions count). The plan named `form_state.current_rating`, but that is a form multiplier, not strength.
- **Venue**: home/away only; fixtures store no neutral venue, so cup finals rate as home/away.
- **Opponent form**: persisted all-competition `club.form_state` string.

- Rating-based, not position-based, so it compares across divisions (product decision pending confirmation).
- Difficulty is computed on read, not stored, so it moves with injuries and form.
- Factors use the same `{label, delta}` shape as the "Why" cards.

## Tests

Unit: bucket edges; home vs away on an equal-rated opponent differ by the venue delta. Integration: `upcoming=true&limit=5` returns ≤5 scheduled fixtures in kickoff order across league and cup; the default call (no params) is unchanged.

## Delivery evidence

### Files

`internal/competition/difficulty.go` (`rateDifficulty`, `xiStrength`, `UpcomingClubFixtures`, `recentForms`), `internal/httpapi/club_handlers.go` (`upcoming`/`limit` params, 400 outside 1..30), openapi (params, `FixtureDifficulty`; also corrected the existing response schema to the real `{fixtures: [...]}` shape). Tests: `difficulty_test.go` (bucket edges, venue swing, XI skips unavailable and caps at 11), integration + HTTP (2 fixtures with difficulty, `limit=0` → 400).

### Verification (2026-10-09, embedded Postgres 16 on :55432)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. golangci-lint not installed locally (not run).
Integration, serial (`-p 1`), packages competition, match, scout, httpapi, pkg/matchsim, compared with a clean HEAD worktree on the same DB: match, httpapi, matchsim pass on both; competition and scout fail on both with the same existing failures (detail/scheduling/cup fixtures, `TestNextFixtureScout`). The only extra branch failure, `TestListClubFixtures`, is flaky on HEAD too (2 of 4 HEAD runs fail): it picks the first club by name, which is sometimes in a league with no started season.
