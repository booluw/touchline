# P0 — Make event recording and dispatch recovery atomic

## Context

`OPENCODE.md` and the technical plan designate `world.events` as the persistent event spine. `pkg/eventbus` documents an at-least-once model and presents `RiverBus.Publish` as transactional: it inserts the event row and River job in one transaction.

## Evidence

Multiple producers first commit business state and a `world.events` row, then invoke `Publish` in a new transaction:

- `backend/internal/scheduler/service.go`, `FireTick`: commits the incremented `world.worlds.current_tick` and `WORLD_TICK` row before `s.bus.Publish`.
- `backend/internal/bootstrap/service.go`, `BootstrapWorld`: commits the world/club/squad/event transaction before publishing `WORLD_BOOTSTRAPPED`.
- `backend/internal/match/service.go`, `PlayFixture`; and `backend/internal/match/live.go`, kickoff/finalize paths: commit match state and event rows before publishing.
- `backend/internal/world/service.go`, `writeEvent`: inserts an event, then publishes separately.
- `backend/internal/manager/service.go`, `publish`: intentionally discards publish errors.

`RiverBus.Publish` uses `INSERT ... ON CONFLICT (id) DO NOTHING`, then inserts the River job. That makes a retry with the same event ID safe, but it does not repair the interval after the producer commits and before it successfully calls `Publish`.

## Failure mode

If the process crashes, loses database connectivity, or returns an error in that interval, the state transition and event log persist without a River job. No code found in the reviewed paths scans `world.events` for undispatched rows and re-enqueues them. In the manager path the failure is ignored, so the caller can receive success while downstream processing is silently absent.

Examples of downstream effects include a missing daily match kickoff, notification/news/read-model gaps, and no realtime tick for a committed `WORLD_TICK`. The record remains auditable but is not reliably actionable, which conflicts with the documented event-spine contract.

## Requested engineering decision

Define one durable publication contract for every event producer. It must ensure that an event committed with business state is eventually dispatched after any crash or transient publish failure, and it must preserve existing idempotency semantics. The owner should also define how to observe, retry, and repair undispatched events.

## Acceptance evidence to add

- A fault-injection test that commits a representative state change while enqueueing fails, then proves recovery dispatches exactly the original event ID.
- Coverage for scheduler, lifecycle/assignment, bootstrap, match completion, and competition events, or a shared producer abstraction that demonstrably covers them.
- An operational signal for an event that is persisted but not yet queued/delivered.

