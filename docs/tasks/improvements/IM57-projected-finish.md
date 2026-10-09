# IM57 — Projected finish + "Why" factors

**Status:** Planned
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM56 (same endpoint), IM58 (xG chance-quality factor)

## What to do

Add `projection` to the IM56 `outlook` response.

1. **Projected finish**: Monte Carlo, 1000 runs of the remaining league fixtures. Each result drawn from the two clubs' home/away points-per-game rates. Return `position` (median) and `range` [p10, p90].
2. **Deterministic**: seed = season id + matchday, so the same day always gives the same answer. Cache per (league, matchday).
3. **Factors** (`[{label, delta, detail}]`, delta in points vs a neutral baseline, same shape as finance/board factors):
   - Remaining fixtures vs current top 6 (fixture list + table).
   - Home form (W/D/L at home, season).
   - Injuries to key players (player injury tables, weighted by rating, expected return vs games left).
   - Goal-difference trend: last 5 vs season average.
   - Chance-quality trend: xG for − xG against, last 5 vs season (needs IM58; omitted until it has data).
4. Mirror in `openapi.yaml`.

## Recorded decisions

- Both GD trend and xG trend ship (product owner, 2026-10-09).
- Simulation lives in the read path; no new tables. If profiling shows cost, persist per matchday instead.

## Tests

Unit: same seed → identical output; a finished season → range collapses to the actual position; a stronger remaining schedule → negative fixtures factor.
