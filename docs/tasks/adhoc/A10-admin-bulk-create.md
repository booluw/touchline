# A10 — Admin bulk player creation

**Status:** Not started
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

- Pending.
