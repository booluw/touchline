# S01-01 — Create core schemas and world-scoped migrations

**Status:** Not started  
**Sprint:** 01 — World foundation and event spine  
**Source:** OPENCODE.md; technical plan §§3, 6, 15, 18  
**Depends on:** S00-01

## What to do

Add versioned `golang-migrate` migrations per engine schema. Establish the minimum Phase-0 `world`, `club`, `player`, and `manager` persistence model, including `world.events`, `world.world_config`, `world.nationality_pool`, and `world.news_stories`. Configure migrations for Helm pre-install/pre-upgrade execution.

## Acceptance criteria

- Migrations create the `club`, `player`, `manager`, `transfer`, `finance`, `social`, `match`, `competition`, and `world` schemas.
- Every world-bound entity introduced in the migration set carries `world_id`; scheduler and event records can be queried per world.
- `world.events` persists event identity, type, occurrence time, monotonic world tick, optional actor and causal IDs, and typed JSON payload.
- `world.world_config` can store runtime tick cadences and feature flags; `world.nationality_pool` supports weighted nationality records.
- `finance` has no stored mutable balance column; schema constraints preserve append-only ledger intent.
- Migrations run cleanly from an empty Neon-compatible Postgres database and are repeatable through the documented migration workflow.

## Delivery evidence

- Pending.
