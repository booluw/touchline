-- =====================================================================
-- TOUCHLINE — FULL DATABASE SCHEMA (PostgreSQL)
-- =====================================================================
-- Companion to Touchline_Technical_Implementation_Plan.md
--
-- Conventions used throughout:
--   * Every world-scoped table carries `world_id` from day one (multi-world
--     decision — see implementation plan, resolved open question #1).
--   * Primary keys are UUIDs (gen_random_uuid()) unless a table is a
--     natural reference table (e.g. ref.nationalities uses an ISO code).
--   * No table ever stores a derived `balance`/`score` as a mutable column
--     where an append-only log can compute it instead (finance ledger,
--     manager reputation, trust). This is deliberate — see plan sections
--     6, 36/67, and the session-model resolution on reputation.
--   * Enumerated values are TEXT + CHECK constraints rather than native
--     Postgres ENUM types, so new values can be added with a simple
--     constraint migration instead of an ALTER TYPE that can lock/deadlock
--     under concurrent access.
--   * `payload`/`detail`/`context` JSONB columns exist for fields that are
--     genuinely variable-shaped (event payloads, explanation breakdowns,
--     negotiation terms) — everything queried/filtered/joined on a regular
--     basis is a real typed column, not buried in JSON.
-- =====================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

