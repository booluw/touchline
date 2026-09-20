-- Revert to the global-only pool. The reference source remains the curated
-- data/clubs/clubnames.json files, so data is always recoverable.

DROP INDEX IF EXISTS idx_club_name_parts_kind;

ALTER TABLE ref.club_name_parts
    DROP CONSTRAINT club_name_parts_pkey;

ALTER TABLE ref.club_name_parts
    ADD PRIMARY KEY (kind, value);

CREATE INDEX idx_club_name_parts_kind ON ref.club_name_parts(kind);

ALTER TABLE ref.club_name_parts
    DROP COLUMN country_code;