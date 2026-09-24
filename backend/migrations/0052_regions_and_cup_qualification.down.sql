-- Reverse IM06: drop the cup tables, the region scope columns, and the
-- regions table (reverse order of the .up.sql).
DROP TABLE IF EXISTS competition.manager_cup_choices;

DROP TABLE IF EXISTS competition.cup_qualification;

DROP INDEX IF EXISTS idx_competitions_region;
ALTER TABLE competition.competitions DROP COLUMN IF EXISTS region_id;

DROP INDEX IF EXISTS idx_countries_region;
ALTER TABLE world.countries DROP COLUMN IF EXISTS region_id;

DROP TABLE IF EXISTS world.regions;