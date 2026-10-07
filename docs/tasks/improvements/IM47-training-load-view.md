# IM47 — Training: load, fatigue and injury risk view

**Status:** Planned (no API change yet)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html (training)`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S08 training

## What to do

No change now. Per-player fatigue, fitness, sharpness and injury_risk already ship in roster `dossier.condition`.

## Open questions (design needs data the engine does not model)

- Load by unit, individual focus, sports scientist, injury risk vs baseline factors: not modelled.

## Recorded decisions

Avoid duplicating condition data on the training endpoint.
