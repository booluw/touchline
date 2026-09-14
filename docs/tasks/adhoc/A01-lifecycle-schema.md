# A01 — Player lifecycle schema foundations

**Status:** Done
**Sprint:** Ad-hoc (player lifecycle)
**Source:** PRD §23, §28; user design session; OPENCODE.md
**Depends on:** S05-02

## What to do

Add the migration 0036 that lays every schema hook the player lifecycle needs: origin tracking on `player.players`, extension of the existing `club.academies` table (from migration 0005) with investment-tier columns, and country-academy intake dedup.

## Schema changes (migration 0036)

### player.players additions

```sql
ALTER TABLE player.players
    ADD COLUMN origin      TEXT NOT NULL DEFAULT 'generated'
        CHECK (origin IN ('generated', 'club_academy', 'street')),
    ADD COLUMN country_id  UUID REFERENCES world.countries(id);
CREATE INDEX idx_players_pool ON player.players(world_id, country_id, status)
    WHERE club_id IS NULL;
```

`country_id` is set when a player enters a country pool (`NULL` world-level pool at bootstrap only). Index is partial (WHERE club_id IS NULL) for fast free-agent lookups.

### club.academies — extended (NOT recreated)

Migration 0005 already created `club.academies` with: `club_id` PK, `is_active`, `coaching_level` (1-10), `recruitment_level` (1-10), `regional_reach` TEXT[], `reputation`, `shutdown_at`, `reopened_at`. This migration extends it with:

```sql
ALTER TABLE club.academies
    ADD COLUMN world_id             UUID REFERENCES world.worlds(id) ON DELETE CASCADE,
    ADD COLUMN investment_tier      INT NOT NULL DEFAULT 1 CHECK (investment_tier BETWEEN 1 AND 5),
    ADD COLUMN facility_level       INT NOT NULL DEFAULT 1 CHECK (facility_level BETWEEN 1 AND 5),
    ADD COLUMN scouting_level       INT NOT NULL DEFAULT 1 CHECK (scouting_level BETWEEN 1 AND 5),
    ADD COLUMN staff_quality        INT NOT NULL DEFAULT 1 CHECK (staff_quality BETWEEN 1 AND 5),
    ADD COLUMN annual_cost          NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN last_intake_season   INT NOT NULL DEFAULT 0;
```

Existing columns reused as-is: `is_active` (active/shuttered), `coaching_level` (1-10), `recruitment_level` (1-10), `regional_reach` TEXT[], `reputation`, `shutdown_at`/`reopened_at`.

### Country-academy intake dedup

```sql
CREATE TABLE world.country_academy_intakes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id      UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    country_id    UUID NOT NULL REFERENCES world.countries(id) ON DELETE CASCADE,
    season_number INT NOT NULL,
    player_count  INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (world_id, country_id, season_number)
);
```

### Down migration

Drops the 0036-added columns from `club.academies`, drops `country_academy_intakes`, drops the partial index, and drops `player.players.origin` + `country_id`. Leaves the original 0005 `club.academies` columns untouched.

## Acceptance criteria

- ✅ Migration 0036 applies cleanly via `migrate up`.
- ✅ Down migration 0036 rolls back cleanly (0005's base `club.academies` columns preserved).
- ✅ Existing `player.players` rows keep `origin = 'generated'` (default).
- ✅ `club.academies` table extended with new lifecycle columns; original columns retained.

## Delivery evidence

- Migration 0036 applied to local test DB (localhost:55432): `version=36, dirty=false`
- Down migration verified: returns to version 35 with clean schema.
- Re-applied up: version 36 clean.
- `psql \d club.academies` confirms all 15 columns (7 original + 7 new + club_id PK).
- `idx_players_pool` partial index created.
- `idx_academies_world` + `idx_academies_active` indexes created.
