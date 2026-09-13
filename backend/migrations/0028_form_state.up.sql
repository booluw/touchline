-- =====================================================================
-- 0028: club form state (S04-02, Phase 2 — internal/form)
--
-- Persistent FormState per club (v1.2 §2.2): a rolling EWMA of recent
-- performance vs. expectation, clamped to [0.85, 1.15] and applied as a
-- multiplier on Attack/Defense before it reaches Simulate. Form is a
-- multi-week trend (alpha=0.2) that decays back toward 1.0 when results
-- normalize. form_string is the derived W-D-W-L-W read-model required by
-- the Part 5 §3 dashboard decision, kept on the row so reads stay cheap.
-- =====================================================================

CREATE TABLE club.form_state (
    club_id           UUID PRIMARY KEY REFERENCES club.clubs(id) ON DELETE CASCADE,
    current_rating    NUMERIC(4,3) NOT NULL DEFAULT 1.000 CHECK (current_rating BETWEEN 0.850 AND 1.150),
    last_updated_tick BIGINT NOT NULL DEFAULT 0,
    form_string       TEXT NOT NULL DEFAULT '' -- W/D/L for the most recent results, oldest first (length <= 5)
);