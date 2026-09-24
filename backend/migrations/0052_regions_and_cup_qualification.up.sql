-- =====================================================================
-- 0052: world regions + cup qualification rows + manager cup choices (IM06)
--
-- IM06 world-region layer and the qualification/choice tables the cup
-- engine (IM07) and regional cup campaigns (IM08/IM09) build on:
--
--   * world.regions groups a world's countries into administrative regions
--     (a country belongs to at most one region, or none — region_id is
--     dropped to NULL when its region is deleted). Later milestones scope
--     continental ('regional') cup competitions to a region.
--   * competition.competitions.region_id is the nullable regional scope of a
--     competition, exclusive of country_id (that XOR is enforced in service
--     code when regional cups are created in IM08 — schema stays additive).
--   * competition.cup_qualification records a per-league position band a cup
--     draws entrants from: from_position..to_position where NULL to_position
--     means "through last place". This is the universal qualification
--     primitive (6..10, 1..4, or 1..NULL for a whole league).
--   * competition.manager_cup_choices is a club's opt-in to a specific cup
--     when it qualifies for several (consumed by the IM09 resolution sweep).
-- =====================================================================
CREATE TABLE world.regions (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    name     TEXT NOT NULL,
    UNIQUE (world_id, name)
);

ALTER TABLE world.countries
    ADD COLUMN region_id UUID REFERENCES world.regions(id) ON DELETE SET NULL;
CREATE INDEX idx_countries_region ON world.countries(region_id);

-- A regional competition's scope: references a world region instead of a
-- country. Nullable so country-scoped leagues/cups stay unchanged.
ALTER TABLE competition.competitions
    ADD COLUMN region_id UUID REFERENCES world.regions(id);
CREATE INDEX idx_competitions_region ON competition.competitions(region_id);

-- Position-band qualification: the league's most recent COMPLETED season
-- provides the rank order; to_position NULL means through last place.
CREATE TABLE competition.cup_qualification (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cup_id        UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    league_id     UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    from_position INT NOT NULL DEFAULT 1 CHECK (from_position >= 1),
    to_position   INT CHECK (to_position IS NULL OR to_position >= from_position),
    UNIQUE (cup_id, league_id)
);

-- A double-qualified club's opt-in to a specific cup (IM09). Idempotent by
-- the unique key: re-recording the same choice is a no-op upsert.
CREATE TABLE competition.manager_cup_choices (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    club_id  UUID NOT NULL REFERENCES club.clubs(id) ON DELETE CASCADE,
    cup_id   UUID NOT NULL REFERENCES competition.competitions(id) ON DELETE CASCADE,
    chosen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (club_id, cup_id)
);