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

1. Read the technical plan (`docs/Touchline_Technical_Implementation_Plan.md`) — Phase 0 scope is the only thing scaffolded so far.
2. Copy `backend/.env.example` → `.env` and set a Neon `DATABASE_URL`.
3. `docker compose up`

## Handoff notes for AI agents

See [`OPENCODE.md`](OPENCODE.md) — it is the working-notes handoff for an LLM agent joining this codebase.