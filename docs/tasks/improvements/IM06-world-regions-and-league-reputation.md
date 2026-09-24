# IM06 — World regions, country-to-region assignment, and editable league reputation

**Status:** Not started
**Sprint:** Improvements (competition scheduling)
**Source:** Product decision (manual session)
**Depends on:** S04-01 (country/league admin seam, migration `0025`); IM05
(scheduling seams); migration `0037` (cup membership hooks). Lays the schema
+ admin surface that IM07 (qualification engine) and IM08/IM09 (regional cup
campaigns + resolution) build on.

## What to do

Give every world a **region layer** and turn the dormant `competitions.reputation`
column into a real, admin-managed **league reputation** that later milestones
use to suggest cup qualification slots.

1. **Regions** (`world.regions`): a region belongs to exactly one world and has
   a world-unique name. Countries are assigned to a region (`nullable` — a
   country can exist unassigned).
2. **Regional competition scope**: `competition.competitions` gains a nullable
   `region_id`; a regional competition is the one place the later milestones
   create `competition_type 'continental'` cups.
3. **Qualification + choice tables** (schema only in this task; the engine that
   reads them is IM07/IM09):
   - `competition.cup_qualification` — per-league position-band rows
     `(cup_id, league_id, from_position, to_position NULL = through last place)`,
     the single mechanism for "top-4", "6th–10th", and "whole league".
   - `competition.manager_cup_choices` — a club opting into a specific cup when
     it is double-qualified (consumed by IM09).
4. **Editable league reputation** (0–100, default 10): surfaced on the `League`
   type and league reads, admin-editable via a `PATCH`. The value drives the
   IM08 wizard's *suggested* qualification bands; the engine always reads the
   explicit band rows.
5. **Admin UX for the foundation**: regions CRUD, reassign a country to a
   region, edit league reputation — all world-scoped, all documented.

## Behaviour

### Regions (`world.regions`)

```sql
CREATE TABLE world.regions (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    UNIQUE (world_id, name)
);

ALTER TABLE world.countries
    ADD COLUMN region_id UUID REFERENCES world.regions(id) ON DELETE SET NULL;
CREATE INDEX idx_countries_region ON world.countries(region_id);

ALTER TABLE competition.competitions
    ADD COLUMN region_id UUID REFERENCES world.regions(id);
```

- A region is created under a world; `UNIQUE(world_id, name)` prevents name
  collisions within a world (cross-world regions may share names).
- Countries are re-assignable (`PATCH /api/admin/countries/:id/region` with
  `region_id` or `null` to clear). Deleting a region clears assignments
  (`ON DELETE SET NULL`) — never orphan countries.
- `competition.competitions.region_id` is **nullable and exclusive with
  `country_id`**: a competition is scoped to a country XOR a region. A shared
  schema CHECK is added in IM08 when regional cups are created; this task only
  adds the column.

### Qualification + choice tables (schema only)

```sql
CREATE TABLE competition.cup_qualification (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cup_id        UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    league_id     UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    from_position INT NOT NULL DEFAULT 1 CHECK (from_position >= 1),
    to_position   INT CHECK (to_position IS NULL OR to_position >= from_position),
    UNIQUE (cup_id, league_id)
);

CREATE TABLE competition.manager_cup_choices (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id  UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    cup_id   UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    chosen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (club_id, cup_id)
);
```

- `fr_to_position NULL` means "through last place", so a `1..NULL` row is the
  whole-league / "all clubs" case and `6..10` is a position band. Future
  country-cup rows can reuse this without a separate mode column.
- The choice row means "this club opts into this cup" among its conflicting
  entitlements; absence of movement is the IM09 default. `UNIQUE(club_id, cup_id)`
  makes the choice idempotent — recording it again is a no-op upsert.

### Editable league reputation

- Reuse `competition.competitions.reputation` (existing column, default 10 —
  currently only read by `internal/match/service.go:662`, which writes it to
  `FixtureContext.LeagueTier`, a field **not consumed anywhere** in the engine;
  verified — no behavior change). Range enforced 0–100.
- `League` gains `Reputation int json:"reputation"`; `ListLeagues` /
  `ListCountryLeagues` / the single-league read include it.
- `PATCH /api/admin/leagues/:id/reputation` `{"reputation": N}` (world-scoped,
  `400` out of range, `404` missing league, `403` no world context).
- Reputation is the *input* to the IM08 default-band calculator, nothing more;
  the qualification engine itself never consults it.

### New endpoints

- `POST /api/admin/regions` `{world_id, name}` → `201` region (`409` name
  collision in world, `404` world, `422` empty name).
- `GET /api/admin/regions?world_id=…` → all regions of the world.
- `PATCH /api/admin/regions/:id` `{name}` → rename.
- `DELETE /api/admin/regions/:id` → delete (countries fall back to unassigned).
- `PATCH /api/admin/countries/:id/region` `{region_id | null}` → assign/clear.
- `PATCH /api/admin/leagues/:id/reputation` `{reputation}` → set league
  reputation.
- Reads surface the assignment: country reads include `region_id`; a manager
  `GET /api/countries` may include the region ref (world-scoped).

## Changes

### migrations

- `0052_regions_and_cup_qualification.{up,down}.sql`: the four DDL blocks
  above (regions, `countries.region_id`, `competitions.region_id`,
  `cup_qualification`, `manager_cup_choices`).
- `0053_league_reputation.sql` only if separate change-set hygiene is
  preferred; otherwise reputation reuse needs no migration (column exists).

### internal/competition

- `regions.go` (new): `world.regions` CRUD (create/list/rename/delete),
  `SetCountryRegion`, `AssignCountryRegion` world-scoping (`ErrWorldNotFound`,
  `ErrRegionNotFound`, `ErrCountryNotFound`, `ErrCompetitionWorldMismatch`).
- `service.go`: `League.Reputation` (+ scan columns); reputation bounds error.

### internal/world

- If region CRUD is cleaner under the world package (regions are world
  entities), place `regions.go` there instead — decide by reading the package
  boundaries; the HTTP surface must not change either way.

### internal/httpapi + openapi

- `router.go`: the admin region/reputation/country-assignment routes
  (`requireAuth` + `requireAdmin`, world-scoped).
- `openapi.yaml`: `Region`, extended `Country` + `League` refs, the new routes;
  `TestDocsCoverRouter` updated.
- `handlers.go`/new `region_handlers.go`: bind + validate the requests.

## Tests

- Unit: reputation clamp (0 and 100 allowed, −1 and 101 rejected); name
  normalization for regions.
- Integration (CI-only): create region under world A, verify invisible to world
  B; same-name collision `409` within a world, allowed across worlds; assign +
  clear a country's region; delete region → countries `NULL`; league
  reputation PATCH round-trips through `ListLeagues` and `ListCountryLeagues`;
  `403` for no world context; `404`/`409`/`422` paths.
- Docs: `TestDocsCoverRouter` + `TestDocsOpenAPIValid` green.

## Docs

- `docs/tasks/improvements/IM06-world-regions-and-league-reputation.md` (this file).
- `docs/tasks/improvements/IM07-cup-qualification-engine.md` (next milestone,
  consumes the two tables).
- `docs/how-to/glossary.md`: `region`, `league reputation`, `cup qualification`,
  "position band".
- `docs/product_manager.md`: the region model (a region hosts regional cups;
  a country participates in exactly one region) as a recorded product decision.

## Recorded decisions

- **Regions are a hard product entity** (world → regions → countries), not a
  field on the cup: countries "are part of a region and world", so the
  geography lives on the countries, and a regional cup merely points at a
  region (product decision, manual session).
- **Country ↔ region is optional on the data model** (`nullable`); only the
  geography an admin actually assigns participates in regional cups.
- **League reputation reuses the dormant `competitions.reputation` column**
  (verified safe: its only consumer, `FixtureContext.LeagueTier`, is never
  read). It must keep a 0–100 documentation contract as IM08's default-band
  input; the match engine wiring is untouched and regression-guarded by the
  existing match integration tests.
- **Position bands are the universal qualification primitive** — `6..10`, `1..4`,
  `1..NULL` (whole league). No separate mode column; IM08/IM09 define creation
  and resolution over this table.