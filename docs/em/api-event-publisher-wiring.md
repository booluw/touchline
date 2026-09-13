# P1 — Wire the API's state-changing services to the transactional publisher

## Context

OPD-23 now states that every event producer writes its state, `world.events` record, and River dispatch job in one transaction. The shared `eventbus.WriteTx` helper intentionally permits a nil publisher for log-only usage, which is useful in narrowly defined offline/test contexts but changes production behavior when used by the API binary.

## Evidence

- `backend/cmd/api/main.go` creates `worldSvc`, `mgrSvc`, `bootSvc`, `compSvc`, and `matchSvc` with `nil` publishers.
- The corresponding API routes perform state changes: world lifecycle/config/bootstrap, job offers and assignment actions, and competition administration/seeding.
- `backend/pkg/eventbus/eventbus.go`, `WriteTx`, calls `RecordTx` only when its publisher is nil. It therefore commits the event log row without creating a `touchline_event` River job.
- `docs/tasks/S04-04-review-findings-fixes.md` and `OPENCODE.md` explicitly acknowledge this as an open follow-up, so this is a remaining implementation gap rather than a disputed interpretation.

## Failure mode

Admin or manager actions accepted by the API become log-only events. They do not reach worker subscribers, realtime fan-out, notifications, news/read models, or any future event-driven engine. The repair sweep will later see those rows as missing jobs and enqueue them, but only after its polling interval and—under the current retention-based detection—also risks the broader replay problem documented separately.

This makes delivery behavior depend on which executable initiated the state change, contrary to the cross-engine event contract. It also weakens failure semantics: API requests succeed when no dispatch was attempted at all.

## Requested engineering decision

In production, construct and inject a River publisher into every API service that can mutate authoritative state. Preserve a clearly named log-only mode only for tests, migrations, or explicitly offline maintenance commands, and ensure it cannot be selected accidentally in normal API startup.

## Acceptance evidence to add

- An API integration test that invokes a representative state-changing route and observes a worker subscriber receive the same persisted event ID.
- Startup/wiring coverage proving all production event-producing services receive a non-nil publisher.
- A documented, explicit policy for any intentional log-only producer, including how and when its events are dispatched.

