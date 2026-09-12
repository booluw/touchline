-- Drop the club name parts pool; the reference source remains the curated
-- data/clubs/clubnames.json files, so data is always recoverable.
DROP TABLE IF EXISTS ref.club_name_parts;