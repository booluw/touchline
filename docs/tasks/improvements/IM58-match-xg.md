# IM58 — Match engine: expected goals (xG)

**Status:** Planned
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** —

## What to do

1. `pkg/matchsim`: for every chance, add its goal probability to the attacking side's xG. In `resolveChance` that probability is already exact: `goalW / total`. Penalties add `penaltyConversionRate`. Result gains `HomeXG`, `AwayXG` (float, 2 dp).
2. **No extra RNG draws**: xG is computed from values already present, so seed + inputs replay gives the same results and same xG. Golden/replay tests must stay green unchanged.
3. Migration `0061_match_xg`: `home_xg`, `away_xg NUMERIC(4,2)` nullable on the match result row (null = played before IM58); add to `migrations/README.md` matrix.
4. Expose on the fixture/match result response (`GET /fixtures/:id`, IM45 match stats) so the manager sees their xG at full time.
5. Mirror in `openapi.yaml`; Touchline Book match chapter.

## Recorded decisions

- Needed now for IM57's chance-quality factor and later for the post-match screen (product owner, 2026-10-09).
- Pre-IM58 matches stay null; no backfill (cannot be recomputed without re-sim).

## Tests

Unit: same seed → identical goals and xG; xG ≥ 0; over many seeded sims, mean xG ≈ mean goals (within tolerance) — the calibration check.
