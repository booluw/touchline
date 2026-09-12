ALTER TABLE match.fixtures
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS at_score,
    DROP COLUMN IF EXISTS ht_score;