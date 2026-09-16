# A05 — Country/street academy intake

**Status:** Implemented  
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session (country academy = street kids)
**Depends on:** A01, A02, A03

## What to do

Build the "country academy" intake: each season, 10–20 very young players (13–15) are discovered in the streets and enrolled as free agents in the country pool. Clubs can sign them but can't match them until 18.

## Delivery evidence

- **`internal/playerpool/street_intake.go`** — deterministic country street intake into the country free-agent pool: cohort `[StreetMinProspectsPerCountry, StreetMaxProspectsPerCountry]`, ages 13–15, `origin = 'street'`, `status = 'free_agent'`, dedup on `(world_id, country_id, season_number)` with idempotent country intake rows; gets personas + talents via `playergen` (street talent pools). Wired through `internal/academy/service.go` `IntakeForCountry`, which is driven from the seasonal `SEASON_COMPLETED` (country_id-aware) and the league-less-world fallback in `internal/app`.
- **`internal/academy`** — `IntakeForCountry`/`IntakeForWorld`/`IntakeForClub` intake machinery, `EnsureAcademies`, tier/investment models — tests green.
- **`pkg/playergen` + `internal/playerpool.PersistGeneratedPlayer`** — street 13–15s get canonical OVR via `PositionalOverall` in the free-agent read model (consistency with S08-01).
- **Canonical read surface**: `internal/squad/overall.go` + `internal/squad/overall_test.go` establish the single OVR/delta/headline-key model used by both street intake and club academies.
- **HTTP surface**: academy GET/PUT documented in `internal/apidocs/openapi.yaml` + coverage test green; street intake has no API route (rollover-triggered only, as specified).
- Idempotency test: re-running intake for the same `(world, country, season)` yields no duplicate prospects (dedup key + intake-row upsert).

## Changes

### internal/playerpool/street_intake.go

```go
// StreetIntakeConfig holds the per-country seasonal configuration.
type StreetIntakeConfig struct {
    MinCount int // default 10
    MaxCount int // default 20
    MinAge   int // 13
    MaxAge   int // 15
}

// StreetIntake generates street-origin free agents into the country pool.
func StreetIntake(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
    worldID, countryID uuid.UUID, seasonNumber int,
    factory *playergen.PlayerFactory, ref time.Time, cfg StreetIntakeConfig,
) ([]uuid.UUID, error)
```

- Generates random count in `[cfg.MinCount, cfg.MaxCount]`
- Ages in `[cfg.MinAge, cfg.MaxAge]`
- `origin = 'street'`, `club_id = NULL`, `status = 'free_agent'`
- Each gets a person + player + attributes + traits (no contract yet — signed on demand)
- Dedup: checks `world.country_academy_intakes` for `(worldID, countryID, seasonNumber)` — returns early if already exists
- Records intake in `world.country_academy_intakes`
- Emits `COUNTRY_ACADEMY_INTAKE` event: `{country_id, season_number, player_count, player_ids[]}`

### Default config

`StreetIntakeConfig{MinCount: 10, MaxCount: 20, MinAge: 13, MaxAge: 15}` stored as a `const` or world_config key.

### API routes

No API route — triggered automatically at rollover.

## Acceptance criteria

- `go test ./internal/playerpool/...` — StreetIntake determinism, age range, dedup, pool integration.
- Integration test: seed country → street intake → free agents have `origin = 'street'`, ages 13–15, `country_id` set.
- Idempotency: running twice for same (country, season) produces no duplicates.
- `go vet ./... && go build ./...` clean.

## Delivery evidence

- Pending.
