-- =====================================================================
-- 0032: canonical world calendar day (S04-04, OPD-24)
--
-- world.worlds.current_tick is a monotonic ordering/audit counter advanced
-- by every granularity (hourly|daily|weekly|monthly|seasonal, OPD-17(3)).
-- The matchday runner previously read it as "days since launch", so hourly
-- and other non-daily emissions inflated the fixture calendar (~25x/day).
--
-- current_day is the canonical in-game calendar: the number of in-game days
-- since launch. It is incremented ONLY by WORLD_TICK{daily} emissions (the
-- scheduler does both updates in the same transaction), and the matchday
-- runner derives worldDate = COALESCE(launched_at, created_at) +
-- current_day days. Hourly/weekly/monthly/seasonal emissions never touch it.
--
-- Backfill: for any world that already ran, current_day is computed from the
-- count of stored daily WORLD_TICK events so already-seeded fixtures stay
-- eligible and the calendar does not jump.
-- =====================================================================

ALTER TABLE world.worlds
    ADD COLUMN current_day BIGINT NOT NULL DEFAULT 0;

UPDATE world.worlds w
SET current_day = COALESCE((
    SELECT count(*)
    FROM world.events e
    WHERE e.world_id = w.id
      AND e.event_type = 'WORLD_TICK'
      AND e.payload->>'granularity' = 'daily'
), 0)
WHERE w.current_day = 0;