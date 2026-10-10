# IM67 — Fixtures & results page

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** `Touchline Fixtures.dc.html` (claude.ai/design project 244e00dd…), product-owner request 2026-10-10 (frontend in scope).
**Depends on:** IM59 (difficulty), IM66 (attendance), IM45/IM58 (match stats, xG)

## What to do

1. `GET /api/clubs/:id/fixtures?season=current`: every fixture from the club's current league season start up to the next season's start (all competitions, no 30 cap). Each row adds `attendance`, `position_after` (league position once that matchday is in), and on unplayed rows `difficulty` + `last_meeting`. Other values of `season` → 400.
2. `/play/fixtures` + nav entry: All/League/Cups and All/Home/Away filters, five summary tiles, month-grouped table (MD, date, comp, opponent, H/A, score, result, pos, att), side panel for the selected fixture. Mobile: card list; tapping a card expands it.

## Recorded decisions

- Window = league season `start_date` up to the next league season's `start_date` (open-ended until it exists), so cup ties after the final league matchday are listed (product owner, 2026-10-10).
- Attendance shows as soon as the match kicks off (live rows included), not only once it is completed.
- `position_after` is replayed from results in GetStandings order (points, GD, GF, name); not stored.
- Played panel shows our scorers + minutes, xG, chances and cards (IM45 stats). Possession, shots and player of the match from the design are **not modelled** and are left out.
- Upcoming panel: kick-off, difficulty (engine's five labels, not the design's three), last meeting (any competition). "Opponent position" from the design is left out (not in the payload; difficulty already uses strength + form).
- Cup round shows as `R<n>` (fixtures store the round as matchday; round names are not modelled).
- Difficulty costs two squad loads per unplayed fixture (`ponytail:` note in `season_fixtures.go`).

## Delivery evidence

- Backend: `internal/competition/season_fixtures.go` (+ `season_fixtures_test.go`), `difficulty.go` (extracted `rateUpcoming`), `internal/httpapi/club_handlers.go`, `calendar_integration_test.go`, `internal/apidocs/openapi.yaml`.
- Frontend: `pages/play/fixtures.vue`, `composables/manager/fixtures.ts`, `types/manager/fixtures.ts` (adds `difficulty.level`), `utils/routes.ts`.
- Gates 2026-10-10: gofmt clean; `go build`, `go vet`, `go vet -tags integration` pass; `go test ./...` pass. Integration (embedded Postgres): httpapi Calendar/Docs/Fixture/MatchFeed/Live pass (incl. new season=current assertions); competition Upcoming/Difficulty/ClubFixtures pass. `nuxi typecheck` 0 errors; eslint clean on touched files.
- Not browser-checked (needs login). The integration fixture has no played matches, so `position_after`/`attendance` on real rows is covered by unit tests only.
