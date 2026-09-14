# A09 — AI auto-fill for thin squads

**Status:** Not started
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

- Pending.
