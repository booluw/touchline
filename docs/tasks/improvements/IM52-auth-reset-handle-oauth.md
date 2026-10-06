# IM52 — Auth: password reset, handle availability, OAuth

**Status:** Planned (deferred: do not implement until asked)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Landing Auth.dc.html`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** —

## What to do

New: `POST /api/auth/password-reset`, `POST /api/auth/password-reset/confirm`, `GET /api/auth/handle-available`, `/api/auth/oauth/:provider` (Google, Discord). Needs email delivery and OAuth apps.

## Open questions (design needs data the engine does not model)

Full design to be scoped when picked up.

## Recorded decisions

Deferred by the product owner on 2026-10-06.
