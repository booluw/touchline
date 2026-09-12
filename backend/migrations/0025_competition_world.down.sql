-- Reverts 0025_competition_world.up.sql
DROP INDEX IF EXISTS match.idx_match_inputs_match;
DROP TABLE IF EXISTS match.match_inputs;
DROP INDEX IF EXISTS match.idx_fixtures_world_status;
ALTER TABLE competition.competition_rules
    DROP COLUMN IF EXISTS relegates_to_competition_id,
    DROP COLUMN IF EXISTS promotes_to_competition_id,
    DROP COLUMN IF EXISTS relegations,
    DROP COLUMN IF EXISTS promotions;
ALTER TABLE competition.competitions
    DROP COLUMN IF EXISTS team_count,
    DROP COLUMN IF EXISTS tier,
    DROP COLUMN IF EXISTS country_id;
DROP TABLE IF EXISTS world.countries;