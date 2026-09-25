-- =====================================================================
-- 0055 down: drop the live-session uniqueness invariant. Deprecated rows
-- keep their revoked_at tombstones; the login/logout hard-deletes remain a
-- code-level guarantee only.
-- =====================================================================
DROP INDEX IF EXISTS auth.uq_sessions_one_live_per_user;