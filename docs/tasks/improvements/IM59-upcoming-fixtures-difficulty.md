# IM59 — Next 5 fixtures with difficulty

**Status:** Planned
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Competitions screen follow-up, product-owner request 2026-10-09. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** IM55 (form query), IM39/scout (opponent rating + form)

## What to do

1. Extend `GET /api/clubs/:id/fixtures` with optional query params `upcoming=true` (scheduled only, kickoff ascending) and `limit` (1–30, default 30, the current value). With no params, the response is unchanged.
2. When `upcoming=true`, each fixture gains `difficulty` relative to the club in the path:
   `{ level: 1..5, label: "Very easy"|"Easy"|"Even"|"Hard"|"Very hard", factors: [{label, delta}] }`
   - **Strength gap**: opponent team rating − ours (the same rating the scout report already reads, `CurrentRating`).
   - **Venue**: home / away / neutral (cup finals).
   - **Opponent form**: their last 5 results (IM55 query).
   - Score = sum of factor deltas, bucketed into 5 levels; the thresholds are a tuning table, set from the simulated distribution so "Even" is the middle bucket.
3. All competitions (league + cups), so cup ties against clubs from other leagues work: the comparison uses ratings, not league positions.
4. The competitions screen calls `/clubs/{own}/fixtures?upcoming=true&limit=5`; the dashboard can reuse it.
5. Mirror in `openapi.yaml` (query params + Fixture.difficulty).

## Recorded decisions

- Rating-based, not position-based, so it compares across divisions (product decision pending confirmation).
- Difficulty is computed on read, not stored, so it moves with injuries and form.
- Factors use the same `{label, delta}` shape as the "Why" cards.

## Tests

Unit: bucket edges; home vs away on an equal-rated opponent differ by the venue delta. Integration: `upcoming=true&limit=5` returns ≤5 scheduled fixtures in kickoff order across league and cup; the default call (no params) is unchanged.
