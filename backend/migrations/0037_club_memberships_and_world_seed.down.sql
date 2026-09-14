ALTER TABLE world.worlds
    DROP COLUMN IF EXISTS world_seed;

DROP INDEX IF EXISTS uq_club_one_league;
DROP INDEX IF EXISTS idx_club_competitions_club;
DROP INDEX IF EXISTS idx_club_competitions_competition;
DROP TABLE IF EXISTS competition.club_competitions;