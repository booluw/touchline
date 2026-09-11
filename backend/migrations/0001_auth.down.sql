-- Reverts 0001_auth.up.sql
-- CASCADE also drops any cross-schema foreign keys that later migrations
-- (e.g. manager, competition) added pointing INTO this schema's tables,
-- as long as down migrations are applied in reverse numeric order.
DROP SCHEMA IF EXISTS auth CASCADE;
