-- =====================================================================
-- SCHEMA: player + club + world — player lifecycle foundations (A01)
--
-- Lays the schema hooks the player lifecycle needs:
--   1. player.players gains `origin` (generated | club_academy | street)
--      and `country_id` (country-scoped free-agent pool membership;
--      NULL = world-level bootstrap pool).
--   2. club.academies — EXTENDS the PRD §23 model from migration 0005
--      (is_active, coaching_level 1-10, recruitment_level 1-10,
--      regional_reach TEXT[], reputation) with the investment-tier and
--      lifecycle columns: world_id, investment_tier, facility_level,
--      scouting_level, staff_quality, annual_cost, last_intake_season.
--   3. world.country_academy_intakes — idempotency anchor for the
--      seasonal street-kid intake (one row per country+season).
-- =====================================================================

ALTER TABLE player.players
    ADD COLUMN origin      TEXT NOT NULL DEFAULT 'generated'
        CHECK (origin IN ('generated', 'club_academy', 'street')),
    ADD COLUMN country_id  UUID REFERENCES world.countries(id);

-- Free-agent pool lookup: free agents share (world, country), are
-- unaffiliated, and carry the free_agent status.
CREATE INDEX idx_players_pool
    ON player.players(world_id, country_id, status)
    WHERE club_id IS NULL;

-- Academy investment model (PRD §23). Migration 0005 created the base
-- table (coaching_level 1-10, recruitment_level 1-10, is_active,
-- regional_reach TEXT[], reputation, shutdown_at, reopened_at). The
-- investment tier, facility, scouting, staff, and lifecycle columns are
-- added here; they feed the seasonal intake skew (A04).
ALTER TABLE club.academies
    ADD COLUMN world_id             UUID REFERENCES world.worlds(id) ON DELETE CASCADE,
    ADD COLUMN investment_tier      INT NOT NULL DEFAULT 1 CHECK (investment_tier BETWEEN 1 AND 5),
    ADD COLUMN facility_level       INT NOT NULL DEFAULT 1 CHECK (facility_level BETWEEN 1 AND 5),
    ADD COLUMN scouting_level       INT NOT NULL DEFAULT 1 CHECK (scouting_level BETWEEN 1 AND 5),
    ADD COLUMN staff_quality        INT NOT NULL DEFAULT 1 CHECK (staff_quality BETWEEN 1 AND 5),
    ADD COLUMN annual_cost          NUMERIC(14,2) NOT NULL DEFAULT 0,
    ADD COLUMN last_intake_season   INT NOT NULL DEFAULT 0;
CREATE INDEX idx_academies_world ON club.academies(world_id);
CREATE INDEX idx_academies_active ON club.academies(world_id, is_active);

-- Idempotency anchor for the seasonal country-academy (street kids)
-- intake: exactly one row per (world, country, season) so a redelivered
-- ROLLOVER / SEASON_COMPLETED hook can never double-intake a country.
CREATE TABLE world.country_academy_intakes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    world_id      UUID NOT NULL REFERENCES world.worlds(id) ON DELETE CASCADE,
    country_id    UUID NOT NULL REFERENCES world.countries(id) ON DELETE CASCADE,
    season_number INT NOT NULL,
    player_count  INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (world_id, country_id, season_number)
);