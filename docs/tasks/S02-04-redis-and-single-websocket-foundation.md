# S02-04 — Add Redis-backed realtime foundation

**Status:** Not started  
**Sprint:** 02 — Authenticated, schedulable worlds  
**Source:** OPENCODE.md; technical plan §§2, 5, 11–12  
**Depends on:** S02-01, S01-05

## What to do

Wire Redis for the approved ephemeral concerns and implement authenticated `/ws` as the only client socket. Build the server/client multiplexing seam before feature-specific pushes exist.

## Acceptance criteria

- Redis is used for the approved ephemeral/session, rate-limit, or pub/sub concerns without becoming authoritative gameplay storage.
- `/ws` authenticates and supports typed multiplexed events.
- Nuxt `useSocket()` owns one session connection and fans messages to Pinia consumers; screens do not create individual sockets.
- The design supports Redis pub/sub fan-out across API pods.
- Connection, authentication failure, reconnection, and unknown event handling are tested or documented for verification.

## Delivery evidence

- Pending.
