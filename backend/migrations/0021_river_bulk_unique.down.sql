-- =====================================================================
-- RIVER schema: river
-- Migration exported from github.com/riverqueue/river (riverpgxv5) v0.44.0
-- Source: riverdriver/riverpgxv5/migration/main/006_bulk_unique.down.sql
-- Schema-qualified via TEMPLATE substitution (/* TEMPLATE: schema */ -> river.)
-- Regenerate on river upgrades; keep the source comment above in sync.
-- =====================================================================
SET search_path TO river;


--
-- Drop `river_job.unique_states` and its index.
--

DROP INDEX river.river_job_unique_idx;

ALTER TABLE river.river_job
    DROP COLUMN unique_states;

CREATE UNIQUE INDEX IF NOT EXISTS river_job_kind_unique_key_idx ON river.river_job (kind, unique_key) WHERE unique_key IS NOT NULL;

--
-- Drop `river_job_state_in_bitmask` function.
--
DROP FUNCTION river.river_job_state_in_bitmask;
