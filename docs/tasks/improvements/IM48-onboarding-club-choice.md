# IM48 — Onboarding: choose a club with match %

**Status:** Planned (no API change yet)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Landing Auth.dc.html (Choose club)`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** A13 signup, job offers

## What to do

No change now. `GET /api/managers/me/offers` already returns offer context (board expectations, squad summary, form, supporters, league).

## Open questions (design needs data the engine does not model)

- Match % with factors from stated play-style preferences: preferences are not collected.
- Club story/tags, rival manager handle: not stored.
- Register already takes `display_name` (manager name).

## Recorded decisions

Wait for a preference model before computing match %.
