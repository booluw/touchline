-- =====================================================================
-- RIVER schema: river
-- Migration exported from github.com/riverqueue/river (riverpgxv5) v0.44.0
-- Source: riverdriver/riverpgxv5/migration/main/004_pending_and_more.down.sql
-- Schema-qualified via TEMPLATE substitution (/* TEMPLATE: schema */ -> river.)
-- Regenerate on river upgrades; keep the source comment above in sync.
-- =====================================================================
SET search_path TO river;

ALTER TABLE river.river_job ALTER COLUMN args DROP NOT NULL;

ALTER TABLE river.river_job ALTER COLUMN metadata DROP NOT NULL;
ALTER TABLE river.river_job ALTER COLUMN metadata DROP DEFAULT;

-- It is not possible to safely remove 'pending' from the river_job_state enum,
-- so leave it in place.

ALTER TABLE river.river_job DROP CONSTRAINT finalized_or_finalized_at_null;
ALTER TABLE river.river_job ADD CONSTRAINT finalized_or_finalized_at_null CHECK (
  (state IN ('cancelled', 'completed', 'discarded') AND finalized_at IS NOT NULL) OR finalized_at IS NULL
);

CREATE OR REPLACE FUNCTION river.river_job_notify()
  RETURNS TRIGGER
  AS $$
DECLARE
  payload json;
BEGIN
  IF NEW.state = 'available' THEN
    -- Notify will coalesce duplicate notifications within a transaction, so
    -- keep these payloads generalized:
    payload = json_build_object('queue', NEW.queue);
    PERFORM
      pg_notify('river_insert', payload::text);
  END IF;
  RETURN NULL;
END;
$$
LANGUAGE plpgsql;

CREATE TRIGGER river_notify
  AFTER INSERT ON river.river_job
  FOR EACH ROW
  EXECUTE PROCEDURE river.river_job_notify();

DROP TABLE river.river_queue;

ALTER TABLE river.river_leader
    ALTER COLUMN name DROP DEFAULT,
    DROP CONSTRAINT name_length,
    ADD CONSTRAINT name_length CHECK (char_length(name) > 0 AND char_length(name) < 128);