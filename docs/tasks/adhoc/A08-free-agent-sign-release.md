# A08 — Free-agent list, sign, and release APIs

**Status:** Implemented
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session
**Depends on:** A01, A03, A07
**Implemented:** 2026-09-17

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
    worldID, playerID, clubID uuid.UUID, wage int64, years int, ref time.Time) error

// ReleasePlayer terminates the player's contract, sets club_id=NULL,
// status='free_agent', and returns them to the country pool.
func ReleasePlayer(ctx context.Context, tx pgx.Tx, pub eventbus.Publisher,
    playerID uuid.UUID, reason string) error
```

### Handlers

`internal/httpapi/freeagent_handlers.go`:

- `handleListFreeAgents` — paginated query of `player.players WHERE club_id IS NULL AND status = 'free_agent'` with optional position/age/nationality filters. Joins person for age, player_attributes for overall.
- `handleSignFreeAgent` — validates club ownership (via the finance ownership gate), calls `playerpool.SignFreeAgent`.
- `handleReleasePlayer` — validates ownership (admin or the player's current club), calls `playerpool.ReleasePlayer`.

### Events

- `PLAYER_SIGNED` — `{player_id, club_id, weekly_wage, origin}` + `years`
- `PLAYER_RELEASED` — `{player_id, previous_club_id, reason, origin}`

### Finance

`SignFreeAgent` creates the club's finance account if missing (idempotent `finance.EnsureAccount`) and inserts the wage commitment twin alongside the professional contract, mirroring the academy signing machinery (S05-02). `ReleasePlayer` terminates active contracts and ends their wage commitments (reusing the transfer-service termination pattern).

## Acceptance criteria

- `go test ./internal/playerpool/...` — sign, release, eligibility guards. — see `sign_test.go` (`TestCheckSignEligible`, `TestYearsSince`, `TestApplyFreeAgentFilter`) + `sign_integration_test.go` (`TestSignReleaseIntegration`, `//go:build integration`).
- API integration test: seed world → list free agents → sign one → verify club has the player → release → player back in pool. — covered by `TestSignReleaseIntegration` (requires a Postgres-backed run).
- `go vet ./... && go build ./...` clean. — verified.

## Delivery evidence

- `internal/playerpool/sign.go` — `SignFreeAgent` guards (free-agent status + no club, street ≥ 18, wage affordability against the club's current-season wage budget), then in one tx: senior contract + `finance.wage_commitments` twin + `finance.EnsureAccount` + club assignment + `PLAYER_SIGNED` event. `ReleasePlayer` terminates active contracts (and their wage commitments) and returns the player to the country pool with a `PLAYER_RELEASED` event. Both use `eventbus.WriteTx` inside the caller's tx (OPD-23). `checkSignEligible` is the pure, DB-free gate mirroring A07.
- `internal/playerpool/model.go` — `FreeAgentFilter{CountryID, Position, AgeMin, AgeMax, Nationality}`.
- `internal/playerpool/pool.go` — `ListFreeAgents` now takes `FreeAgentFilter`, aggregates positional overall, computes age, applies position/age/nationality filters, and reports `total` reflecting the filtered set.
- `internal/httpapi/freeagent_handlers.go` — the three handlers plus `poolStatus`/`financeOwnershipStatus` error mapping, `isAdmin`, `playerClub`, `parseFreeAgentFilter`. Routes wired in `internal/httpapi/router.go`.
- `internal/httpapi/server.go` + `internal/app/app.go` — added `Bus eventbus.Publisher` to the httpapi `Options` so handlers can emit domain events (previously the server only carried the realtime hub).
- `internal/apidocs/openapi.yaml` — new paths (`/api/worlds/{worldID}/countries/{countryID}/free-agents`, `/api/clubs/{id}/free-agent-signings`, `/api/players/{playerID}/release`) + `FreeAgent`, `FreeAgentList`, `FreeAgentSigningInput`, `ReleasePlayerInput`, `OkResponse` schemas. `TestDocsCoverRouter` passes.
- Notes: endpoints live in `internal/httpapi` (the repo's handler package), not `internal/api` as the original brief wrote. Sign/release are compiled but their integration path only runs with a Postgres backend (repo convention: `//go:build integration`).

## Known deviation

- `SignFreeAgent` accepts `worldID` and `ref` so club-world and age checks can be performed correctly against the request's world and reference date; the task's sketch had neither.