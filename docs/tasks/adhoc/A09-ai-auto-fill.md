# A09 — AI auto-fill for thin squads

**Status:** Implemented
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session
**Depends on:** A03, A08

## What to do

AI-controlled clubs should automatically sign free agents when their squad drops below a target size. Triggered at season rollover (inside the lifecycle hook).

## Changes

### internal/lifecycle/autofill.go

```go
const TargetSquadSize = 24

// AutoFill checks every AI club in the country; if squad < TargetSquadSize,
// sign best-available free agents until squad is at target. Fills position
// gaps first (using the squad template), then fills remaining by overall rating.
func (s *Service) AutoFill(ctx context.Context, tx pgx.Tx, worldID, countryID uuid.UUID) (int, error)
```

For each AI club:
1. Count active players in `player.players WHERE club_id = $1 AND status = 'active'`.
2. If count < TargetSquadSize, compute the squad template gaps (which positions are under-represented).
3. Query free agents in the country pool ordered by position match + overall rating descending.
4. Sign each (calling `pool.SignFreeAgent`) until squad is at target or pool exhausted.
5. Use a deterministic RNG seeded from (worldID, seasonNumber, clubID) so the same state produces the same signings.

### Finance

Each auto-fill signing creates a professional contract with a wage derived from the player's overall rating:
`weekly_wage = overallRating * 150` (range ~6,000–15,000).

### Event

`AI_AUTO_FILL` — `{club_id, signed_count, player_ids[]}`

### Worker wiring

Called from within `lifecycle.OnSeasonCompleted` after intake and retirement.

## Acceptance criteria

- `go test ./internal/lifecycle/...` — AutoFill signs correct number, respects position gaps, doesn't overfill.
- Integration test: retire several players → AutoFill → squad back at target.
- `go vet ./... && go build ./...` clean.

## Delivery evidence

- Implemented 2026-09-17.
- `internal/lifecycle/autofill.go`: `TargetSquadSize = 24`, `AutoFill(ctx, tx, worldID, countryID uuid.UUID, season int, ref time.Time) (int, error)`; per-club gap-fill via `orderAutoFillCandidates` (quota gaps GK=2/DEF=7/MID=7/FWD=6, deepest first with fixed group precedence, then overall top-up); deterministic tie-break via RNG seeded `hashSeed(worldID, clubID, season)`; wage = `overall * 150`, 1-year contract; skips `ErrStreetUnder18` / `ErrClubCannotAfford`; emits one `AI_AUTO_FILL` per club.
- Wired into `Service.OnSeasonCompleted` after intake + retirement + replenish (runs in the rollover tx); `Result.AutoFilled` reports the count.
- Tests: `internal/lifecycle/autofill_test.go` (gap-first, gap-depth ordering, no overfill, pool exhaustion, deterministic under seed, group mapping) — `go test ./internal/lifecycle/` green; `internal/lifecycle/autofill_integration_test.go` (`//go:build integration`, `TestAutoFillRestoresThinSquad` — guts a club to 0 active, AutoFill restores ≥24, wage == overall*150, one AI_AUTO_FILL event, re-run no-op) compile-checked via `go vet -tags integration`.
- `go vet ./... && go build ./...` clean.
- Deviations from sketch: `AutoFill` additionally takes `season int` and `ref time.Time` (needed for the deterministic seed and contract dates); signature order is `(ctx, tx, worldID, countryID, season, ref)`.
