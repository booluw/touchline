-- =====================================================================
-- SCHEMA: player — injury engine foundations (S08-03)
--
--   1. injury_setbacks is the audit + idempotency anchor for recovery
--      setbacks (PRD §27): while an injury is open and past 55% of its
--      planned recuperation, the weekly recovery scan may roll a setback
--      that pushes expected_recovery_date later. The (injury_id, week)
--      primary key makes a redelivered weekly tick a no-op — the same
--      setback can never be applied twice.
--
-- There is deliberately NO change to player.injuries itself: it already
-- carries injury_type / severity / expected_recovery_date /
-- actual_recovery_date / recurrence_risk / match_id and the open-injury
-- eligibility gate used by squad selection.
-- =====================================================================

CREATE TABLE player.injury_setbacks (
    injury_id   UUID NOT NULL REFERENCES player.injuries(id) ON DELETE CASCADE,
    week        BIGINT NOT NULL,
    days_added  INT NOT NULL CHECK (days_added BETWEEN 1 AND 14),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (injury_id, week)
);

CREATE INDEX idx_injury_setbacks_injury ON player.injury_setbacks(injury_id, week);