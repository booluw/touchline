# S08-02 — Implement dynamic potential growth and age-curve player development

**Status:** In progress — engine + weekly integration implemented; final verification pending  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §10; technical plan §16; OPENCODE.md  
**Depends on:** S04-02 (asserts S08-02 in the task list), S08-01

## What to do

Build the dynamic player potential and progression engine. Rather than static fixed potential ceilings, calculate player growth dynamically based on competitive playing time, match performance ratings, facility quality, training intensity, and age curves. Handle physical and mental attribute decline for aging veteran players (30+ years old).

## Acceptance criteria

- `player.player_attributes` updates dynamically following periodic development ticks.
- Young players receiving regular first-team match minutes experience accelerated attribute growth and potential ceiling expansion.
- Lack of playing time or poor training discipline causes young player development to stagnate.
- Veteran players follow realistic physical attribute decay curves while preserving or increasing tactical/mental attributes.
- All attribute adjustments produce auditable event records accompanied by `Explanation` objects detailing development drivers (e.g., "+2 Stamina from high match minutes and quality training").

## Design decisions (implemented)

- **Single writer preserved**: the S08-01 training sweep (`internal/training`) remains the only `player_attributes` writer. The pure engine `internal/development` returns per-key **growth multipliers** the sweep folds into positive plan deltas (decay stays ×1.0), plus potential flex/lock decisions and an auditable explanation. Numerics (proposal data): `backend/docs/design/development-numerics.md`.
- **Match-feeding the engine**: `pkg/matchsim` v1.6 emits deterministic per-player 1–10 match ratings (+ goals/assists) with a strict attribution pass; the weekly pass reads each player's last 10 rated appearances for the elite-form signal.
- **Hidden potential stays hidden**: `player_hidden_traits.potential` is read only by the development pass, never surfaced through squad read models. Flex expansion happens only for ≤26-year-old elite performers at their ceiling; the ceiling locks last-expansion or at 27 (`player_development.potential_locked_week`).
- **Auditability**: one `DEVELOPMENT_WEEK` system event per club per applied week carries that club's per-player `explanation.Explanation` array; numeric movement is the existing `player_attribute_changes` rows.
- **Migration 0045** adds `player_appearances.rating/goals/assists` (nullable) and `player.player_development` (last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks, potential_expansions_remaining, potential_locked_week). Delivered with the v1.6 match engine (S04-02/S08-02 co-landing).

## Delivery evidence

- `internal/development` pure engine + unit tests (`go test ./internal/development/`): age-curve ordering, minutes/stagnation, potential-ceiling trickle, flex expansion + budget/lock, age-lock, legacy-no-ceiling, deterministic replay, Overall blend, explanation auditability.
- Sweep integration compiles and unit-tests clean; deterministic weekly model test updated to fold development multipliers; integration-tagged suite compiles (`go vet -tags integration ./internal/training/`), full DB verification pending CI.
- Manager-facing read surface: `GET /api/clubs/:id/players/:playerID/development` (`internal/player.GetPlayerDevelopmentDetail` + `internal/httpapi`) returns the observable trajectory (last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks, `stagnating` per `development.StagnatingThreshold`, recent attribute deltas excluding the morale pseudo-key) and the stored `DEVELOPMENT_WEEK` drivers (PRD §54 — stored, never recomputed). The hidden potential ceiling / expansion budget / lock state are **never** exposed (design: managers see trajectory, not headroom). `cmd/api/player_development_integration_test.go` covers 200-with-state+drivers, never-evaluated zeros, 403 other-club, 400 invalid id, 401 anonymous, and asserts ceiling keys are absent from the body.
- Docs: `backend/docs/design/development-numerics.md`; OPENCODE.md repo-layout + status note.