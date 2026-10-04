-- 0060: a club's policy bot that hands its seat to a human is retired, not
-- left unemployed. uq_managers_world_policy_bot guards the single absence bot
-- per world, so it now covers only unemployed club-less bots; before this,
-- the second human takeover in a world violated it ("free incumbent manager").
DROP INDEX IF EXISTS manager.uq_managers_world_policy_bot;
CREATE UNIQUE INDEX uq_managers_world_policy_bot
    ON manager.managers(world_id)
    WHERE is_policy_bot = TRUE AND current_club_id IS NULL AND status = 'unemployed';
