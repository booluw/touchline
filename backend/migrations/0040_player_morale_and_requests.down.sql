-- =====================================================================
-- 0040 down: reverse player morale/appearances/requests + social journal
-- =====================================================================

DROP TABLE IF EXISTS social.relationship_events;

DROP TABLE IF EXISTS player.player_transfer_requests;

DROP TABLE IF EXISTS player.player_appearances;

ALTER TABLE player.contracts
    DROP COLUMN IF EXISTS squad_role;

ALTER TABLE player.player_condition
    DROP COLUMN IF EXISTS transfer_request_cooldown_until,
    DROP COLUMN IF EXISTS playing_time_pct,
    DROP COLUMN IF EXISTS morale;