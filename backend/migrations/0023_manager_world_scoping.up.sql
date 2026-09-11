-- =====================================================================
-- SCHEMA: manager (world scoping for human accounts)
--
-- Records the S02-01 account model (resolved with the product lead, OPD-15):
--   * one account (auth.users) may inhabit MANY worlds — at most one manager
--     row per user per world (uq_managers_user_world);
--   * a user may hold at most ONE job across all worlds (status='active'
--     AND current_club_id IS NOT NULL), enforced by uq_manager_one_job_per_user.
-- =====================================================================

-- One manager row per user per world.
CREATE UNIQUE INDEX uq_managers_user_world
    ON manager.managers(user_id, world_id)
    WHERE user_id IS NOT NULL;

-- At most one active job per user, platform-wide (partial index only rows
-- currently employed, so a user can be active-but-jobless in many worlds).
CREATE UNIQUE INDEX uq_manager_one_job_per_user
    ON manager.managers(user_id)
    WHERE user_id IS NOT NULL AND status = 'active' AND current_club_id IS NOT NULL;