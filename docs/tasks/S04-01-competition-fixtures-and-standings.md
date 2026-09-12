# S04-01 — Implement MVP competition scheduling and standings

**Status:** Implemented  
**Sprint:** 04 — Deterministic football competition  
**Source:** PRD §§30–32, 58, 74; technical plan §§6, 16  
**Depends on:** S03-02
**Resolves:** OPD-01 (via OPD-20), OPD-22
**Key files:** `backend/internal/competition/{service,seeding,clubnames,standings,rollover,util}.go`, `backend/migrations/0025_competition_world.*`, `backend/migrations/0026_fixture_results.*`, `backend/cmd/api/competition_handlers.go`, `frontend/pages/admin/competitions.vue`, `frontend/pages/competitions.vue`, `frontend/composables/useCompetition.ts`

## What to do

Build the MVP competition data and processing needed for persistent league fixtures, tables, promotion, and relegation. Use only approved initial league formats and rules.

## Acceptance criteria

- Fixtures, competitions/rules, matches, and standings are persisted world-scoped in the match/competition schemas.
- Fixture and standings processing is event-driven and auditable.
- Match results update a league table deterministically from the recorded result data.
- Seasonal promotion/relegation is supported by explicit competition rules; unspecified format details are not invented.
- Authorized API/UI surfaces can retrieve a competition, fixtures, and standings.

## Delivery evidence

- **Schema:** migrations `0025` (world-scoped `world.countries`; `competition.competitions` gaining `country_id`, `tier`, `team_count`, `promotions`, `relegations`, `promotes_to`, `relegates_to`; `match.match_inputs`) and `0026` (`match.fixtures` `ht_score`, `at_score`, `completed_at`) are applied and documented in `backend/migrations/README.md`.
- **Admin-declared structure (no invented formats, AC4):** `CreateCountry`, `CreateLeague` (tier/team-count/P&R rules), and `UpdateLeagueAdjacency` (`PATCH /api/admin/leagues/:id/adjacency`) let an admin declare a world's competition shape; `validateAdjacency` enforces symmetric borders at seed time (`ErrAdjacencyMismatch`). Format details are never inferred.
- **Scheduling (AC1, AC2):** `SeedCompetition` dry-run `POST /api/admin/worlds/:id/seed-competition` builds the country's leagues from `WORLD_BOOTSTRAPPED.random_seed` (503 `ErrWorldNotBootstrapped`), places the starter club, fills with `GenerateAIClub` squads (`squadFactory` via `pkg/playergen`), schedules a deterministic home-and-away double round-robin (`roundRobin`, kickoff 19:00 UTC, matchday=round+1), and emits per-league `SEASON_CREATED` + country `COMPETITION_SEEDED` events. Re-seeding a league returns `409 ErrLeagueAlreadySeeded`; the country seeds atomically. (Contract recorded as OPD-22.)
- **Results & standings (AC3):** `ApplyResult` (wired for the S04-02 engine) records fixture score + `completed_at`, upserts a league table for both clubs, and on the final country-wide fixture triggers the `rolloverCountry` cascade: seasons → `completed`, next entries = stayers + promoted-above + relegated-below, next seasons scheduled from `lastScheduledDay`, emitting `SEASON_COMPLETED`, `CLUB_PROMOTED`/`CLUB_RELEGATED`, and rollover `SEASON_CREATED` (OPD-22(5)).
- **Read surface (AC5):** world-scoped `GET /api/competitions`, `GET /api/competitions/:id/fixtures?matchday=`, `GET /api/competitions/:id/standings` (JSON `rows`, ties broken by GD/GF/name per `orderedClubIDs`); admin `GET /api/admin/countries|leagues`. Frontend: manager `/competitions` page (league tabs, table, matchday fixtures with club names) and admin `/admin/competitions` league builder.
- **Verified:** `internal/competition/competition_integration_test.go` (`TestSeedCompetition`, `TestApplyResultAndRollover`), `cmd/api/competition_integration_test.go` (`TestHTTPCompetitionAdminAndReads` — auth 403s, admin create/adjacency/seed, 409 re-seed, world-scoped manager reads) all pass under `go test -p 1 -tags integration -race ./...` (full suite green). `frontend` `pnpm typecheck` and `pnpm lint` clean.