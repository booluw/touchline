# A04 — Club academy system and seasonal intake

**Status:** Implemented  
**Sprint:** Ad-hoc (player lifecycle)
**Source:** PRD §23; user design session
**Depends on:** A01, A03

## What to do

Build the club academy investment model and seasonal youth intake that produces 15–17 year-old academy prospects.

## Schema assumptions

`club.academies` table exists (A01).

## Changes

### internal/academy/service.go

```go
type Service struct { pool *pgxpool.Pool; bus eventbus.Publisher }

func NewService(pool *pgxpool.Pool, bus eventbus.Publisher) *Service

// EnsureAcademies creates a default tier-1 academy row for every club
// in the world that doesn't already have one. Called during seeding.
func (s *Service) EnsureAcademies(ctx context.Context, worldID uuid.UUID) error

// ConfigureAcademy updates a club's academy settings.
func (s *Service) ConfigureAcademy(ctx context.Context, clubID uuid.UUID, cfg AcademyConfig) error

// IntakeForClub generates youth prospects for one club in one season.
// Returns the new player IDs.
func (s *Service) IntakeForClub(ctx context.Context, clubID, countryID uuid.UUID, seasonNumber int, ref time.Time) ([]uuid.UUID, error)

// IntakeForWorld runs IntakeForClub for every non-shuttered academy in the world.
func (s *Service) IntakeForWorld(ctx context.Context, worldID, countryID uuid.UUID, seasonNumber int, ref time.Time) (int, error)
```

## Delivery evidence

- **`internal/academy`** (new package, `model.go`/`store.go`/`service.go`): `Service{EnsureAcademies, GetAcademy, RequireOwnership, SetInvestment, SetActive, IntakeForClub, IntakeForCountry, IntakeForWorld}`; deterministic per world+club+season intake keys (replays stable), ability/quality helpers (`ProspectCountForTier`, `QualityOffsetForTier`, `TalentOddsForTier`), tier tables + annual cost, 3-season youth-contract numerics. Tests green (`internal/academy/model_test.go` + integration Intake test).
- **`internal/playerpool`**: `PersistGeneratedPlayer` exported and used by academy intake to land intake youth rows; free-agent listing surfaces canonical `PositionalOverall` (read-model OVR) — `internal/playerpool/pool.go` + profile.go.
- **`pkg/playergen`**: region-parameterizable youth quality + `roll` machinery (tier odds/floor) from S08-01; talent/potential numerics landed.
- **`internal/finance`**: `SignAcademyProspects` (youth contract + `finance.wage_commitments` twin, per-player partial-unique idempotency) + `AcademyLedger` maintenance debit; `YouthWeeklyWage` tier-scaled. Tests green.
- **`internal/training`**: weekly `PlayerAttributeChanges` recording incl. morale-pseudo-key deltas (`internal/training/deltas.go`), migration `0043_player_attribute_changes` + `0044_youth_contracts`; `applyClubWeekly` persists net deltas + morale swing. Tests green.
- **`internal/competition`**: `SEASON_COMPLETED` payload carries `country_id` (rollover.go); `internal/app` wires academy maintenance + seasonal intake (WORLD_TICK monthly debit idempotent per tick; SEASON_COMPLETED → `IntakeForCountry`/`IntakeForWorld` fallback for league-less worlds).
- **HTTP + docs**: `GET/PUT /api/clubs/:id/academy` handlers in `internal/httpapi/academy_handlers.go`, wired in router + server, documented in openapi.yaml, docs-coverage test green; design doc `docs/design/academy-numerics.md` (single source of numerics truth).

## Acceptance criteria

- `go test ./internal/academy/...` — unit + intake.
- `go mod build` clean.
- API returns academy config.

### Intake formula

- Prospect count = base + investment_tier bonus: `3 + investment_tier * 2` (range: 5–13 prospects)
- Age: 15–17 uniformly
- Quality: `categoryMeans` offset = `investment_tier * 3 + reputation / 10` (capped ±20)
- Nationality bias: weighted toward the club's country (use `country_id` → nationality pool subset)
- All prospects: `origin = 'club_academy'`, `is_academy_product = true`
- Each gets a youth-type professional contract (wage: `investment_tier * 500` per week, 3-year term)
- `last_intake_season` updated

### Finance integration

Each academy contract's `weekly_wage` is inserted via `finance.AppendLedger` or direct `player.contracts` insert. Academy `annual_cost` is debited from the club's ledger as a seasonal entry on intake.

### Event

`ACADEMY_INTAKE` — payload: `{club_id, season_number, prospect_count, player_ids[]}`.

### API routes

```
GET  /api/clubs/:clubID/academy         → current academy config
PUT  /api/clubs/:clubID/academy         → update config (admin or club owner)
```

### Worker hook

In the `SEASON_COMPLETED` handler (to be wired in A06): call `IntakeForWorld`.

## Acceptance criteria

- `go test ./internal/academy/...` — unit tests for intake formula, age range, wage calculation.
- Integration test: seed world → ensure academies → intake → player count and attributes correct.
- `go vet ./... && go build ./...` clean.
- API: GET/PUT academy config returns proper JSON.

## Delivery evidence

- Pending.
