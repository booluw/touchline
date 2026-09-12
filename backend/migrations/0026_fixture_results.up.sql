-- =====================================================================
-- 0026: fixture results (S04-01)
--
-- match.matches carries scores only after the S04-02 engine runs; the
-- competition layer needs the final score on the fixture itself so that
-- standings rollups and the fixtures API stay readable even for quick
-- round-robin results. completed_at mirrors match.matches.ended_at.
-- =====================================================================

ALTER TABLE match.fixtures
    ADD COLUMN ht_score     INT,
    ADD COLUMN at_score     INT,
    ADD COLUMN completed_at TIMESTAMPTZ;