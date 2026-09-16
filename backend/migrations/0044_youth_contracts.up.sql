-- =====================================================================
-- SCHEMA: player — academy youth contracts (S08-01)
-- =====================================================================

-- Distinguish academy youth deals from senior starter contracts so the weekly
-- wage run can treat developing prospects like any other wage commitment
-- while signings can target 'youth' specifically. One active youth contract
-- per player is enforced by the partial unique index: a redelivered intake
-- cannot double-sign the same prospect (base migration 0006 was pre-row,
-- so backfilling is a no-op — no existing rows).
ALTER TABLE player.contracts
    ADD COLUMN contract_type TEXT NOT NULL DEFAULT 'senior'
        CHECK (contract_type IN ('senior', 'youth'));

CREATE UNIQUE INDEX contracts_one_active_youth_per_player
    ON player.contracts (player_id) WHERE contract_type = 'youth';