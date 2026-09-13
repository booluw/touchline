-- =====================================================================
-- 0030: fixture standings application (S04-02)
--
-- The deterministic match engine completes a fixture (status 'completed')
-- the moment PlayFixture runs; the competition layer later records the
-- standing/table write in its own ApplyResult step. standings_applied_at
-- is the guard that makes that second step idempotent: fixtures are
-- single-application whether they were completed by the engine or by a
-- manual ApplyResult, without conflating the engine's simulation with the
-- standings write.
-- =====================================================================

ALTER TABLE match.fixtures
    ADD COLUMN standings_applied_at TIMESTAMPTZ;