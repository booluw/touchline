# S08-03 — Implement injury simulation, medical staff quality, and rehabilitation

**Status:** Completed  
**Sprint:** 08 — Academies and player development  
**Source:** PRD §18; technical plan §16; OPENCODE.md  
**Depends on:** S04-02, S05-01

## What to do

Build the match and training injury engine. Simulate realistic injury events (type, severity, duration) during match simulation and high-intensity training. Factor in player natural fitness, fatigue accumulated over congestion periods, pitch condition, and medical staff quality to determine injury probability and recovery time.

## Acceptance criteria

- ✅ `player.players` persists active injury status, injury type (e.g. hamstring pull, ACL tear), expected return date, and recovery progress — `player.injuries` (type/severity/expected/actual dates/recurrence/match_id, migration 0006) is the durable side of the S08-03 engine; "open vs recovered" is `actual_recovery_date IS NULL` and `recovery_progress`/`days_remaining` are **derived at read time** from occurred/expected (never stored) in `internal/player/injury.go` `deriveProgress` (unit tests).
- ✅ Match simulation deterministically triggers injury events based on match seed, player fatigue levels, and physical tackle interactions — `pkg/matchsim` v1.6 already attributes WHO is injured (`EventInjury`); `internal/injury.Evaluate` resolves type/severity/duration/recurrence from a **separate** fnv64a stream (`MatchStream(matchSeed, playerID)`, so the frozen canonical digest is untouched) using fatigue, susceptibility, minutes and foul context. `match.injuryCandidates` + `injury.PersistMatch` run in `PlayFixture`/live `Finalize`'s tx; `TestPersistMatchRoundTrip` re-derives the expected outcome from the same context and asserts the row, risk bump and seeded `PLAYER_INJURED` match.
- ✅ Club medical staff quality level reduces recovery duration and lowers recurrence probability for previous injuries — `MedicalLevel` (1..10, neutral 5) folds a per-level recovery factor (floor 0.55) and an absolute recurrence reduction into `Evaluate`; `RecurrenceForPlayer` feeds the last completed injury's risk back as `RecurrenceLoad`, forcing the `recurring` type at ≥0.60. Facility level lives in `club.facilities('medical')` via `internal/academy/medical.go` (GET/PUT `/clubs/:id/medical-facility`, ensure-on-demand, cumulative cost, finance debit, `MEDICAL_FACILITY_UPGRADED`).
- ✅ Injured players are automatically blocked from selection in squad lineups; rushing players back early carries high recurrence risk — `tactics.setLineup` hard-rejects the whole lineup when any selected player is unavailable (`ErrPlayerUnavailable` → HTTP 422, nothing written; `TestHTTPLineupRejectsInjuredPlayer`); `injury.RushClose` (dedicated `POST .../rush-return`, `internal/player.RushReturn`) floors recurrence at `RushRecurrenceFloor` (0.65) and emits `PLAYER_RUSHED_RETURN`.
- ✅ Every injury event emits `PLAYER_INJURED` and `PLAYER_RECOVERED` events with full `Explanation` context — `PersistMatch`/`PersistTraining` emit `PLAYER_INJURED` (actor `system`, seed = match seed), the weekly `RecoverDue` set-back stage emits `INJURY_UPDATE`, the recovery stage emits `PLAYER_RECOVERED` + an `injury_return` history row + risk reset to susceptibility/200, and `RushClose` emits `PLAYER_RUSHED_RETURN` — all `world.events` rows carry an `explanation.Explanation`.

## Design decisions (implemented)

- **Determinism without touching the replay contract**: matchsim stays byte-identical; the engine draws only from `internal/injury/stream.go` fnv64a streams (`MatchStream`, `WeekStream`, `SetbackStream`). Numerics (proposal data): `backend/docs/design/injury-numerics.md` — recalibration is data-only.
- **Training injuries**: rolled per (week, player) in `training.applyClubWeekly` from `player_condition.injury_risk` and the plan's `planIntensity`; Recovery plans and players with an open injury are skipped; `injuredCount` joins the `TRAINING_WEEK` payload.
- **Recovery, uncertainty and setbacks**: recovery dates carry a ±15% seeded spread; while open and past 55% of the plan a weekly setback may add 3–7 days. `player.injury_setbacks` `(injury_id, week)` PK makes a redelivered tick a no-op. `RecoverDue` is world-scoped and self-transacting, called from `player.WeeklyTick`.
- **Medical facility**: no existing `club.facilities` writer, so the read is ensure-free (neutral 5) and the upgrade command materialises the row at the implied level on first use, ratchets one step, debits under a `(club, target-level)` dedup key and no-ops at level 10.
- **Migration 0046** adds `player.injury_setbacks` only — `player.injuries` already carried every column S08-03 needs and the selection eligibility gate.

## Delivery evidence

- `internal/injury` pure engine + unit tests (`go test ./internal/injury/`): deterministic type/severity/duration/recurrence, training-chance monotonicity, setback bounds, rushed-recurrence floor, recalibration-as-data.
- `internal/injury` DB suite (`store_integration_test.go`): `PersistMatch` round-trip vs re-derived `Evaluate` + `PLAYER_INJURED` seed + open-injury redelivery no-op; `PersistTraining` round-trip; `RecoverDue` closes a due injury (event + `injury_return` history + susceptibility/200 risk reset); setback idempotency under a redelivered tick; `RushClose` recurrence floor + `PLAYER_RUSHED_RETURN` + `ErrNoOpenInjury`; `MedicalLevel` neutral default vs materialised; `RecurrenceForPlayer`. `./internal/injury/...` added to the CI integration job (`.github/workflows/ci.yml`).
- `internal/academy` medical suite (`medical_integration_test.go`): neutral default 5 with **no** row written on read, billed 5→6 step under `medical:upgrade:<club>:6`, `MEDICAL_FACILITY_UPGRADED`, foreign-manager `ErrNotOwned`, cap no-op preserving `upgraded_at`.
- HTTP suite `cmd/api/injury_medical_integration_test.go`: fit player reads `null`; open injury reads type + derived progress; rush-return floors recurrence to 0.65 with `actual_recovery_date` and a `PLAYER_RUSHED_RETURN` row; second rush 404; foreign player 403; medical facility 5 (no row) → PUT 6 → GET 6 → PUT 7 with a £1.0M ledger debit, `MEDICAL_FACILITY_UPGRADED`, foreign 403; injured-player lineup → 422 with the rejected lineup not persisted.
- `internal/player` `deriveProgress` unit tests (clamping + zero-window safety); `GET /api/clubs/:id/players/:playerID/injury` and `POST .../rush-return` land in `internal/apidocs/openapi.yaml` (`PlayerInjury`/`MedicalFacility` schemas) keeping `TestDocsCoverRouter` green.
- Verification: `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./cmd/api/...`, `go test -race ./...` and `gofmt -l` green; DB suites run in CI (`-p 1 -tags integration`, no local Docker). Docs: `backend/docs/design/injury-numerics.md`; OPENCODE.md repo-layout + status note.
