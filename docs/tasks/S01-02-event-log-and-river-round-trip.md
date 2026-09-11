# S01-02 — Implement the event log and River event round trip

**Status:** Not started  
**Sprint:** 01 — World foundation and event spine  
**Source:** Technical plan §§4, 14, 16; OPENCODE.md  
**Depends on:** S01-01

## What to do

Implement `pkg/eventbus.EventBus` with River as the Phase-0 transport. Publish meaningful events by appending the event log and enqueueing work, then consume and dispatch events through the worker without changing engine-facing interfaces.

## Acceptance criteria

- A published test event is written to `world.events`, queued through River, consumed by the worker, and handled exactly according to the defined idempotency behavior.
- Events retain `world_id`, tick/order context, payload, actor, causal relationship, and random seed when applicable.
- Engine packages communicate through the stable event-bus interface rather than direct queue implementation details.
- Failure/retry behavior does not silently discard an event or create duplicate state changes.
- An integration test exercises publish → queue → worker handler against real Postgres.

## Delivery evidence

- Pending.
