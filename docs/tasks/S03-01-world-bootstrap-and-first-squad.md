# S03-01 — Produce a seeded world, club, and first squad

**Status:** Not started  
**Sprint:** 03 — Seeded playable world bootstrap  
**Source:** Technical plan §§6–7, 16, 18; PRD §§5, 74  
**Depends on:** S01-04, S02-02, S02-03

## What to do

Implement the Phase-0 bootstrap path that creates a world and generates a club with a squad of procedurally named, nationalized players. Limit initial competition/assignment semantics to the approved open-decision outcome.

## Acceptance criteria

- A bootstrap operation creates a world, club, manager assignment, and player squad under one `world_id`.
- Every generated player uses the shared player-generation subsystem and is persisted in the player schema.
- Bootstrap emits auditable world events for material generated state.
- A generated club and squad are retrievable through an authorized server API.
- The resulting world can receive its daily tick.

## Delivery evidence

- Pending.
