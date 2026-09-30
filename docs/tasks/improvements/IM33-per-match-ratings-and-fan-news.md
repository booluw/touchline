# IM33 — Per-match board rating, supporter reaction and fan-reaction news

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (supporter/board engine)
**Source:** Product request — supporters and the board should judge the manager
after every game, as in the real world, while board confidence stays monthly;
plus a news story after every match in which fans tell a news agency what they
think of the manager.
**Depends on:** the board review engine (IM02 monthly cadence, IM21 sentiment
range), the match-completion hooks (`match.Service`, quick-play and live), the
social rivalry edge (S06-04c), `world.news_stories`.

## What to do

1. After every completed match, give each club's manager a 0-100 board rating
   (result vs expectation) and store it per fixture.
2. Move supporter sentiment after every match, further in rivalry games; the
   monthly review only reads it.
3. Keep board confidence monthly; its Performance factor becomes the running
   mean of recent match ratings.
4. Publish one fan-reaction news story per human-managed club per match.

## Recorded decisions

OPD-58 in `docs/product_manager.md`: new per-match rating; per-match sentiment
replaces the monthly blend; stories for human-managed clubs only; deterministic
templates, backend only. Ratings and sentiment also apply to bot-managed clubs.

## Delivery evidence

### Files

- `backend/migrations/0057_match_ratings.{up,down}.sql` — `manager.match_ratings`
  and the `fan_reaction` news category; README matrix row.
- `backend/internal/board/numerics.go` — `expectedResult`, `matchRating`,
  `sentimentAfterMatch`, `runningRating` and their constants; removed
  `supporterBlend` / `SupporterSentimentAlpha`.
- `backend/internal/board/match.go` — `Service.RecordCompletedMatch`.
- `backend/internal/board/fannews.go` — seeded story templates.
- `backend/internal/board/store.go`, `evaluate.go`, `service.go` — the review
  loads the ratings, uses them for Performance, and no longer writes sentiment.
- `backend/internal/match/service.go`, `live.go` — `WithBoard` and the hook call
  in both completion paths; `backend/internal/app/app.go` wires it.
- Docs: `docs/design/board-numerics.md` §4a, `docs/design/academy-numerics.md`,
  `docs/how-to/cadences-and-time.md`, `docs/product_manager.md` (OPD-58).

### Verification (2026-09-30, working tree on `2cf6fb9`)

- `gofmt`, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` — all pass.
  New unit tests: `TestMatchRating`, `TestSentimentAfterMatch`,
  `TestRunningRating`, `TestBuildFanReaction`.
- `go test -p 1 -tags integration ./internal/match/... ./internal/board/...`
  against a throwaway `postgres:16` container (`TEST_DATABASE_URL`): the two new
  tests pass — `TestPlayFixtureWiresBoardHook` (two ratings, sentiment written,
  one home-club story linked to `MATCH_PLAYED`) and `TestReviewReadsMatchRatings`
  (Performance = mean of ratings, sentiment untouched).
- The same run reports five failures that also fail identically on untouched
  `2cf6fb9` (baseline run in a separate worktree and database), so they predate
  this change (see IM32): `TestFeedCommentaryNamesPlayers`,
  `TestPlayFixtureWiresSocialHook`, `TestBoardMandatesGeneratedOnFirstView`,
  `TestNegotiateMandateLifecycle`, `TestReviewSacksUnderperformingHumanManager`.
- Migration 0057 applied down, up, down, up on that database without error.

### Not verified

- The replay-adds-nothing behaviour is enforced by the `(fixture_id, club_id)`
  key but has no integration assertion (a completed fixture cannot be replayed
  through `PlayFixture`).
- The live completion path (`live.go`) is compile-checked only; no live-match
  integration test installs the board hook.
- The testcontainers fallback in `internal/testdb` failed on this machine
  (connection reset right after the first "ready" log); the suites were run with
  an explicit `TEST_DATABASE_URL` instead.
- No manual check through the running server.
