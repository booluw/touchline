DROP INDEX IF EXISTS manager.uq_managers_world_policy_bot;
CREATE UNIQUE INDEX uq_managers_world_policy_bot
    ON manager.managers(world_id)
    WHERE is_policy_bot = TRUE AND current_club_id IS NULL;
