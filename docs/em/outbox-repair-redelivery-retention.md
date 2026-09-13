# P0 — Do not treat a cleaned River job as an undispatched event

## Context

S04-04 added `internal/eventoutbox.Sweep` as a repair mechanism for an event row that exists without a River dispatch job. The contract is at-least-once delivery: repair must recover genuinely undispatched events, but it must not convert normal queue retention into perpetual redelivery.

## Evidence

- `backend/internal/eventoutbox/outbox.go`, `Sweep`, classifies an event as undispatched solely when there is no matching `river.river_job` row of kind `touchline_event`.
- `backend/cmd/worker/main.go` runs this sweep every 60 seconds by default.
- `backend/pkg/eventbus/river.go` creates the River client without setting a completed-job retention policy.
- River v0.44.0's `Config.CompletedJobRetentionPeriod` defaults to 24 hours; the River job cleaner permanently removes completed jobs after that interval. This is also visible in the locally resolved module documentation (`go doc github.com/riverqueue/river.Config`) and its default constants.
- Once a completed job is deleted, the sweep's left join finds no job row, calls `EnqueueRepair`, and River's unique-by-args check has no retained job to deduplicate against.

## Failure mode

After the retention period, every historical successfully delivered event eventually appears missing to the sweep and is enqueued again. As the event log grows, the worker repeatedly replays the oldest batches of history; the repair loop becomes a permanent replay generator, not a recovery path.

The event contract requires handlers to be idempotent, but current worker behavior demonstrates a non-idempotent observable side effect: each `WORLD_TICK` delivery publishes a realtime envelope. Replayed historical ticks can therefore be sent again to connected clients. Future notification, news, and social consumers would inherit the same risk unless every side effect has durable event-ID deduplication.

This also creates unbounded avoidable queue load and can starve genuine work as the history grows.

## Requested engineering decision

Define a durable delivery-state record independent of River's transient job-retention table, then make both normal dispatch and repair consult it. Alternatively, explicitly retain the required job history forever and document the operational storage/cleanup policy. The chosen design must distinguish:

- never enqueued;
- queued or in progress;
- successfully delivered; and
- terminally failed/discarded.

Do not use queue-row existence alone as proof that an event has never been delivered.

## Acceptance evidence to add

- A test that simulates removal of a completed River job and proves the repair sweep does not re-enqueue an already delivered event.
- A test that still re-enqueues a genuinely committed-but-never-enqueued event by its original ID.
- A long-retention operational test or policy assertion covering completed, discarded, and cancelled jobs.
- Metrics that separately report undelivered events, retried events, and terminal failures.

