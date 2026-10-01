# IM34 — 2D match simulation (positional engine), 3D later

**Status:** Implemented
**Owner:** unassigned
**Branch:** `feat/simulation`
**Sprint:** Improvements (match experience)
**Source:** Product request — show a 2D/3D simulation while a match is being
played.
**Depends on:** `pkg/matchsim` (v1.6, deterministic, minute-resolution, events
only — no ball or player positions), live pacing in `internal/match/live.go`
(`PaceMinute`, one simulated minute per `tick.match_cadence`, default 20s), the
`match_tick` realtime envelope, formations/lineup slots in `internal/squad`,
the match page `frontend/app/pages/play/matches/[fixtureId].vue`.

## Current status (2026-09-30, `feat/simulation`, uncommitted on `b90731b`)

Phases 1-5 are implemented; see **Delivery evidence** at the end. Phase 6 (3D)
is a separate task. The 2D view has **not been seen running in a browser** yet —
that manual check is the next action.

## Recorded decisions (product owner, 2026-09-30)

1. **2D first, 3D later.** The movement data must be renderer-independent so a
   3D view can reuse it.
2. **A new positional engine, separate from `matchsim`, rolled out
   incrementally.** Both engines give the **same result and the same events**;
   the new one may offer **more** (a superset).
3. **Continuous full match**, live and in replay — not highlights only.
4. **The picture comes first.** The text feed and scoreline never announce a
   goal (or any event) before the picture shows it.
5. **The extra events are both shown and stored.** They appear as visuals and
   stats **and** are saved in `match.match_events`. The migration is done up
   front, not deferred.
6. **Golden-goal extra time is animated in full** — every flushed minute, not
   just the deciding one.
7. **Anyone can watch any match.** The later goal is a shareable link that lets
   anyone holding it view that match, so access must be designed for that.
8. **Only notable extra events are stored as rows** (shots, saves, tackles,
   fouls, corners, offsides). Passes and throw-ins are not rows: they live in
   the movement track, which is recomputed from the seed and so costs no
   storage at all. Pass statistics are counted from that track.

### What decision 2 means technically

Two engines can only be guaranteed to agree if one follows the other. So:

- `matchsim` stays the **only** thing that decides score, events, player
  ratings, injuries and everything downstream. It is not modified.
- The new engine (`pkg/pitchsim`) takes the same inputs **plus `matchsim`'s
  result** and produces the movement that *realises* those events: the goal in
  minute 37 by player X is a move that ends with X scoring in minute 37.
- The superset is everything `matchsim` does not have: positions, and extra
  events (pass, shot off target, save, tackle, corner, throw-in, offside). These
  never change the score or any stored outcome.
- `pitchsim` uses its own RNG stream derived from the match seed, so
  `matchsim`'s draw order and replay guarantee are untouched.

Tested invariant: `pitchsim`'s events filtered to `matchsim`'s event types are
identical (type, minute, club, player, order) to `matchsim`'s events.

## Design

### `pkg/pitchsim` (pure Go, no DB/network/clock)

`Generate(opts matchsim.Options, res matchsim.MatchResult, shape Shapes) Track`

- **Inputs:** the `matchsim` options and result, plus each side's formation and
  slot assignment (from `internal/squad`, frozen in the live session snapshot).
- **Output (`Track`, versioned `track_version`):** per match minute, a list of
  keyframes (time offset within the minute; ball x/y/height; 22 player x/y) and
  the minute's events with their time offset. Pitch coordinates are normalised
  0..1, home attacking left-to-right in the first half; height is carried from
  the start so 3D needs no format change.
- **Model, per minute:**
  - team shape = formation slot positions, shifted and compressed by who has
    the ball, the style (`low_block` sits deep, `gegenpress` high) and score;
  - possession phases split the minute in proportion to `HomePossession`; in a
    phase the ball moves by passes between nearby team-mates (picked with the
    existing `PlayerRef.Weight`), with turnovers between phases;
  - a minute containing a `matchsim` event is scripted backwards from it: a goal
    ends a move with the recorded scorer (and assister) finishing; a chance ends
    in a save or miss; a penalty is a spot kick; a card follows a foul by the
    booked player; an injury stops play; a substitution swaps the player at the
    touchline; a red card removes the player for the rest of the match.
- **Continuity:** the state at the end of minute *m* is the start of *m+1*. Like
  `PaceMinute`, generate from minute 1 and take the minute needed — cheap, and
  the replay contract stays "seed + snapshot + inputs".
- **Size:** about 20 keyframes a minute, quantised integers — roughly 2 KB a
  minute, under 200 KB a match before compression. The client interpolates.

### Storage and API

- **Positions are not stored.** The track is recomputed from what is already
  persisted (seed, snapshot, inputs), exactly like the events.
- **Extra events are stored** in `match.match_events` (decision 5) by migration
  `0058`, written first:
  - extend the `event_type` CHECK with the notable types only (`shot`, `save`,
    `tackle`, `foul`, `corner`, `offside` — final list fixed in the phase 1
    spec); passes and throw-ins stay in the track (decision 8);
  - add `source TEXT NOT NULL DEFAULT 'matchsim'` (`'matchsim' | 'pitchsim'`)
    and `offset_millis INT` (position within the minute);
  - `UNIQUE (match_id, sequence)` stays, and the existing events keep their
    sequence numbers, so extra rows take sequences from a separate high range
    and the feed orders by `(minute, offset_millis, sequence)`.
  Everything that reads the feed for an outcome (ratings, appearances,
  injuries, commentary, replay-equality tests) filters `source = 'matchsim'`.
- Extra events are written where the normal ones are: `PaceMinute` (live) and
  `PlayFixture` (quick-play), in the same transaction, only when the world
  switch is on.
- `GET /api/matches/:id/track?from=&to=` — track for a minute range. Mirror in
  `openapi.yaml` (router coverage gate).
- **Access (decision 7):** the track and events endpoints are readable by any
  signed-in user for any match, not tied to club or world membership. Reads go
  through one "may view this match" check so the shareable link later is a
  second way to pass that check (a per-match token on a public route), not a
  rewrite. The share link itself is out of scope here.
- Live: `PaceMinute` attaches the paced minute's track to the `match_tick`
  envelope.
- Rollout switch: world setting `match.visual_engine` = `off` (default) | `2d`,
  read like `tick.match_cadence`. Off means no track is computed or sent and
  the match page is exactly today's.

### Frontend (in scope for this task)

- A `MatchPitch` component on the native Canvas 2D API (no new dependency):
  pitch, 22 dots with shirt numbers, ball, scoreline, clock; interpolates
  between keyframes with `requestAnimationFrame`.
- A playback clock in the match store: one match minute plays over the match's
  `pacing_millis`. Replay of a finished match adds pause, speed and seek.
- The component consumes only the `Track` format, so a 3D renderer is a second
  component over the same data.

### Live timing (consequence of "continuous")

A minute's events are only known when that minute is paced, so its movement can
only be shown afterwards: the picture runs **one cadence step behind** the
server (20s by default). Per decision 4, the feed, scoreline and clock on the
match page are revealed by the playback clock, not on arrival: an event appears
when the picture reaches its `offset_millis`.

**Golden goal (decision 6):** the extra-time block arrives in one step but is
played back minute by minute at the match's pace, so the viewer sees all of it.
The server already knows the result while this plays, so the match page must
hold "full time" and the final score until playback reaches the deciding goal.

## Phases (each shippable behind the switch)

| Phase | Delivers | Proof |
| --- | --- | --- |
| 1. Contract + migration | `pitchsim` spec (README in the package, like the `matchsim` addenda), `Track` format v1, slot coordinates per formation, final extra-event list; migration `0058` (event types, `source`, `offset_millis`) and `source = 'matchsim'` filters on existing readers | reviewed spec; migration up/down/up; existing suites unchanged |
| 2. Engine v1 | shapes, possession phases, ball passing, all `matchsim` event types scripted, red cards and substitutions | unit tests: event-equality invariant, determinism (same input, byte-identical track), continuity across minutes, everyone stays on the pitch, sent-off players stay off |
| 3. Replay | track endpoint open to any signed-in user through the single view check, world switch, extra events stored on quick-play, `MatchPitch` + replay controls for **finished** matches | handler integration test, OpenAPI gates, manual replay of a finished match |
| 4. Live | track and extra events in `match_tick`, playback-synced feed and scoreline, full golden-goal playback, reconnect mid-match catch-up, live tactic change visible from its minute | live integration test (kickoff → full time with tracks), manual live match |
| 5. Superset | match stats: shots, saves, corners, fouls from the stored rows; passes and possession by zone counted from the track; feed lines for the notable events | invariant still holds; stats equal the stored rows and the track |
| 6. 3D | second renderer over the same track (separate task; adds a 3D library) | — |

Phases 2 and 3 can be built and tried on finished matches without touching the
live path at all, which is the low-risk start.

## Risks

- **Believability**: scripted-backwards moves can look staged. Phase 2 needs a
  look-and-feel review on real fixtures before phase 4.
- **Pacing cost**: generating from minute 1 on every paced minute is O(minute)
  per live match. Measure in phase 4; cache the carried state in the live
  session if it shows up.
- **Golden goal**: the engine may need many extra minutes (the code comment
  says possibly hundreds) to find a goal. Animating all of them means a tie can
  play on screen long after the server has the result, and the rest of the game
  (fixtures list, news, notifications) will show that result first. Measure how
  long real ties run before phase 4.
- **Feed volume**: notable events only (decision 8) should add a few dozen rows
  a match; confirm the real number in phase 2. If passes ever need to be
  persisted (for example if the engine changes and old matches must still
  replay identically), store the whole track as one compressed blob per match
  rather than rows — not needed while the track is recomputable, and the
  `track_version` field is what tells us when it would be.
- **Leaking the result**: anything outside the match page that shows a live
  score (dashboard, fixtures) is not delayed and will be ahead of the picture.
- **Lab cadences** (`10ms`): playback cannot keep up; the switch should be
  ignored below a minimum cadence.
- **Existing integration failures** (IM32) sit in the match suite and will
  muddy phase 3–4 evidence unless baselined the same way as IM33.

## Open decisions

None.

## Delivery evidence

### Files

- `backend/pkg/pitchsim/` — engine, tests, README (contract and track format).
- `backend/migrations/0058_pitchsim_events.{up,down}.sql` + README matrix row.
- `backend/internal/match/pitch.go` (switch, input, extra-event storage,
  `GetTrack`), and edits to `live.go`, `service.go`, `persist.go`, `read.go`,
  `events.go`, `match.go`; `internal/matchday/runner.go` (track on the tick);
  `internal/world/service.go` (default key).
- `backend/internal/httpapi/match_handlers.go`, `router.go`,
  `internal/apidocs/openapi.yaml` — `GET /api/matches/:id/track`, open reads
  through `mayViewMatch`.
- `frontend/app/components/MatchPitch.vue`, `stores/match.ts`,
  `pages/play/matches/[fixtureId].vue` — canvas view, playback clock, feed and
  score revealed by playback, replay controls, stats, polling fallback.
- Docs: `docs/how-to/match-simulation.md`, `docs/product_manager.md` (OPD-59).

### Verification (2026-09-30; Go run in a `golang:1.26` container because the
host toolchain is x86_64 and Rosetta is unavailable)

- `gofmt`, `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` — pass,
  including the OpenAPI gates.
- `pitchsim` unit tests: determinism; a minute is identical in a partial and a
  full generation; every matchsim event cued exactly once; extras limited to the
  six types; positions in bounds and consistent with the lineup; ball in the
  correct goal for goals in both halves and extra time; red card and
  substitution; pass share follows the ball share over ten seeds.
- Integration, against a throwaway `postgres:16`
  (`go test -p 1 -tags integration`): `./internal/match/...` and
  `./internal/world/...` pass, including the new `TestQuickPlayPitchsim`
  (switch off = unchanged feed and no track; switch on = offsets on every row,
  extras stored, score unchanged, track regenerates the stored extras, range
  query, feed order) and `TestLivePitchsim` (one track minute per paced minute,
  matchsim rows equal an instant `Simulate`, stored extras equal a
  regeneration).
- Migration 0058 applied down, up, down, up without error.
- Frontend: `eslint` clean on the three touched files. `pnpm typecheck` reports
  one error, in `app/middleware/route.global.ts`, a file this work did not touch.

### Not verified

- **The 2D view has not been run in a browser.** Rendering, playback timing and
  the delayed feed are unexercised outside lint and type checks.
- The track endpoint has no HTTP-level test; `./internal/httpapi/...` under the
  integration tag did not finish within 600s and reported 13 failures in
  unrelated flows (login, admin, board, club — see IM32) before timing out.
- `./internal/matchday/...` has two failing tests
  (`TestRunnerAdvancesMatchdaysAndRollsOver`, `TestRunnerCapOnlyForStaggered`)
  that fail identically on untouched `b90731b`.
- Golden-goal playback and the cross-world polling fallback have no test.
- Real golden-goal lengths and live pacing cost were not measured.

### Follow-ups

- Shareable match link (second path through `mayViewMatch`).
- 3D renderer over the same track; give the ball height.
- Team shape reacting to live tactic changes.
