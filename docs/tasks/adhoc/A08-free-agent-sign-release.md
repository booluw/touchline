# A08 — Free-agent list, sign, and release APIs

**Status:** Not started
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session
**Depends on:** A01, A03, A07

## What to do

Build the HTTP API endpoints for human managers to browse, sign, and release free agents. No frontend yet — API only.

## Changes

### Endpoints

```
GET  /api/worlds/:worldID/countries/:countryID/free-agents
     ?position=GK&age_min=20&age_max=30&nationality=NG&page=1&limit=25

POST /api/clubs/:clubID/free-agent-signings
     Body: { player_id, weekly_wage, years }

POST /api/players/:playerID/release
     Body: { reason }   (admin or owning club only)
```

### internal/playerpool/sign.go

```go
// SignFreeAgent creates a professional contract, assigns the player to the club,
// and emits PLAYER_SIGNED. Returns error if player is not a free agent, if
// street-origin and age < 18, or if club cannot afford.
func SignFreeAgent(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
    playerID, clubID uuid.UUID, wage int64, years int) error

// ReleasePlayer terminates the player's contract, sets club_id=NULL,
// status='free_agent', and returns them to the country pool.
func ReleasePlayer(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
    playerID uuid.UUID, reason string) error
```

### Handlers

`internal/api/freeagent_handlers.go`:

- `ListFreeAgents` — paginated query of `player.players WHERE club_id IS NULL AND status = 'free_agent' AND country_id = $1` with optional filters. Joins person for age, player_attributes for overall.
- `SignFreeAgent` — validates club ownership (user's club or admin), calls `pool.SignFreeAgent`.
- `ReleasePlayer` — validates club ownership, calls `pool.ReleasePlayer`.

### Events

- `PLAYER_SIGNED` — `{player_id, club_id, weekly_wage, origin}`
- `PLAYER_RELEASED` — `{player_id, previous_club_id, reason, origin}`

### Finance

`SignFreeAgent` also inserts a `finance.ContractSeed` → `finance.AppendLedger` for the signing wage commitment (reuse S05-02 machinery). `ReleasePlayer` terminates the contract and adjusts ledger.

## Acceptance criteria

- `go test ./internal/playerpool/...` — sign, release, eligibility guards.
- API integration test: seed world → list free agents → sign one → verify club has the player → release → player back in pool.
- `go vet ./... && go build ./...` clean.

## Delivery evidence

- Pending.
