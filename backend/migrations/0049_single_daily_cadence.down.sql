-- Restore the four legacy cadence rows with their pre-IM02 defaults
-- (JSONB-encoded cron specs; ON CONFLICT keeps any value set meanwhile).

INSERT INTO world.world_config (world_id, config_key, config_value)
SELECT world_id, 'tick.hourly_cadence',   to_json('0 * * * *'::text)
FROM world.worlds
ON CONFLICT (world_id, config_key) DO NOTHING;

INSERT INTO world.world_config (world_id, config_key, config_value)
SELECT world_id, 'tick.weekly_cadence',   to_json('0 0 * * 0'::text)
FROM world.worlds
ON CONFLICT (world_id, config_key) DO NOTHING;

INSERT INTO world.world_config (world_id, config_key, config_value)
SELECT world_id, 'tick.monthly_cadence',  to_json('0 0 1 * *'::text)
FROM world.worlds
ON CONFLICT (world_id, config_key) DO NOTHING;

INSERT INTO world.world_config (world_id, config_key, config_value)
SELECT world_id, 'tick.seasonal_cadence', to_json('0 0 1 1 *'::text)
FROM world.worlds
ON CONFLICT (world_id, config_key) DO NOTHING;