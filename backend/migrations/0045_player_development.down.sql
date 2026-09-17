DROP TABLE player.player_development;

ALTER TABLE player.player_appearances
    DROP COLUMN rating,
    DROP COLUMN goals,
    DROP COLUMN assists;