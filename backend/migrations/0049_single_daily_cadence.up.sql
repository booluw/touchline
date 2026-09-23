-- =====================================================================
-- 0049: single daily cadence (IM02)
--
-- The world clock becomes daily-only: weekly/monthly/seasonal work is
-- derived from the day counter (world.worlds.current_day) via the
-- calendar.days_per_week / calendar.days_per_month config keys, so the
-- scheduler no longer registers hour/week/month/season cadences. This
-- migration only removes the leftover config rows (hygiene) — the
-- scheduler already ignores them since 0049's code drops the granularities
-- from the registry. No schema change.
-- =====================================================================

DELETE FROM world.world_config
WHERE config_key IN (
    'tick.hourly_cadence',
    'tick.weekly_cadence',
    'tick.monthly_cadence',
    'tick.seasonal_cadence'
);