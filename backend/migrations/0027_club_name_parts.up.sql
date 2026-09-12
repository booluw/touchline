-- =====================================================================
-- 0027: global club name parts (stems + suffixes)
--
-- Deterministic AI-club naming pools for S04-01 league seeding. The pool is
-- global (ref schema) and shared across every world, matching the player
-- name pools (0002_ref). Unlike ref.name_pool, ref.club_name_parts is
-- UPSERT-only from cmd/ref-seed: the DB is the runtime source of truth and
-- admins extend/trim it live via the admin API, so a bulk re-ingest must
-- never delete admin-curated entries.
-- =====================================================================

CREATE TABLE ref.club_name_parts (
    kind             TEXT NOT NULL CHECK (kind IN ('stem', 'suffix')),
    value            TEXT NOT NULL,
    frequency_weight NUMERIC NOT NULL DEFAULT 1.0,
    PRIMARY KEY (kind, value)
);

CREATE INDEX idx_club_name_parts_kind ON ref.club_name_parts(kind);