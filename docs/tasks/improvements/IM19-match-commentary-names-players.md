# IM19 — Match commentary names players

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (live match feed)
**Source:** Product report — the live match feed showed the engine's raw
commentary templates ("Assist from {assist} … service for {player}"), so a
manager never saw a player's name in their own match
**Depends on:** the engine's role-placeholder commentary (`matchsim`
descriptions), the S04-02 attribution pass that links every event to
`player_id` / `related_player_id` (v1.6), and `resolveEventRefs`
(`internal/match`), which already resolves the names for both event paths
(`GetMatchEvents` and the live `PaceMinute` tick). No schema, engine, or
frontend change.

## What to do

Render the persisted commentary placeholders with the names the event row
already carries. The engine must keep storing templates (a re-simulated match has
to produce identical text for the determinism contract), so the substitution
happens **at read time**, inside the one function both event paths already call.

## Delivery evidence

- `backend/internal/match/commentary.go` (new) — `resolveCommentary` /
  `renderCommentary` rewrite a row's `detail` JSON text (`commentary` and
  `detail`) by substituting the placeholders with the names `resolveEventRefs`
  attached. Pure and idempotent: it reads the row, returns new JSON, leaves a
  detail that is not the engine's object shape byte-for-byte alone, and
  re-resolving an already-rendered row is a no-op (a live tick renders the rows
  it just persisted; the REST read renders them again).
- The **role → column mapping is per event type**, because the engine's
  attribution pass orders the two ids by what the event *is*:
  - goal / chance / penalty / card — `player_id` is the subject → `{player}`
  - assist — `player_id` is the **assister**, `related_player_id` is the scorer
    → `{assist}` = primary, `{player}` = related
  - substitution — `player_id` is the bench player **coming on**,
    `related_player_id` is the player coming off → `{sub}` = primary,
    `{player}` = related
  - anything else — `{player}` = primary, which is the subject in every remaining
    case.
  Getting this wrong names the wrong man in a sentence about two people
  ("Assist from Ada Bright … service for Kit Marek" is wrong, not cosmetic).
- Unresolved tokens fall back to neutral wording — `{player}` → "the player",
  `{assist}` → "a teammate", `{sub}` → "a substitute" — so a raw placeholder can
  never leak to the client (e.g. a side with no lineups).
- `backend/internal/match/service.go` — `resolveEventRefs` calls
  `resolveCommentary(rows)` after the name lookups, so **both** read paths (REST
  `GetMatchEvents` and the live `PaceMinute` tick, which feeds the S04-03
  `match_tick` envelope) are covered by the single hook. Nothing is rewritten in
  the database.
- `backend/internal/apidocs/openapi.yaml` — the `MatchEvent.detail` schema
  documents the contract (placeholders are always resolved to names on the wire,
  with the neutral fallbacks). `TestDocsCoverRouter` / `TestDocsOpenAPIValid`
  unaffected (no route change).
- Tests:
  - `backend/internal/match/commentary_test.go` (new, unit) —
    `TestCommentaryResolvesNames` pins each type's mapping, the neutral
    fallbacks, and that a structural event's text is untouched;
    `TestCommentaryLeavesForeignDetailAlone` pins the pass-through for a
    non-object detail and a nil detail; `TestCommentaryIdempotent` pins
    re-resolution; `TestCommentaryCoversEngineTemplates` renders **every**
    `{…}` template the engine can currently emit (goal, assist, chance, penalty
    scored/missed, both substitution wordings, the injury-forced change) and
    fails if any leaks a placeholder.
  - `backend/internal/match/live_integration_test.go` —
    `TestFeedCommentaryNamesPlayers` (new) checks the wiring end to end on a real
    persisted match: no commentary line contains a placeholder, every player
    reference is named, each named event's line actually contains that name, and
    the substitution line names both the player coming on and the player coming
    off. The two-name case is **forced** rather than hoped for: the manager's own
    substitution at the 60' window is submitted before full time, so the sentence
    under test is always emitted. The engine's random sub draw is 0.85 per side per
    window, so a naked random match would leave the most interesting line
    unexercised roughly once in two thousand runs — a green test that usually
    proves nothing.
- Verify: `gofmt -w` on the touched files, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/... ./cmd/...`, `go test ./...`
  all green. (Integration tests remain compile-gated locally — no
  `TEST_DATABASE_URL` and no usable Postgres/Docker on this arm64 host.)
- Docs: `docs/how-to/glossary.md` §15 ("Commentary"), the `MatchEvent.detail`
  schema, and `docs/product_manager.md` OPD-45.

## Recorded decisions

- **Resolve at read time; never rewrite the stored text.** The engine's
  templates are the determinism contract (OPD-21): a live re-run and an instant
  simulate must produce identical event streams, which a post-hoc name rewrite
  in the database would break. Resolution is a pure presentation step on the
  read, so it is free of migration, backfill, and re-simulation risk.
- **Map tokens per event type, not per column.** "Primary" means "the event's
  subject" and that subject is the bench player for a substitution and the
  assister for an assist. A single `player_id → {player}` rule would render the
  wrong name in exactly the two sentences that name two people.
- **Neutral wording over a visible gap.** A placeholder the attribution pass
  could not fill becomes ordinary English ("a substitute on for the player")
  rather than an empty hole, a raw token, or a fabricated name.
- **Both event paths, one hook.** `resolveEventRefs` is already the single
  name-resolution point for the REST read and the live tick; putting the
  substitution behind it means the realtime envelope and a later page refresh can
  never disagree about what a sentence said.
- **No engine change, no frontend change.** The match engine keeps emitting
  templates (its golden replay digest pins them), and the existing
  `ev.detail?.commentary` render in the fixture page picks the names up with no
  frontend edit.
