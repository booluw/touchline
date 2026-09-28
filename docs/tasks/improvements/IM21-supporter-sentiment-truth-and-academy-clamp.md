# IM21 — Supporter sentiment truth pass and academy shutdown clamp

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (supporter/board engine truth)
**Source:** Product request — "how is the supporter's score calculated and when
is it reevaluated?" A read-only audit of the supporter lens answered the
question and found the documentation disagreeing with the code in four places,
plus one real defect: an academy shutdown could push supporter sentiment
**below** the floor the board EWMA converges within
**Depends on:** the board review engine (IM02 monthly cadence, the
`(manager_id, world_tick)` snapshot guard in `board.snapshotExists`, the EWMA in
`board.supporterBlend`), `club.supporter_groups.current_sentiment` (migration
0005), and the academy shutdown path in `academy.SetActive`. No migration, no
config key, no route, no formula change.

## What to do

Make the supporter score's documentation state what the code actually does, and
stop the academy shutdown from writing sentiment outside the engine's range.

1. **Cadence truth.** A review is triggered by the month-boundary `Board.Review`
   **or** lazily by `GET /api/manager/me/board`, and is capped at one per
   `(manager_id, world_tick)`. Correct the "monthly only" and "weekly" claims;
   record the path-dependence as an accepted trade-off rather than changing the
   trigger (that would also unhook the lazy mandate set from first board view).
2. **Clamp the shutdown hit** at `board.SupporterSentimentMin` instead of 0,
   through one pure helper, and report the delta actually applied.
3. **Disambiguate "sentiment"** in the glossary: the transfer-request deltas are
   the player↔manager relationship journal, not club supporter sentiment. Fix
   the two wrong schema/constant names.

## Delivery evidence

### Backend — board (comment-only)

Five Go comments still described the pre-IM02 **weekly** review. IM02 moved
reviews weekly→monthly and updated `docs/design/board-numerics.md`, but missed
every Go comment, so the code and its own documentation contradicted each other:

- `backend/internal/board/model.go:2` — package doc "the weekly confidence
  scoring" → "the monthly confidence scoring".
- `backend/internal/board/numerics.go:30-33` — `SupporterSentimentAlpha` comment
  "each weekly review" → states the real rule (monthly + lazy per-tick board
  view, one blend per tick).
- `backend/internal/board/numerics.go:52-54` — `SackThresholdTotal` "on the weekly
  review" → "on the monthly review".
- `backend/internal/board/store.go:299-300` — `snapshotExists` "idempotent weekly
  replay" → "idempotent monthly replay" (the adjacent comment already named the
  read-triggered half correctly).
- `backend/internal/board/evaluate.go:50-52` — `evaluateAndRecord` "emits the
  weekly review" → "emits the board review".

`service.go:40-42` was already correct ("It is the monthly board review (IM02)")
and is the reference. `store.go:189` (`weekly_wage`) and the academy youth-wage
mentions are genuinely weekly and untouched.

### Backend — academy (the fix)

`backend/internal/academy/model.go`:

- `sentimentAfterShutdown(current int) int` — new pure helper, `−ShutdownSentimentPenalty`
  floored at `board.SupporterSentimentMin`. Imports the floor from `board`
  rather than duplicating the literal, so the clamp cannot drift. The edge is
  cycle-free (`board` imports only `manager`; `manager` does not import `academy`),
  and cross-package constant imports are house style here (`finance.YouthWeeklyWage`).
- Also lifts a legacy sub-floor value (a pre-fix shutdown wrote 0–14) back to 15.

`backend/internal/academy/service.go` (`SetActive`):

- The old SQL was `SET current_sentiment = GREATEST(0, current_sentiment - $2)`,
  which could write **below** the EWMA floor — a value the next review immediately
  blends back up, so the explanation's headline number was not what the fanbase
  felt, and the EWMA could never settle at its own minimum.
- Now: read the current value with `SELECT … FOR UPDATE` (so a concurrent
  `Board.Review` cannot blend between read and write), compute the new value in Go
  through the helper, and write it as a parameter. The clamp lives in one place,
  not in SQL.
- The event explanation and the `sentiment_hit` payload report
  `next - currentSentiment` — the **delta actually applied**. At the floor this
  is smaller than 15, and the old code claimed the full penalty regardless.
- A missing supporter row is treated as a no-op (`pgx.ErrNoRows`), matching the
  old `UPDATE`-affects-zero-rows behaviour.
- Removed `boolToInt`, left dead by the change.
- Unchanged: reopening the academy still restores no sentiment.

### Backend — tests

`backend/internal/academy/model_test.go` (plain unit test, no build tag, so it
runs in `go test ./...` — the package had **no** sentiment coverage at all):

- `TestSentimentAfterShutdown` — table over `100→85`, `50→35`, `20→15` (clamp
  binds), `15→15` (no-op at floor), `10→15` and `0→15` (legacy sub-floor lifted).
- `TestSentimentAfterShutdownRespectsEWMAFloor` — sweeps `−20..100` asserting the
  result never lands outside `[board.SupporterSentimentMin, board.SupporterSentimentMax]`.

### Docs

- `docs/design/board-numerics.md`:
  - `:118` — the stored column was documented as `supporter_groups.sentiment`;
    it is **`current_sentiment`** (migration 0005). Cadence wording corrected to
    "on every review that runs".
  - `:186` — "recalculated on the month-boundary review **only**" was false;
    replaced with the real rule, the `(manager_id, world_tick)` cap, both
    triggers, and the path-dependence consequence.
  - `:18-22` — §1 now notes the lazy board-view trigger alongside the
    month-boundary one, with a pointer to §9.
  - §9 — recorded that the seeded `loyalty`, `financial_sensitivity` and
    `rivalry_intensity_base` columns are read by no scoring path, and that the
    academy shutdown is the only non-EWMA mover.
- `docs/design/academy-numerics.md` — the shutdown row and §5 named a constant
  `AcademyShutdownSentimentHit` that does not exist (it is
  `ShutdownSentimentPenalty`); corrected, plus the floor, the immediate-not-blended
  application, the realised-penalty caveat and the no-restore-on-reopen rule.
- `docs/how-to/glossary.md`:
  - **Sack threshold** row said "**Weekly** weighted total" — stale, corrected.
  - **Supporter sentiment (board lens)** — added the column name, the real
    cadence, the academy-shutdown exception and an explicit pointer away from the
    §9 relationship deltas.
  - **Request lifecycle** / **Relationship memory** rows — the approve `+15` /
    deny `−25` deltas were labelled "sentiment", which in a glossary that defines
    "Supporter sentiment (board lens)" invites reading them as supporter
    sentiment. They are the player↔manager relationship journal on
    `social.relationships` (the `Sentiment*` constant names in
    `internal/player/numerics.go` are historical). Relabelled, with the
    distinction spelled out.
- `docs/product_manager.md` — **OPD-47** records all three decisions.
- `docs/tasks/README.md` — **no row added**: the improvements matrix was last
  touched at IM12 and does not list IM13–IM20 either, so adding IM21 alone would
  not reflect the actual state. Left consistent with recent practice.

### Verification

Run from `backend/`, in the order `AGENTS.md` prescribes:

- `gofmt -w` on the six touched Go files; `gofmt -l` clean over
  `internal/academy/` and `internal/board/`.
- `go build ./...` — pass
- `go vet ./...` — pass
- `go vet -tags integration ./internal/... ./pkg/...` — pass
- `go test ./...` — pass, including the two new `TestSentimentAfterShutdown*`
  cases
- `go test ./internal/academy/ -run TestSentimentAfterShutdown -v` — 2/2 pass

Integration tests remain **compile-gated only** (no `TEST_DATABASE_URL`, no
usable local Postgres: available binaries are x86_64 on an arm64 host). The
academy shutdown's SQL path therefore has no executed coverage in this
environment; the arithmetic it delegates to is covered by the unit tests above.

Unrelated pre-existing dirty files were left untouched
(`internal/match/seed.go`, `pkg/matchsim/attribution.go`,
`internal/httpapi/scheduling_handlers.go`).

## Recorded decisions

- **The lazy board-view review stays.** A manager opening `GET /api/manager/me/board`
  still triggers a per-tick review, so the board page is never stale. The
  consequence — sentiment converges ~4× faster for a daily checker than for a
  manager who never opens the board, from identical league positions — is
  **accepted, not a bug**, and is now documented rather than papered over.
  Making the figure monthly-only is a balance decision: it would also detach
  first-view mandate generation (`TestBoardMandatesGeneratedOnFirstView`).
- **The floor is imported, not duplicated.** `academy` imports
  `board.SupporterSentimentMin` so the EWMA range has one source of truth; the
  alternative (a local literal with a keep-in-sync comment) was rejected as
  drift-prone.
- **`GREATEST(0, …)` was a bug, not a design choice.** A 0 floor cannot be
  defended: the EWMA cannot converge to a value below its own minimum, so the
  number was guaranteed to move on the next review regardless of football.
- **The clamp is applied in Go, not SQL,** so it is unit-testable without a
  database and lives in one place. The row is locked `FOR UPDATE` because the
  read-modify-write is no longer a single atomic statement.
- **The realised penalty is reported, not the nominal one.** An event that claims
  `−15` when it applied `−5` is a lie in the explanation payload; the at-the-floor
  case is now honest.
- **The broader supporter model is untouched.** Attendance, gate receipts scaled
  by happiness, tradition demands (S10-03) and journalist influence (S15-02) remain
  PRD §39 scope, as do the three seeded-but-unread supporter columns. IM21 makes
  the current model legible; it does not extend it.
- **The transfer-request deltas were relabelled, not removed.** They are
  implemented and correct — the `Sentiment*` constant names are the only thing
  wrong, and renaming constants across the player package was out of scope for a
  docs-truth pass.
