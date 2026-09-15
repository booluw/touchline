-- =====================================================================
-- 0039: board integrity (S06-02)
--
-- One open mandate per (club, manager, season, category): board review and
-- negotiation may never stack duplicate targets, and a handover that leaves
-- untouched rows behind must not block the incoming manager's fresh set.
-- Partial so resolved (met/broken) rows never collide across seasons and so
-- NULL manager_id (pre-assignment scaffold rows) stay out of the way.
-- =====================================================================
CREATE UNIQUE INDEX IF NOT EXISTS uq_board_mandate_open_per_category
    ON club.board_mandates(club_id, manager_id, season, category)
    WHERE status IN ('pending', 'agreed');