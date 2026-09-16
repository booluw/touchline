DROP INDEX IF EXISTS uq_managers_world_policy_bot;

ALTER TABLE club.club_lineups DROP COLUMN IF EXISTS updated_at;

DROP TABLE IF EXISTS manager.policies;

ALTER TABLE manager.managers DROP COLUMN IF EXISTS away_since;
ALTER TABLE manager.managers DROP COLUMN IF EXISTS away_auto;
ALTER TABLE manager.managers DROP COLUMN IF EXISTS consecutive_missed;
ALTER TABLE manager.managers DROP COLUMN IF EXISTS last_activity_at;