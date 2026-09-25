-- =====================================================================
-- 0054 down: drop the cup final-date policy columns.
-- =====================================================================
ALTER TABLE competition.competitions
    DROP COLUMN IF EXISTS final_offset_days,
    DROP COLUMN IF EXISTS final_date,
    DROP COLUMN IF EXISTS final_date_mode;