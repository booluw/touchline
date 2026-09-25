-- =====================================================================
-- 0054: cup final-date policy (IM10)
--
-- The final of a knockout cup (country or regional scope) is either:
--   * calculated (recurring)      -- the engine derives it each season from
--                                   the scope's latest league fixture, offset
--                                   by final_offset_days (default 3) and
--                                   weekday-snapped to the cup's resolved
--                                   allowed weekdays; or
--   * fixed (one-time)            -- final_date pins the final on exactly that
--                                   date, authoritative and never recalculated.
-- final_date NULL + calculated is the IM05-compatible default: it reproduces
-- the existing anchored final ("first allowed weekday at least 3 game-days
-- after the latest league fixture") byte-for-byte.
-- =====================================================================
ALTER TABLE competition.competitions
    ADD COLUMN final_date_mode  TEXT NOT NULL DEFAULT 'calculated'
        CHECK (final_date_mode IN ('calculated', 'fixed')),
    ADD COLUMN final_date       DATE NULL,
    ADD COLUMN final_offset_days INT NOT NULL DEFAULT 3
        CHECK (final_offset_days >= 0);