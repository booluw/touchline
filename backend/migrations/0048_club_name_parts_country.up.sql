-- =====================================================================
-- 0048: country-scoped club name parts
--
-- ref.club_name_parts gains a country dimension so AI-club naming is
-- region-appropriate for seeded countries (per-code regional pools in
-- data/clubs/regional/{code}.json) while keeping a single global fallback
-- (country_code = ''). The pool is still UPSERT-only from cmd/ref-seed and
-- shared across every world, matching the 0027 contract: the DB is the
-- runtime source of truth and admins extend/trim it live via the admin API.
-- =====================================================================

ALTER TABLE ref.club_name_parts
    ADD COLUMN country_code TEXT NOT NULL DEFAULT '';

ALTER TABLE ref.club_name_parts
    DROP CONSTRAINT club_name_parts_pkey;

ALTER TABLE ref.club_name_parts
    ADD PRIMARY KEY (country_code, kind, value);

DROP INDEX IF EXISTS idx_club_name_parts_kind;

CREATE INDEX idx_club_name_parts_kind ON ref.club_name_parts(country_code, kind);