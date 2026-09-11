# S01-02 — Implement the event log and River event round trip

**Status:** Done  
**Owner:** opencode agent  
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

- River v0.44.0 schema exported to golang-migrate files `backend/migrations/0016_river_migration` … `0022_river_notification_outbox` (up/down), regenerable per the note in `backend/migrations/README.md`.
- Migration gate covers the new files: verified on local Postgres 14.20 (`up`→22, `down -all`→0, re-up→22) and on the Neon dev DB (force-cleaned per owner approval; same up/down/re-up cycle → river tables `river_job`, `river_leader`, `river_migration`, `river_notification`, `river_queue` present). CI job `migrations` runs the cycle on postgres:16.
- `pkg/eventbus` rewritten: `eventbus.go` (schema-aligned `Event`, stable `EventBus` interface), `dispatcher.go` (handler registry), `worker.go` (`touchline_event` job, unique-by-args enqueue), `river.go` (RiverBus: `Publish` = atomic `world.events` INSERT `ON CONFLICT (id) DO NOTHING` + River `InsertTx`; `Subscribe`; `Start`/`Stop`).
- `cmd/worker/main.go` wired to the bus with a Phase-0 `WORLD_TICK` logging handler and graceful shutdown.
- Integration tests behind `//go:build integration`: `TestPublishConsumeRoundTrip`, `TestDuplicatePublishDoesNotDoubleEnqueue`, `TestRetryThenSuccessNoDuplicateStateChange` — all pass against real Postgres (`go test -tags integration ./pkg/eventbus/`).
- `go vet ./...`, `go build ./...`, and `go test ./...` all clean.
- Go toolchain bumped to 1.25 in CI (`ci.yml`) and Dockerfiles; new CI job `eventbus-integration` runs the tagged integration tests on postgres:16.
