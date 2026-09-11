# S01-05 — Make the local environment executable and documented

**Status:** Not started  
**Sprint:** 01 — World foundation and event spine  
**Source:** OPENCODE.md; technical plan §§2, 15, 18  
**Depends on:** S01-01

## What to do

Finish the Docker Compose and environment configuration contract for Redis, API, scheduler, worker, frontend, and Neon Postgres. Postgres remains external to Compose.

## Acceptance criteria

- A documented setup specifies all required environment variables, including Neon `DATABASE_URL`, without committing secrets.
- Docker Compose starts Redis, API, scheduler, worker, and frontend and connects them to the configured external Postgres.
- The initial migration and a health check can run in this environment.
- The developer guide clearly states that Postgres is Neon/external, not a Compose service.
- Failed/missing configuration produces actionable startup errors.

## Delivery evidence

- Pending.
