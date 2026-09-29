# IM25 — Game-date math uses the world's calendar, not the server's

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (world clock / finance integrity)
**Source:** Doc-vs-code audit against OPD-24 (`worldDate = COALESCE(launched_at,
created_at) + current_day days`) and OPD-42 (fixed-scale clock,
`tick.day_length`). About twenty queries used Postgres's real `CURRENT_DATE`
for contract start/end dates, wage-commitment windows, player ages, contract
days-left, dashboard expiry windows, manager-history dates and instalment due
dates. In any world not at real-time scale (or after a pause) those drifted
from the game. The audit also found `FutureInstallments` summing the raw JSON
text (`SUM(elem->>'amount')`), which failed on every call; the error was
swallowed and the summary always reported 0.
**Depends on:** OPD-24 day counter, IM16 clock scale. Migration `0056`.

## What to do

- Add SQL helpers `world.world_date(world_id)` and
  `world.club_world_date(club_id)` (migration `0056_world_date`).
- Replace every `CURRENT_DATE` in game-date math with them; date new transfer
  contracts from the world date in Go.
- Only shorten wage commitments that are still running when a contract ends.
- Fix the instalment sum (cast per element, skip non-array JSON).

## Delivery evidence

### Migration

- `backend/migrations/0056_world_date.{up,down}.sql` — both functions (STABLE,
  launch day taken in UTC); row added to `backend/migrations/README.md`.

### Backend call sites

- Wage commitments: `finance/store.go` (`WageCommitments`,
  `ActiveWageCommitments`), `board/store.go` (committed wage),
  `admin/clubs.go`, `admin/finance.go`, `admin/service.go`.
- Ages / days-left: `transfer/store.go` (listing, attrs, active players),
  `transfer/ai.go`, `policybot/store.go`, `club/service.go` (`GetClubSquad`
  ages via `ageAt(dob, world_date)`).
- Contract lifecycle: `transfer/completion.go` (seller commitments end on the
  world date; buyer contract + commitment start on it and end
  `ContractLengthMonths` later), `playerpool/sign.go` (release),
  `manager/service.go` (history start/end), `dashboard/store.go` (expiring
  contracts window).
- `finance/store.go` `FutureInstallments` — `SUM((elem->>'amount')::bigint)`
  over array-typed installments, due dates vs the world date.

`grep -rn CURRENT_DATE backend/internal` now matches only the test harness.

### Verification

See [IM29](IM29-single-backend-image-deploy.md#verification). The finance,
transfer, board, dashboard, playerpool and manager integration suites exercise
every changed query.

## Recorded decisions

- **One definition of "today" per world**: `world.world_date`, i.e. OPD-24's
  launch date + `current_day`, in UTC.
- **Out of scope (follow-up):** timestamps that are genuinely real time
  (`created_at`, `responded_at`, session expiry) stay on `now()`. Player-promise
  deadlines and injury recovery clocks still use real time; aligning them to
  world days is a separate product decision (recorded as open in
  `docs/product_manager.md`, OPD-53).
