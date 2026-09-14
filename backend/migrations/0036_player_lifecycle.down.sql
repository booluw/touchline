DROP TABLE IF EXISTS world.country_academy_intakes;
DROP INDEX IF EXISTS idx_academies_active;
DROP INDEX IF EXISTS idx_academies_world;
ALTER TABLE club.academies
    DROP COLUMN IF EXISTS last_intake_season,
    DROP COLUMN IF EXISTS annual_cost,
    DROP COLUMN IF EXISTS staff_quality,
    DROP COLUMN IF EXISTS scouting_level,
    DROP COLUMN IF EXISTS facility_level,
    DROP COLUMN IF EXISTS investment_tier,
    DROP COLUMN IF EXISTS world_id;
DROP INDEX IF EXISTS idx_players_pool;
ALTER TABLE player.players
    DROP COLUMN IF EXISTS country_id,
    DROP COLUMN IF EXISTS origin;