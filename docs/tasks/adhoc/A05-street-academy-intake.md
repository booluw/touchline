# A05 — Country/street academy intake

**Status:** Not started
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session (country academy = street kids)
**Depends on:** A01, A02, A03

## What to do

Build the "country academy" intake: each season, 10–20 very young players (13–15) are discovered in the streets and enrolled as free agents in the country pool. Clubs can sign them but can't match them until 18.

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
