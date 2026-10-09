# IM55 — Standings: last-5 form per club

**Status:** Planned
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Screens.dc.html` competitions screen), product-owner review 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** —

## What to do

1. `GET /api/competitions/:id/standings`: each row gains `form: ["W","D","L",…]`, newest first, at most 5, from the active season's completed league fixtures.
2. One query (window function over completed fixtures, partitioned by club), not N+1. Reuse the IM39 form query if its shape fits.
3. Mirror in `openapi.yaml` (StandingRow.form).

## Recorded decisions

- Table is about clubs: **no manager handle / "You" marker** on rows (product owner, 2026-10-09). Only the form dots.
- GD is derived client-side from goals_for − goals_against; no new field.

## Tests

Integration: a club with >5 results returns exactly 5, newest first; a club with none returns `[]` (not null).
