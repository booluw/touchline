-- =====================================================================
-- 0056: the in-game calendar date as a SQL function (IM25)
--
-- OPD-24 defines a world's date as COALESCE(launched_at, created_at) +
-- current_day days. Contract, wage-commitment, age and history date math used
-- the server's real CURRENT_DATE instead, which drifts from the game in any
-- world whose clock is not running at real-time scale (tick.day_length) or
-- that was paused. These helpers give every query one definition:
--   world.world_date(world_id)     -> the world's calendar date
--   world.club_world_date(club_id) -> the calendar date of the club's world
-- The launch day is taken in UTC (the OPD-42 epoch is UTC midnight), so the
-- result does not depend on the session time zone. Both are STABLE (they
-- read world.worlds) and return NULL for unknown ids.
-- =====================================================================
CREATE OR REPLACE FUNCTION world.world_date(p_world_id uuid)
RETURNS date
LANGUAGE sql STABLE AS $$
    SELECT (COALESCE(w.launched_at, w.created_at) AT TIME ZONE 'UTC')::date
           + w.current_day::int
    FROM world.worlds w
    WHERE w.id = p_world_id
$$;

CREATE OR REPLACE FUNCTION world.club_world_date(p_club_id uuid)
RETURNS date
LANGUAGE sql STABLE AS $$
    SELECT world.world_date(c.world_id)
    FROM club.clubs c
    WHERE c.id = p_club_id
$$;
