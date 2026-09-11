# Touchline

**The persistent multiplayer football universe.**

Every club has a personality. Every player has a story. Every decision has consequences.

Browser-based multiplayer football management simulation. Thousands of managers share a persistent, evolving football world — no requirement to be online simultaneously (async-first world clock).

- Product spec: [`docs/Touchline — Persistent Multiplayer Football Manager PRD.md`](docs/)
- Technical plan: [`docs/Touchline_Technical_Implementation_Plan.md`](docs/)

## Repo layout

```
/backend            Go backend (Gin HTTP API, river event bus, tick scheduler, worker pool)
  /cmd/api          API gateway binary
  /cmd/scheduler    Tick scheduler binary (WORLD_TICK cadences)
  /cmd/worker       Simulation worker pool binary
  /cmd/ref-seed     Reference data seeder (nationalities + name pools; idempotent)
  /internal         Engine packages: club, player, transfer, finance, match, social, world
  /pkg/eventbus     Event bus interface + river implementation
  /pkg/playergen    Procedural player name/nationality generation
  /pkg/auth         Custom JWT (httpOnly cookies)
  /data/names       Curated per-nationality name datasets
/frontend           Nuxt 3 (Vue 3 + Pinia) SPA/PWA
/infra
  /docker           Dockerfiles per deployable
  /helm             Helm charts: api, scheduler, worker, frontend
.github/workflows   CI (lint, test, build multi-arch, push to GHCR)
```

## Stack

Go + Gin · PostgreSQL (Neon) · Redis · river (event bus / job queue) · Nuxt 3 + Pinia · Tailwind · Docker Compose (dev) · k3s + Helm (prod) · GitHub Actions / Argo CD

## Getting started

Full walkthrough in [`docs/development.md`](docs/development.md).

1. Read the technical plan (`docs/Touchline_Technical_Implementation_Plan.md`) and the developer guide (`docs/development.md`).
2. `cp .env.example .env` (repo **root**; `.env` is git-ignored) and set a Neon `DATABASE_URL` — unpooled endpoint, `postgres://` scheme, no surrounding quotes.
3. `docker compose up --build`
4. Verify: `curl -s http://localhost:8080/health/db` → `{"status":"db-ok"}`, then open http://localhost:3000.

Postgres is **external** (Neon) — the base compose stack has no Postgres service. Want a hermetic DB? Use the override: `docker compose -f docker-compose.yml -f infra/ci/docker-compose.postgres.yml up`. Compose handles the bootstrap order automatically: migrations → ref-seed → api/scheduler/worker → frontend.

## Handoff notes for AI agents

See [`OPENCODE.md`](OPENCODE.md) — it is the working-notes handoff for an LLM agent joining this codebase.