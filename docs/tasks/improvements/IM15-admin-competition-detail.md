# IM15 — Admin competition detail dossier

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (admin surface)
**Source:** Product decision (admin console)
**Depends on:** the `competition.competitions`/`competition_rules`/`seasons`/
`standings`/`competition_entries` schema (`0012`), `match.fixtures`/
`match.matches`/`match.match_events` (`0011`), `world.events` rollover payloads
(`0026`), and the admin route group (OPD-01). No in-flight improvement changes
any of these read shapes.

## What to do

Expose a single global admin endpoint that returns everything about one
competition — league **or** cup — so the admin console can render a full
dossier without chasing a dozen read calls.

## Delivery evidence

- `backend/internal/competition/detail.go` — the read-model:
  - `Service.CompetitionDetail(ctx, competitionID)` hits the same `id` on any
    competition row and returns a `CompetitionDetail` with the shared base
    (id, world, name, `competition_type`, status, country/region scope, tier,
    team count, league reputation, seasons total) plus the type-specific half.
    **`League` and `Cup` are mutually exclusive**, discriminated by
    `competition_type` (`league` | `domestic_cup` | `continental`), mirroring
    the existing `ClubCompetitionItem` club dossier.
  - `PastWinners` — every completed season's champion, newest first. Leagues:
    the standings leader per completed season using the same ordering as
    `GetStandings` (`points`, goal difference, goals for, name). Cups:
    `competition_entries.status = 'champion'` per completed campaign.
  - `TopScorers` — a `current_season` (the latest season's date window) and an
    `all_time` table, top 10 each, from `match.match_events` with
    `event_type IN ('goal', 'penalty_scored')`. `Goals` counts penalties in;
    `Penalties` is the subset. The player's current club is nested (nil for
    free agents). Fixtures carry no season id, so the current window is the
    latest season's `start_date .. end_date` (open-ended when the season has
    no end).
  - `league{}` — the season archive, the current/latest season with
    `fixtures_played`/`fixtures_total`, the full table (`StandingRowExt`:
    each club's country, leading result **streak** `{outcome W/D/L, length}`,
    and **next fixture**), and `movement`: `promoted_in` / `relegated_out` as
    the membership churn between the latest season and its immediately
    preceding completed season.
  - `cup{}` — the latest campaign season, late-entry round, total rounds, the
    champion once decided (read directly, since `GetCup` only surfaces a live
    season's champion), the materialized `rounds` (reusing `GetCup`), and
    `clubs`: every entrant with country, elimination status, highest round
    reached (ties + byes), its W/L streak, and next tie.
- `backend/internal/httpapi/competition_handlers.go` —
  `handleAdminGetCompetitionDetail` (`GET /api/admin/competitions/{id}/detail`,
  404 for unknown ids); `backend/internal/httpapi/router.go` — route added to
  the `requireAuth` + `requireAdmin` group. Admin-only, intentionally **not**
  world-scoped (the admin console is world-less; `competition_id` drives every
  query).
- `backend/internal/apidocs/openapi.yaml` — added the `getCompetitionDetail`
  path and the `CompetitionDetail` / `PastWinner` / `ScorerList` / `TopScorer`
  / `StreakInfo` / `LeagueDetail` / `SeasonDetail` / `StandingRowExt` /
  `LeagueMovement` / `CupDetail` / `CupClubRow` schemas. Deleted the two
  dead, league-shaped orphan schemas `CompetitionSummary` and `CompetitionDetail`
  (no Go type referenced them). `TestDocsCoverRouter` / `TestDocsOpenAPIValid`
  still pass.
- Tests (integration, compile-gated locally — live Postgres required):
  - `backend/internal/competition/detail_integration_test.go` —
    `TestCompetitionDetailLeague` (base identity + country/tier/team count,
    table enriched with country/streak/next fixture, fixture progress, empty
    movement first season, scorer history with a goal+penalty split),
    `TestCompetitionDetailLeagueMovement` (a real two-tier season played
    through `ApplyResult` so the rollover creates season 2; past winners =
    the completed champion; movement matches the actual membership diff),
    `TestCompetitionDetailCup` (past winner from a completed campaign, live
    campaign with two decided ties: entrant countries, elimination, round
    reached, W/L streaks, undecided champion).
  - `backend/cmd/api/competition_detail_integration_test.go` —
    `TestHTTPAdminCompetitionDetail` (league branch 200 with base +
    `league{}` only, cup branch 200 with `cup{}` only, unknown id → 404,
    non-admin → 403).
- Verify: `go build ./...`, `go vet ./...`,
  `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` all
  green (includes the openapi gating tests); touched files `gofmt`-clean.
- Docs: `docs/product_manager.md` OPD-41.

## Recorded decisions

- **One universal endpoint, discriminated on the wire.** The dossiers share
  the base identity and history (seasons, winners, scorers), so they live on
  a single `GET /api/admin/competitions/{id}/detail` returning a
  `competition_type` discriminator plus an exclusive `league{}` / `cup{}`
  sub-object — the same union shape `ClubCompetitionItem` already uses. A
  per-type route pair (`/leagues/{id}/detail` + `/cups/{id}/detail`) was
  rejected: it duplicates the shared half and forces every client to know the
  type up front anyway.
- **Past winners is a list, not per-season history.** The dossier carries one
  champion per completed season; deep dive into a specific season's archive is
  a future read, not part of this surface.
- **Scorers are split current/all-time, each top 10.** Two SQL-clean windows
  (latest season's date range vs unbounded) keep the payload small; penalties
  ride along so clients can render "goals from open play" without double
  counting. Current club is the player's club *today*, not the scoring
  season's roster.
- **Movement is membership diff, not the event log.** `CLUB_PROMOTED` /
  `CLUB_RELEGATED` events are keyed by the *source* league's season id, which
  has no stable relation to this league's season number — scoping them to
  "this season's new teams" is fragile. Instead `promoted_in` / `relegated_out`
  is simply the symmetric difference of `competition_entries` between the
  latest season and its immediately preceding completed season: exactly "the
  teams playing here now that weren't last time", robust to adjacency wiring
  and multi-league rollovers alike.
- **Global admin read, no world scope.** The manager-facing `GetLeague` /
  `GetCup` are world-scoped; the admin console has no world context and this
  endpoint is gated by `requireAdmin`. `competition_id` is globally unique, so
  every query keys on it alone.
- **The season of interest is the latest season, whatever its status.** During
  the off-season gap the latest season is `upcoming`: the dossier shows its
  scheduled fixtures (streak/next), a full subscriber table at zero played (the
  table is **formulated at season creation** — every entrant gets a zeroed
  `competition.standings` row in the same tx that creates the season, entries
  and fixtures — so an upcoming season reads a complete table, not an empty
  one), empty current scorers, past winners from the older completed years, and
  the movement that produced it — all self-consistent. Force-completing a
  season never rolls it into the "current" slot.
- **No migration and no frontend.** Pure read path on top of existing schema;
  the frontend work is a separate task (out of scope per sprint rules).