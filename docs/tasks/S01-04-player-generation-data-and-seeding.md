# S01-04 — Deliver nationality-aware player generation and seed data

**Status:** Not started  
**Sprint:** 01 — World foundation and event spine  
**Source:** Technical plan §7, §16; OPENCODE.md  
**Depends on:** S01-01

## What to do

Complete the standalone player-generation subsystem with curated, versioned per-nationality name data and weighted nationality selection. Use it to seed believable players without licensing real players.

## Acceptance criteria

- Curated name datasets are added for the agreed nationality coverage; provenance/licensing for each source is documented before inclusion.
- Generator selection uses `world.nationality_pool` weights and the seeded random source.
- Generated names follow the supported nationality/culture rules and collision handling prevents duplicate first/last-name pairs within a world.
- Player generation remains independent of domain DB code and is unit-tested, including deterministic seeded output.
- World seeding can create a squad of generated players and persist their nationality/name data.

## Delivery evidence

- Pending.
