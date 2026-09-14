# A03 — Player pool and squad draft system

**Status:** Not started
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session; OPENCODE.md
**Depends on:** A01, A02

## What to do

Build the `internal/playerpool` package and rewire `bootstrap.GenerateAIClub` + `competition.SeedCompetition` so every club (including the starter) drafts its squad from a shared free-agent pool rather than minting fresh players.

## Schema assumptions

`player.players.country_id` and `player.players.origin` exist (A01).

## New package: internal/playerpool

### SeedPool(ctx, tx, pub, worldID, countryID *uuid.UUID, size int, factory *playergen.PlayerFactory, ref time.Time) ([]uuid.UUID, error)

Generate `size` free agents (age-diverse 17–33) and persist them into `player.players` with:
- `club_id = NULL`, `status = 'free_agent'`
- `country_id` set if non-nil (world-level pool at bootstrap uses NULL)
- `origin = 'generated'`, `is_academy_product = false`
- Person + player + attributes + traits + personality persisted in one tx
- Returns the new player IDs

Age distribution heuristic: roughly 15% 17–20, 50% 21–28, 25% 29–31, 10% 32–33.

### DraftSquad(ctx, tx, pub, worldID clubID, poolCountryID *uuid.UUID, factory *playergen.PlayerFactory, ref time.Time, size int) (*DraftResult, error)

Pick `size` players from the pool via the squad template (`bootstrap.squadTemplate`), assign them to `clubID`:
- `SET club_id = $1, status = 'active'` for selected players
- Create a professional contract per player (`finance.ContractSeed` analogue or direct insert)
- Emit `PLAYER_CLAIMED_FROM_POOL` event per player

Returns clubID, drafted player count, remaining pool count.

### ReplenishPool(ctx, tx, worldID, countryID, target int, factory) error

If current pool size < target, generate additional players to reach it. Idempotent (no-op if already at target).

### ListFreeAgents(ctx, worldID, countryID, filters) ([]FreeAgent, error)

Return paginated free agents with position, age, nationality, and overall rating.

### Contract creation helper

`signToClub(ctx, tx, pub, playerID, clubID, wage, start, end)` — sets `club_id`, `status='active'`, inserts `player.contracts` row, emits `PLAYER_SIGNED` event.

## Changes to existing packages

### bootstrap/service.go

`BootstrapWorld` → no longer calls `generateSquad(factory, 24)`. Instead:
1. Call `playerpool.SeedPool(worldID, NULL, size=100, factory, ref)` to seed the world-level pool.
2. Call `playerpool.DraftSquad(starterClubID, NULL, factory, ref, 24)` to draft the starter squad.

`GenerateAIClub` → changed signature: now takes `poolCountryID uuid.UUID` and calls `playerpool.DraftSquad` internally (does NOT generate players). No change to its public result struct.

### competition/seeding.go

`SeedCompetition` → before the league loop:
1. Call `playerpool.SeedPool(worldID, countryID, size=100, factory, ref)` (first-time only; check if pool exists).

Inside the league loop, each AI club call passes `poolCountryID = countryID` to the new `GenerateAIClub` signature.

### New event: `PLAYER_CLAIMED_FROM_POOL`

Payload: `player_id`, `club_id`, `pool_country_id`.

## Acceptance criteria

- `go test ./internal/playerpool/...` — unit tests for SeedPool, DraftSquad, age distribution, replenish.
- `go test ./internal/bootstrap/...` — updated integration tests for pool-based starter club.
- `go test ./internal/competition/...` — seeded-competition integration test drafts from pool; free agents exist.
- `go vet ./... && go build ./...` clean.

## Delivery evidence

- Pending.
