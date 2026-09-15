-- =====================================================================
-- 0041 down: reverse social profile support
-- =====================================================================

-- Drop only the seeded rows (reason prefixed 'backfilled:') — live trust
-- events written by the app carry un-prefixed reasons and stay untouched.
DELETE FROM social.trust_events WHERE reason LIKE 'backfilled:%';

DROP INDEX IF EXISTS idx_messages_sender;