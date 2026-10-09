ALTER TABLE match.matches
    DROP COLUMN IF EXISTS home_xg,
    DROP COLUMN IF EXISTS away_xg;
