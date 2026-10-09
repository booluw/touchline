# IM60 — Standings window around the manager's club

**Status:** Planned
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Competitions screen follow-up, product-owner request 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM55 (rows carry form)

## What to do

1. New `GET /api/me/competitions/:id/standings`: the same `StandingRowSet` shape as `/competitions/:id/standings`, with `rows` cut to the 3 positions above and 3 below the manager's club.
2. `StandingRow` gains `position` (also on the full table, so a cut-out slice can still show the right numbers). This is additive and non-breaking.
3. **At the edges**, keep 7 rows by shifting the window: 1st place shows 1–7 and last place shows the bottom 7. A league with fewer than 7 clubs returns all of them.
4. 404 if the manager's club isn't in that competition's active season; 400 for a cup id.
5. Mirror in `openapi.yaml`.

## Recorded decisions

- Slice of the same computed table (one query, then a slice in Go), so the positions always match the full table.
- Window size fixed at ±3 (no query param) until a second screen needs a different size.

## Tests

Unit (slice helper): middle, 1st, 2nd, last, and a league of 5 clubs. Integration: positions in the slice equal those in the full table.
