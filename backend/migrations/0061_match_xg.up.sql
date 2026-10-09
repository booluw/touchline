-- 0061: expected goals per side on the persisted match (IM58).
-- NULL = played before the engine computed xG; never backfilled (it cannot
-- be recomputed without re-simulating).
ALTER TABLE match.matches
    ADD COLUMN home_xg NUMERIC(4,2),
    ADD COLUMN away_xg NUMERIC(4,2);
