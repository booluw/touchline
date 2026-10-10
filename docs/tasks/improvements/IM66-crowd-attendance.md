# IM66 — Crowd attendance per match

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** Fixtures screen (`Touchline Fixtures.dc.html`, ATT column), product-owner request 2026-10-10: model crowds so finances can use them (IM68).
**Depends on:** IM58 (match columns pattern), supporter groups (S06-02)

## What to do

1. `match.matches.attendance` (migration 0063), set when a match row is created: live kickoff and the direct `PlayFixture` path.
2. Stadium capacity: `club.clubs.stadium_capacity` was never filled. It is filled the first time a club hosts a match from its league's reputation: `3000 + 400 × rep`, ±30% per-club jitter taken from the club id (stable).
3. Fill rate = 0.55 + sentiment (±0.20) + loyalty (±0.10) + derby +0.15, six-pointer +0.05, dead rubber −0.10, cup tie −0.10, ±0.05 seeded noise; clamped to [0.10, 1]. Attendance = capacity × fill.

## Recorded decisions

- Capacity is filled at the first home match, not at bootstrap. Clubs join their league after bootstrap, so the league reputation isn't known there yet (product owner chose "derive at bootstrap"; this keeps its intent: derived, never hand-entered). Club reputation is not used: it is 10 for every club.
- Attendance is decided at kickoff from pre-match supporter state and the fixture context, and is deterministic (fixture seed).
- NULL for matches played before 0063; never backfilled.
- All numbers are proposal constants in `internal/match/crowd.go` (data-only recalibration).
- Ticket revenue is IM68, not here.

## Delivery evidence

- Files: `backend/migrations/0063_match_attendance.{up,down}.sql`, `migrations/README.md`, `internal/match/crowd.go`, `crowd_test.go`, `live.go` (kickoff insert), `persist.go` + `service.go` (PlayFixture), `service_integration_test.go`.
- `go test ./internal/match` (unit): capacity stable/banded, attendance capped at capacity, floored, derby and sentiment raise it, deterministic.
- Integration (local embedded Postgres 16, 2026-10-10): `./internal/match` all pass, incl. new assertion that a played match stores 0 < attendance ≤ capacity and fills capacity.
