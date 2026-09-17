# A10 — Admin bulk player creation

**Status:** Implemented
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session
**Depends on:** A02, A03

## What to do

Admins should be able to bulk-create players by providing high-level metrics. Players are added to a country pool as free agents.

## Changes

### Endpoint

```
POST /api/admin/worlds/:worldID/countries/:countryID/players/bulk
Body: {
    count: 50,            // 1–500
    age_min: 17,
    age_max: 25,
    quality: "mid",       // low (35-55) | mid (50-70) | high (65-85) | elite (80-95)
    positions: ["CM", "ST", "GK"],  // optional; empty = any
    nationality: "NG",    // optional; empty = weighted
    origin: "generated"   // default 'generated'; admin can set 'street'
}
```

### Quality band → QualityOffset mapping

| Quality | Range | Offset |
|---------|-------|--------|
| low     | 35–55 | -10    |
| mid     | 50–70 | 0      |
| high    | 65–85 | +15    |
| elite   | 80–95 | +25    |

### internal/playerpool/bulk.go

```go
// BulkCreate generates `count` players and adds them to the country pool.
func BulkCreate(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
    worldID, countryID uuid.UUID, opts BulkOpts, factory *playergen.PlayerFactory, ref time.Time,
) ([]uuid.UUID, error)
```

### Handlers

`internal/api/admin_handlers.go`:

- `BulkCreatePlayers` — validates admin role, parses body, calls `pool.BulkCreate`.
- `BulkOpts` struct with validation: `count` 1–500, valid positions, valid quality.

### Events

`ADMIN_BULK_PLAYER_CREATED` — `{country_id, count, quality, origin, player_ids[]}`.

## Acceptance criteria

- `go test ./internal/playerpool/...` — BulkCreate with various opts, age ranges, quality bands.
- API integration test: admin POST → verify N players in pool with correct attributes.
- Non-admin 403.
- `go vet ./... && go build ./...` clean.

## Delivery evidence

- Implemented 2026-09-17.
- `internal/playerpool/bulk.go`: `BulkOpts` (+ `Validate`: count 1–500, quality in low|mid|high|elite, age band 13..38, valid positions), `QualityOffset` label→offset map (low -10, mid 0, high +15, elite +25), `BulkCreate(ctx, tx, pub, worldID, countryID, opts, factory, ref)` generating via `playergen.CreatePlayerOptions` (age range, offset, nationality, positions, origin) and persisting as `free_agent` with `country_id` set; emits `ADMIN_BULK_PLAYER_CREATED {country_id, count, quality, origin, player_ids[]}`.
- `pkg/playergen`: `CreatePlayerOptions.Positions` subset + `pickPosition` (drops invalid positions, falls back to any).
- Handler `internal/httpapi/admin_bulk_handlers.go` `handleAdminBulkCreatePlayers` behind `requireAdmin` (deviates from task path `internal/api/admin_handlers.go` — handlers live in `internal/httpapi`). Route `POST /api/admin/worlds/:id/countries/:countryID/players/bulk` (uses `:id` not `:worldID` to stay consistent with the existing `/admin/worlds/:id` wildcards).
- OpenAPI: path + `BulkCreatePlayersInput`, `BulkCreateResult` schemas; `TestDocsCoverRouter` passes.
- Tests: `internal/playerpool/bulk_test.go` (QualityOffset, Validate incl. 403-relevant rejects), `internal/playerpool/bulk_integration_test.go` (`//go:build integration`, `TestBulkCreateIntegration` verifies 11 free agents in-country, ages 17–19, GK|ST only, generated origin, elite offset lifts quality, event row; `TestBulkCreateValidatesRejectsBadOpts`) compile-checked via `go vet -tags integration`; `pkg/playergen/factory_test.go` RestrictedPositions + pickPosition.
- `go build ./... && go vet ./...` (incl. `-tags integration`) clean.

## Acceptance criteria

- `go test ./internal/playerpool/...` — BulkCreate with various opts, age ranges, quality bands.
- API integration test: admin POST → verify N players in pool with correct attributes.
- Non-admin 403 (via `requireAdmin` middleware, shared with all existing admin routes).
- `go vet ./... && go build ./...` clean.
