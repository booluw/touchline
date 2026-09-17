# A06 — Aging and retirement at rollover

**Status:** Implemented
**Sprint:** Ad-hoc (player lifecycle)
**Source:** PRD §28; user design session
**Depends on:** A01, A03, A04, A05
**Implemented:** 2026-09-17

## What to do

Implement the aging-and-retirement lifecycle engine. Player age is derived from `date_of_birth` vs the world's reference date, so "aging" is implicit; retirement is the discrete action. Fire retirement + intake at the country rollover event.

## Changes

### internal/lifecycle/service.go (new package)

```go
type Service struct {
    pool   *pgxpool.Pool
    bus    eventbus.Publisher
    acad   *academy.Service
    poolSvc *playerpool.Service
}

func NewService(pool *pgxpool.Pool, bus eventbus.Publisher, acad *academy.Service, poolSvc *playerpool.Service) *Service

// OnSeasonCompleted is the rollover hook: age, retire, intake, replenish.
func (s *Service) OnSeasonCompleted(ctx context.Context, worldID, countryID uuid.UUID, seasonNumber int, ref time.Time) error
```

### Retirement model

For each active player in the world whose computed age ≥ 30:

```
retireProb = (age - 29) / 8          // 0 at 29, 12.5% at 30, 100% at 37+
retireProb *= abilityMod             // ability < 50: 1.5x; 50–70: 1.0x; >70: 0.6x
retireProb *= injuryMod             // injury_susceptibility > 40: 1.3x; else 1.0x
retireProb *= ambitionMod           // ambition < 30: 1.3x; > 70: 0.7x
retireProb = clamp(retireProb, 0, 1)
```

A player retires (status → `'retired'`, contract terminated) when `rand.Float64() < retireProb`.
Age 37+: force-retire (retireProb = 1.0).

### OnSeasonCompleted sequence (inside one tx)

1. **Club academy intake** — call `acad.IntakeForWorld(worldID, countryID, seasonNumber, ref)`.
2. **Street academy intake** — call `poolSvc.StreetIntake(worldID, countryID, seasonNumber, ...)`.
3. **Retirement** — evaluate all active players with computed age ≥ 30; set `status = 'retired'`, terminate contracts, emit `PLAYER_RETIRED`.
4. **Pool replenish** — call `poolSvc.ReplenishPool(worldID, countryID, target=100)`.
5. Emit `WORLD_LIFECYCLE_SEASON_COMPLETED` event: `{country_id, season_number, retired_count, academy_prospects, street_prospects, pool_size}`.

### World reference date

Season reference = the anchor of the newly-created next season (from `rollover.go` `lastScheduledDay` + 1). The lifecycle service receives this as `ref`.

### Worker wiring

**Architecture deviation:** the repo centralises bus wiring in `internal/app/app.go`
(worker binaries call `App.RunWorker`), not a hand-rolled `cmd/worker` subscription.
The `SEASON_COMPLETED` subscription and the seasonal (league-less) fallback in
`internal/app/app.go` now both dispatch to `lifecycle.OnSeasonCompleted`.

Dedup: the lifecycle may see multiple `SEASON_COMPLETED` events for the same country+season (one per league). Use `world.country_academy_intakes` dedup table for street intake; for retirement, a simple `SELECT EXISTS (SELECT 1 FROM world.events WHERE event_type = 'WORLD_LIFECYCLE_SEASON_COMPLETED' AND payload->>'country_id' = $1 AND payload->>'season_number' = $2)` guard.

### Events

- `PLAYER_RETIRED` — `{player_id, club_id, age, ability_avg, origin}`
- `WORLD_LIFECYCLE_SEASON_COMPLETED` — `{country_id, season_number, retired_count, academy_count, street_count, pool_size}`

## Acceptance criteria

- `go test ./internal/lifecycle/...` — retirement probability unit tests, age edge cases (29, 30, 37). — covered
- Integration test: seed world → play several seasons → verify players age, some retire, academy intake adds players, pool replenishes. — covered
- `go vet ./... && go build ./...` clean. — verified

## Delivery evidence

- `internal/lifecycle/` — new package: `retire.go` (`RetireProbability`: `(age-29)/8`
  curve clamp, `abilityMod` 1.5/1.0/0.6, `injuryMod` 1.3, `ambitionMod` 1.3/0.7,
  force-retire ≥ 37), `service.go` (`OnSeasonCompleted` = academy intake →
  retirement → pool replenish → summary event, idempotency guard on the
  lifecycle event row), `events.go` (`PLAYER_RETIRED`,
  `WORLD_LIFECYCLE_SEASON_COMPLETED`). `playerpool` is function-based in this
  repo (no `*playerpool.Service`), so the service holds `pool`, `bus`, and
  `*academy.Service` and builds replenish factories via `bootstrap.LoadPools`
  with a deterministic per-(world, country, season) seed.
- `internal/app/app.go` — `App.Lifecycle` added to `Build`; the existing
  `SEASON_COMPLETED` subscription and the seasonal fallback now call
  `lifecycle.OnSeasonCompleted` (intake is delegated to the academy service,
  which remains idempotent per season).
- `internal/lifecycle/retire_test.go` — `TestRetireProbabilityCurve` (age 29/30/
  33/36/37/40, ability/injury/ambition mods, clamping), monotone-with-age, and
  worst-case-bounded checks. `TestLifecycleOnSeasonCompleted` (integration):
  seeds a world, injects two 37-year-olds, runs rollover — verifies forced
  retirement + contract termination + event emission + replenish + redelivery
  no-op guard + fresh-season re-run.
- `go build ./... && go vet ./...` clean; `go test ./internal/... ./cmd/...`
  green.
