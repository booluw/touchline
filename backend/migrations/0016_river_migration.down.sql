-- =====================================================================
-- RIVER schema: river
-- Migration exported from github.com/riverqueue/river (riverpgxv5) v0.44.0
-- Source: riverdriver/riverpgxv5/migration/main/001_create_river_migration.down.sql
-- Schema-qualified via TEMPLATE substitution (/* TEMPLATE: schema */ -> river.)
-- Regenerate on river upgrades; keep the source comment above in sync.
-- =====================================================================
SET search_path TO river;

DROP TABLE river.river_migration;
DROP SCHEMA IF EXISTS river CASCADE;
