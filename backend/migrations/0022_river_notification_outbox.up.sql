-- =====================================================================
-- RIVER schema: river
-- Migration exported from github.com/riverqueue/river (riverpgxv5) v0.44.0
-- Source: riverdriver/riverpgxv5/migration/main/007_notification_outbox_sqlite_jsonb_and_sql_cleanup.up.sql
-- Schema-qualified via TEMPLATE substitution (/* TEMPLATE: schema */ -> river.)
-- Regenerate on river upgrades; keep the source comment above in sync.
-- =====================================================================
SET search_path TO river;

--
-- Notification outbox.
--

CREATE TABLE river.river_notification (
    id bigserial PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    payload text NOT NULL,
    topic text NOT NULL,
    CONSTRAINT topic_length CHECK (length(topic) > 0 AND length(topic) < 128)
);

CREATE INDEX river_notification_created_at_idx ON river.river_notification (created_at);
CREATE INDEX river_notification_topic_id_idx ON river.river_notification (topic, id);

--
-- SQLite JSONB conversion.
--
-- No-op. PostgreSQL already stores River JSON columns as jsonb.

--
-- SQL cleanup.
--

--
-- Drop unused tables `river_client` and `river_client_queue`.
--

DROP TABLE river.river_client_queue;
DROP TABLE river.river_client;

--
-- Adds `DEFAULT 25` to `river_job.max_attempts`.
--

ALTER TABLE river.river_job
    ALTER COLUMN max_attempts SET DEFAULT 25;

--
-- Changes `river_queue.updated_at` to have a default of `CURRENT_TIMESTAMP`.
--

ALTER TABLE river.river_queue
    ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
