# A07 — Match eligibility gate

**Status:** Implemented
**Sprint:** Ad-hoc (player lifecycle)
**Source:** User design session
**Depends on:** A01, A03
**Implemented:** 2026-09-17

## What to do

Enforce the match-eligibility rule in `squad.LoadSquad`: a player is match-eligible if they hold an active professional contract AND (they are not a street-origin player OR they are at least 18 years old).

## Changes

### internal/squad/store.go — LoadSquad

Modify `loadPlayerRows` query to join `player.contracts` and compute eligibility:

```sql
SELECT p.id, p.primary_position, p.status, p.origin,
       COALESCE(h.consistency, 50), ...
       CASE WHEN EXISTS (
            SELECT 1 FROM player.contracts c
            WHERE c.player_id = p.id AND c.status = 'active')
       THEN TRUE ELSE FALSE END AS has_contract,
       COALESCE(
           (SELECT EXTRACT(YEAR FROM age(p2.date_of_birth::date))
            FROM person.people p2
            WHERE p2.id = p.person_id), 0) AS computed_age
FROM player.players p
LEFT JOIN player.player_hidden_traits h ON h.player_id = p.id
LEFT JOIN player.player_personality ps ON ps.player_id = p.id
WHERE p.club_id = $1
ORDER BY p.id
```

In the Go scan loop:

```go
p.Available = p.Status == "active" && hasContract && !open &&
    (p.Origin != "street" || computedAge >= 18)
```

### LoadedPlayer additions

```go
type LoadedPlayer struct {
    // ... existing fields ...
    Origin     string
    ComputedAge int
    HasActiveContract bool
}
```

## Acceptance criteria

- Street-origin player aged 17 with active contract: `Available = false`. — covered
- Street-origin player aged 18 with active contract: `Available = true`. — covered
- Academy product aged 16 with active contract: `Available = true`. — covered
- Any player with no active contract: `Available = false`. — covered
- `go test ./internal/squad/...` — eligibility unit tests. — see `TestMatchEligibleA07`
- `go vet ./... && go build ./...` clean. — verified

## Delivery evidence

- `internal/squad/store.go` — `loadPlayerRows` now selects `p.origin`, LEFT JOINs
  `person.people` for `computed_age`, and computes `has_contract` via
  `EXISTS(player.contracts ... status='active')`. `LoadedPlayer` gains
  `Origin`, `ComputedAge`, `HasActiveContract`. The eligibility rule is
  factored into a pure `MatchEligible(status, origin, hasContract, open, computedAge)`
  helper so it is unit-testable without a DB.
- `internal/squad/store_test.go` — `TestMatchEligibleA07` covers the four
  acceptance cases plus retired/injured/on-loan/open-injury variants.
  `go test ./internal/squad/...` passes.
- `go build ./... && go vet ./...` clean.
