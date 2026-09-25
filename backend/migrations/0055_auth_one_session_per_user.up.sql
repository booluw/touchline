-- =====================================================================
-- 0055: one live session per user account + server-side logout (IM13)
--
-- A user account may hold at most one *live* refresh session. Login
-- hard-deletes the prior live row inside the login transaction, and logout
-- deletes the row for the presented refresh token. This migration:
--   1. collapses any pre-existing duplicate live sessions (keeps the newest
--      per user, by (created_at, id)); and
--   2. enforces the invariant in the DB with a partial unique index on live
--      rows only, so a second live row can never be inserted, whatever the
--      code path. Deprecated/rotated rows carry revoked_at and stay as
--      tombstones — they are invisible to the index.
-- =====================================================================
DELETE FROM auth.sessions AS keep_nothing
USING auth.sessions AS later
WHERE keep_nothing.revoked_at IS NULL
  AND later.revoked_at IS NULL
  AND keep_nothing.user_id = later.user_id
  AND (keep_nothing.created_at, keep_nothing.id) < (later.created_at, later.id);

CREATE UNIQUE INDEX uq_sessions_one_live_per_user
    ON auth.sessions(user_id)
    WHERE revoked_at IS NULL;