# S02-01 — Deliver authenticated API and session flow

**Status:** Not started  
**Sprint:** 02 — Authenticated, schedulable worlds  
**Source:** OPENCODE.md; technical plan §§2, 12, 16  
**Depends on:** S01-01, S01-05

## What to do

Wire the Go API as a Gin REST gateway and complete the approved custom JWT access/refresh cookie flow between Nuxt and Go. Keep game logic out of Nuxt server routes.

## Acceptance criteria

- Gin serves health and the documented `POST /api/auth/login` and `POST /api/auth/refresh` endpoints.
- Login and refresh use the existing custom JWT package and httpOnly cookie transport as specified.
- Protected routes reject unauthenticated requests; request validation and authorization occur server-side.
- The Nuxt client can establish and refresh an authenticated session against the Go API.
- The required product policies that are not specified (registration, recovery, verification) are recorded as open decisions rather than guessed.

## Delivery evidence

- Pending.
