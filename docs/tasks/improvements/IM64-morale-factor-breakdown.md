# IM64 — Signed morale factor breakdown ("Why?")

**Status:** Implemented (browser check pending)
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Design `Touchline Squad.dc.html` (Emotional state + "Why? Morale N / 100, net ±X"); product owner 2026-10-10 (OPD-65).
**Depends on:** IM63

## What to do

1. Report the signed terms of each player's morale target on `GET /api/clubs/:id/players/:playerID`.
2. Render them in the squad panel with `UiWhyBreakdown`.

## Recorded decisions (product owner, 2026-10-10)

- `explanation.why` is an Explanation of the **morale target** (what morale drifts toward after each match), on a 0–100 scale relative to neutral 50. Factors are the exact terms of `moraleTarget`: a playing-time band (meets role +15 / below role 0 / far below −15; development role +15), and only when far below, patience, loyalty, ambition and ego. A "limits and rounding" factor appears only when clamping or rounding leaves a remainder, so the deltas always sum to the score.
- One morale value stays. The design's Happiness / Confidence / Frustration / Trust split is not modelled. The design's "Club ambition" and "Friends in squad" factors are not engine inputs, so they are not shown.

## Delivery evidence

Backend: `internal/player/request_preview.go` (`moraleTargetExplanation`), `internal/player/morale.go`, `openapi.yaml`. Unit test `TestMoraleTargetExplanationSums` (sum = score and score = target, across personalities and bands). Frontend: `components/squad/PlayerPanel.vue` (WhyBreakdown "Morale drifting toward N", now M). Shared checks are listed in IM65.
