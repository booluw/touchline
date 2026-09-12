# S02-04 — Add Redis-backed realtime foundation

**Status:** Done ✅
**Sprint:** 02 — Authenticated, schedulable worlds
**Source:** OPENCODE.md; technical plan §§2, 5, 11–12
**Depends on:** S02-01, S01-05

## What to do

Wire Redis for the approved ephemeral concerns and implement authenticated `/ws` as the only client socket. Build the server/client multiplexing seam before feature-specific pushes exist.

## Acceptance criteria

- Redis is used for the approved ephemeral/session, rate-limit, or pub/sub concerns without becoming authoritative gameplay storage.
  - **Resolved:** Redis is used for **pub/sub WebSocket fan-out only** (OPD-19). Sessions/rotation stay in Postgres `auth.sessions` (OPD-15(5)); rate limiting remains deferred to S11. Redis is an ephemeral transport — authoritative state and replay stay in `world.events`.
- `/ws` authenticates and supports typed multiplexed events.
  - `GET /ws` runs behind `requireAuth` (httpOnly `access_token` cookie handshake, Origin checked against `APP_ORIGIN`); events are a typed envelope `{type, payload, world_id?, ts}` (`pkg/realtime/event.go`); the world is derived from the manager row and delivery is world-scoped.
- Nuxt `useSocket()` owns one session connection and fans messages to Pinia consumers; screens do not create individual sockets.
  - `composables/useSocket.ts` is a module singleton with reconnect/backoff, typed `on(type, handler)` unsubscribe, and unknown-type ignore; `stores/realtime.ts` holds the lifecycle + latest `world_tick`/`error`; `pages/auth/login.vue` connects on success. No screen opens its own socket.
- The design supports Redis pub/sub fan-out across API pods.
  - All delivery flows through a `Broker` subscription (Redis channel `touchline:realtime`, events carry `world_id`), so 1..N pods behave identically; `RedisBroker` for multi-pod, `LocalBroker` in-process fallback when `REDIS_URL` is unset/unreachable (fail-soft).
- Connection, authentication failure, reconnection, and unknown event handling are tested or documented for verification.
  - Back-end tests (miniredis, no external deps): no-world handshake rejection, ping→pong, unknown-type→`error` envelope, world isolation, cross-hub Redis fan-out. `cmd/api` integration tests: `/ws` rejects missing cookie (401), authenticated client receives a published event world-scoped. Client reconnect/unknown behavior documented in `docs/development.md` (§Realtime) per AC5; typecheck/lint green.

## Delivery evidence

- `pkg/realtime/` (unit, `go test -race` ✓): `TestNewEventRoundTrip`, `TestHandleWSRejectsMissingWorld`, `TestClientPingAndUnknownType`, `TestLocalBrokerFanOutAndWorldIsolation`, `TestRedisBrokerFansOutAcrossHubs`.
- `cmd/api` integration (`TestWS*`, ✓ against local Postgres): 401 handshake, authenticated event receipt, cross-world isolation.
- Worker→browser proof event: `cmd/worker` publishes `world_tick {event_id, granularity, tick}` on every `WORLD_TICK`.
- Docs: OPD-19 in `docs/product_manager.md`; §Realtime in `docs/development.md`; `OPENCODE.md` layout/backlog/status updated.
- CI: integration job extended with `./pkg/realtime/...` (unit runs in the normal test job).