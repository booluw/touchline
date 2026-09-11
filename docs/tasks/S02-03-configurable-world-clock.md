# S02-03 — Implement the configurable world clock

**Status:** Not started  
**Sprint:** 02 — Authenticated, schedulable worlds  
**Source:** PRD §4; technical plan §§5, 16, 18  
**Depends on:** S01-02

## What to do

Implement the scheduler service using runtime `world_config` cadence values. Publish world-scoped `WORLD_TICK` events, including granularity, without embedding world cadence in code.

## Acceptance criteria

- Scheduler reads tick cadence configuration per world and emits world-scoped `WORLD_TICK` events through the event bus.
- Tick payload identifies granularity sufficient for engine subscriptions.
- A cadence change takes effect without an application redeploy.
- Engines can subscribe/dispatch only the tick granularities they own.
- Integration coverage demonstrates a configured daily tick arriving at a worker handler for the intended world.

## Delivery evidence

- Pending.
