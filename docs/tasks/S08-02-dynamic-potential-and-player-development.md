# S08-02 — Implement dynamic potential growth and age-curve player development

**Status:** Completed  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §10; technical plan §16; OPENCODE.md  
**Depends on:** S04-02 (asserts S08-02 in the task list), S08-01

## What to do

Build the dynamic player potential and progression engine. Rather than static fixed potential ceilings, calculate player growth dynamically based on competitive playing time, match performance ratings, facility quality, training intensity, and age curves. Handle physical and mental attribute decline for aging veteran players (30+ years old).

## Acceptance criteria

- ✅ `player.player_attributes` updates dynamically following periodic development ticks — the S08-01 weekly sweep folds dev multipliers into growth and persists `player.player_development` state (`training_service.go` applyClubWeekly; `internal/development`.
- ✅ Young players receiving regular first-team match minutes experience accelerated attribute growth and potential ceiling expansion — dev-engine unit tests (age-curve ordering, flex expansion) + `TestDevelopmentWeeklyFlexExpansion` (19-year-old elite wonderkid 85→87 potential, budget 3→2).
- ✅ Lack of playing time or poor training discipline causes young player development to stagnate — stagnation counter + ×0.85 gate at `StagnatingThreshold`; `TestDevelopmentWeeklyStagnationWritesState` (counter 1→2→3 over three bench weeks).
- ✅ Veteran players follow realistic physical attribute decay curves while preserving or increasing tactical/mental attributes — age-curve factors (30+: physical ×0.55, mental ×1.15).
- ✅ All attribute adjustments produce auditable event records accompanied by `Explanation` objects detailing development drivers — per-club `DEVELOPMENT_WEEK` event with per-player explanations (subject `player_development`), surfaced via the read endpoint's `drivers`.

## Design decisions (implemented)

- **Single writer preserved**: the S08-01 training sweep (`internal/training`) remains the only `player_attributes` writer. The pure engine `internal/development` returns per-key **growth multipliers** the sweep folds into positive plan deltas (decay stays ×1.0), plus potential flex/lock decisions and an auditable explanation. Numerics (proposal data): `backend/docs/design/development-numerics.md`.
- **Match-feeding the engine**: `pkg/matchsim` v1.6 emits deterministic per-player 1–10 match ratings (+ goals/assists) with a strict attribution pass; the weekly pass reads each player's last 10 rated appearances for the elite-form signal.
- **Hidden potential stays hidden**: `player_hidden_traits.potential` is read only by the development pass, never surfaced through squad read models. Flex expansion happens only for ≤26-year-old elite performers at their ceiling; the ceiling locks last-expansion or at 27 (`player_development.potential_locked_week`).
- **Auditability**: one `DEVELOPMENT_WEEK` system event per club per applied week carries that club's per-player `explanation.Explanation` array; numeric movement is the existing `player_attribute_changes` rows.
- **Migration 0045** adds `player_appearances.rating/goals/assists` (nullable) and `player.player_development` (last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks, potential_expansions_remaining, potential_locked_week). Delivered with the v1.6 match engine (S04-02/S08-02 co-landing).

## Delivery evidence

- `internal/development` pure engine + unit tests (`go test ./internal/development/`): age-curve ordering, minutes/stagnation, potential-ceiling trickle, flex expansion + budget/lock, age-lock, legacy-no-ceiling, deterministic replay, Overall blend, explanation auditability.
- Sweep integration compiles and unit-tests clean; the deterministic weekly model test (TestApplyWeeklyMatchesDeterministicModel) is updated to fold the development multipliers into expected values; the DB dev suite (`internal/training/dev_integration_test.go`) and the dev HTTP suite (`cmd/api/player_development_integration_test.go`) run in CI (`-p 1 -tags integration`; `./internal/training/...` now in the integration job).
- Manager-facing read surface: `GET /api/clubs/:id/players/:playerID/development` (`internal/player.GetPlayerDevelopmentDetail` + `internal/httpapi` + OpenAPI `internal/apidocs/openapi.yaml`, cross-checked by the router/OpenAPI coverage test) returns the observable trajectory (last_eval_week, cum_dev_weeks, consecutive_stagnant_weeks, `stagnating` per `development.StagnatingThreshold`, recent attribute deltas excluding the morale pseudo-key) and the stored `DEVELOPMENT_WEEK` drivers (PRD §54 — stored, never recomputed). The hidden potential ceiling / expansion budget / lock state are **never** exposed (design: managers see trajectory, not headroom). `cmd/api/player_development_integration_test.go` covers 200-with-state+drivers, never-evaluated zeros, 403 other-club, 400 invalid id, 401 anonymous, and asserts ceiling keys are absent from the body.
- Verification: `go build ./...` + `go vet ./...` + `go vet -tags integration ./internal/{training,player}/... ./cmd/api/...` clean; full unit suite (`go test ./pkg/... ./internal/... ./cmd/...`) green incl. the dev-engine tests and the training deterministic-model test folded with development multipliers. The DB-level integration suites (`internal/training` dev tests + `cmd/api` dev HTTP test) use the shared `testdb` harness and run in CI — `./internal/training/...` added to the CI integration job (`.github/workflows/ci.yml`) so the S08-02 dev suite executes; run locally only with Docker or `TEST_DATABASE_URL`.
- Docs: `backend/docs/design/development-numerics.md`; OPENCODE.md repo-layout + status note.