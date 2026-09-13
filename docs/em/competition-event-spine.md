# P1 — Bring competition lifecycle events onto the event-spine contract

## Context

Competition seeding and rollover create consequential domain history: clubs are created/placed, seasons are created/completed, and promotions or relegations occur. The stated architecture requires significant state changes to be represented by `world.events` for downstream consumers and replay/audit.

## Evidence

- `backend/internal/competition/seeding.go` writes `SEASON_CREATED` and `COMPETITION_SEEDED` through `recordSeedEvent`, commits, and returns without calling `s.bus.Publish`.
- `backend/internal/competition/rollover.go` calls `recordSeedEvent` for `SEASON_COMPLETED`, club-movement, and next-season events while explicitly discarding errors (`_ = recordSeedEvent(...)`).
- `backend/internal/competition/clubnames.go`, `recordSeedEvent`, performs a plain `INSERT` without `RETURNING id, occurred_at`, so the in-memory event is not tied to the persisted event identity.
- In contrast, `backend/internal/bootstrap/service.go` explicitly documents and implements an event record followed by bus publication, while `pkg/eventbus.EventWorker` dispatches only events represented by River jobs.

## Failure mode

Competition events are available only as database rows and never reach current asynchronous subscribers. More severely, a failed event insert during rollover is silently ignored while the surrounding transaction may still commit the competition state. This can make the audit/replay/event history incomplete with no returned error, log, or repair marker.

As later news, social, finance, integrity, and notification systems consume the event spine, this creates an engine-specific blind spot and forces special-case repair work.

## Requested engineering decision

Make competition producers conform to the same durable event contract selected for the rest of the system. Event persistence failures within a domain transaction must have an explicit policy; for lifecycle/audit events, silent continuation should not be the default. The persisted event ID should be available to any subsequent dispatch or causal chain.

## Acceptance evidence to add

- Integration coverage proving seeding and rollover create the expected event rows and dispatch the same persisted IDs to an event-bus subscriber.
- A test proving a forced event-write failure prevents, or explicitly and observably compensates for, the associated competition state change.
- A replay/audit assertion covering `SEASON_COMPLETED`, promotion/relegation movement, and the next `SEASON_CREATED` event sequence.

