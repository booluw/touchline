# How to: set up and launch a local game (init → DB → admin → world → league → season)

A step-by-step walkthrough for turning a fresh machine (or fresh database) into a
running Touchline world with a working league and an active season. Every
command here was verified against the current build.

It assumes the bare-metal path from `docs/development.md`; the steps are the same
for the Docker Compose stack, just with the backend `go run` processes replaced
by the compose services.

**Scope:** initializing the application, syncing the DB, creating an admin
account, creating a world, creating a league, starting a season, and opening the
transfer window. Terminology note: "create a league" and "start a season" are
**two different steps with the same command** — seeding a competition both
materializes the league's clubs and creates season #1. See Step 6/7.

---

## 0. Prerequisites

- **Go 1.25** (`backend/go.mod`) and **pnpm** (frontend; never npm).
- A **reachable Postgres** with an empty (or scratch) database. Locally the
  project uses `postgres://touchline@localhost:55432/touchline?sslmode=disable&host=/tmp`.
- **golang-migrate** (`brew install golang-migrate`, or
  `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest`).

Environment variables (see `docs/development.md` for the full table):

| Variable | Required | Example |
|---|---|---|
| `DATABASE_URL` | Yes | `postgres://touchline@localhost:55432/touchline?sslmode=disable&host=/tmp` |
| `JWT_SECRET` | Yes (fail-fast in bare metal) | `dev-secret-change-me` |
| `APP_ORIGIN` | For the browser flow | `http://localhost:3000` |
| `ENV` | For local dev | `development` (otherwise cookies get `Secure`) |
| `REDIS_URL` | No (fail-soft) | `redis://localhost:6379` |

The rest of this guide exports the connection string as:

```bash
export DATABASE_URL="postgres://touchline@localhost:55432/touchline?sslmode=disable&host=/tmp"
```

---

## 1. Initialize the application

Backend binaries (three terminals, or one shell with `&`):

```bash
cd backend
set -a; source ../.env 2>/dev/null; set +a   # optional: load repo-root .env
export JWT_SECRET=dev-secret-change-me
export APP_ORIGIN=http://localhost:3000
export ENV=development
go run ./cmd/api          # :8080
go run ./cmd/scheduler    # :8081 world clock
go run ./cmd/worker       # :8082 engine consumer
```

Frontend:

```bash
cd frontend
pnpm install
pnpm dev                  # :3000
```

Verify:

```bash
curl -s http://localhost:8080/health     # {"status":"ok"}
curl -s http://localhost:8080/health/db  # {"status":"db-ok"}
```

The Compose alternative (`docker compose up --build`) boots migrations, ref-seed,
api, scheduler, worker, and frontend automatically. Continuing below assumes the
API is on `:8080`.

---

## 2. Sync the database (migrations + reference data)

The DB must be migrated to the current schema **before** any binary or `cmd`
tool may use it. Never mix this step with tests — the test harness migrates and
truncates the database it is pointed at (`TEST_DATABASE_URL`), which would wipe
this world.

```bash
cd backend
migrate -database "$DATABASE_URL" -path migrations up
```

Confirm you are on the latest migration (0035 as of S05-02):

```bash
psql "$DATABASE_URL" -tAc "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1;"
# 35 | f
```

Then seed the curated reference data. **This is mandatory** — world bootstrap and
league materialization generate names/squads from these pools and fail with
`ErrRefDataMissing` otherwise:

```bash
cd backend
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names
```

---

## 3. Create an admin account

There is no signup endpoint (`OPD-02`); accounts are made with `cmd/user-create`.
It creates the `auth.users` row **and** an unemployed `manager.managers` row in a
**world that must already exist**. The chicken-and-egg: to make the first admin
you need one world row.

If a world already exists (an earlier run, as on this dev box), reuse its id:

```bash
psql "$DATABASE_URL" -tAc "SELECT id, name, status FROM world.worlds ORDER BY created_at;"
```

If the `world.worlds` table is empty, create one bootstrap provisioning world row
directly (the admin API takes over from here):

```bash
psql "$DATABASE_URL" -tAc "INSERT INTO world.worlds (name, status) VALUES ('bootstrap', 'provisioning') RETURNING id;"
```

Then create the admin account against it:

```bash
cd backend
ADMIN_PW='change-me' go run ./cmd/user-create \
  -email admin@example.com \
  -password-env ADMIN_PW \
  -world-id <world-id> \
  -admin
```

`-admin` marks `auth.users.is_admin`; every `/api/admin/*` route requires it.
Without `-admin` the same command creates a plain (jobless) manager — this is
how you'd add the human manager who later accepts the job offer.

Verify by logging in and hitting a protected route (an `is_admin` flag shows up
on `/api/auth/login` responses):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"change-me"}'
```

---

## 4. Create a world

Use the admin API from here on (the psql row above was only the bootstrap step).
Login again (if your shell session reset), then:

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds \
  -H 'Content-Type: application/json' \
  -d '{"name":"Touchline Test Division"}'
```

This returns the new world in **`provisioning`** state — not yet playable.
Save its `id` as `$WORLD_ID`.

Optional: `POST /api/admin/worlds/:id/status` flips a world's lifecycle later
(`active`/`open_beta` = playable, `paused`, `archived` terminal). Provisioning
states are fine for the next steps.

> Bootstrap requires the world to still be `provisioning`; seeding only rejects
> `archived` worlds. But lifecycle status **does** gate the scheduler: only
> playable worlds get `WORLD_TICK`s, and matches only kick off once the world is
> launched. Launching happens in Step 7.

A world is an empty shell until bootstrap. Materialize the starter club, its
policy-bot manager, and a generated 24-player squad (S03-01; this is also where
S05-02 mints the club's finance account, opening capital, budgets, and starter
contracts):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/bootstrap \
  -H 'Content-Type: application/json' \
  -d '{"name":"Harbour United","short_name":"HAR"}'
# 201: world_id, club_id, club_name, manager_id, squad_size, random_seed, players
#      (the parallel CLUB_CREATED event carries contract_count)
```

Save the returned `club_id` as `$STARTER_CLUB`. It appears in the authenticated
club reads:

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/clubs            # caller world only
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/clubs/$STARTER_CLUB
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/clubs/$STARTER_CLUB/finances
```

---

## 5. Create a league

Leagues are world-scoped and admin-declared per country (`OPD-20`: nothing is
invented — the admin fixes every number). The starter club will be placed in the
league you nominate as the **starter league**.

**5.1 Create a country** (world-scoped):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/countries \
  -H 'Content-Type: application/json' \
  -d '{"world_id":"'"$WORLD_ID"'","code":"ENG","name":"England"}'
# 201: {id, world_id, code, name}
```

Save the `id` as `$COUNTRY_ID`. Listing reads:
`GET /api/admin/countries?world_id=$WORLD_ID` (admin) and
`GET /api/countries` (any authenticated manager in this world).

**5.2 Create league(s)** — one per tier. At minimum create the league that will
host the starter club. `team_count` must be **even and ≥ 4**; a 4-team league is
the smallest playable season.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Premier Division","tier":1,"team_count":6}'
# 201: {id, world_id, country_id, name, tier, team_count, status, ...}
```

Save the `id` as `$STARTER_LEAGUE`. Optional fields: `promotions`, `relegations`,
`promotes_to`, `relegates_to`. Links must reference a league in the **same
country** (`ErrBadAdjacency` otherwise), and movement counts must be **symmetric
across the pair** (`ErrAdjacencyMismatch`: the league above must relegate exactly
as many as the league below promotes). Because leagues validate at seed time,
wire links once both sides exist — a correct two-tier pyramid:

```bash
# tier 1 first (relegations:1; its link comes after tier 2 exists)
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Premier Division","tier":1,"team_count":6,"relegations":1}'
# tier 2 promotes 1 up into tier 1 (tier 1 already exists, so the link is valid)
TIER2_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Championship","tier":2,"team_count":6,"promotions":1,"promotes_to":"'"$STARTER_LEAGUE"'"}' \
  | jq -r .id)
# close the loop: tier 1 relegates down to tier 2
curl -c /tmp/jar -b /tmp/jar -X PATCH localhost:8080/api/admin/leagues/$STARTER_LEAGUE/adjacency \
  -H 'Content-Type: application/json' -d '{"relegates_to":"'"$TIER2_ID"'"}'
```

---

## 6. Create the league's clubs and season

One admin call materializes the whole country into playable shape (`OPD-18`):
for every league it guarantees `team_count` entries — the world's existing clubs
are reused (starting with the starter club in `$STARTER_LEAGUE`) and the rest are
generated as AI clubs with squads — then it creates **season #1 (`in_progress`)**
and schedules a deterministic double round-robin fixture list, **one matchday per
day** from the world's season reference date.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/seed-competition \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","starter_league_id":"'"$STARTER_LEAGUE"'"}'
# 201: {world_id, country_id, leagues:[{league_id, name, tier, team_count,
#        season_id, season_label, fixture_count, matchdays, clubs:[...]}]}
```

**This one call IS both "create the league" (materialized clubs)
and "start a season".** Notes:

- **One seed per league.** A league that already has a season is a
  409 `ErrLeagueAlreadySeeded`. There is no re-seed — retries need a fresh world.
- Every club in the world must end up in some league, so every other world club
  is consumed by `$STARTER_LEAGUE` up to its `team_count`; a leftover club is a
  409 `ErrNoStarterClub` ("world has clubs that fit no league — adjust team
  counts").
- The world must already be bootstrapped (the seed needs `WORLD_BOOTSTRAPPED`
  `random_seed` for determinism) — `409 ErrWorldNotBootstrapped` otherwise.
- Seeding works from `provisioning`/`active`/`paused`; only `archived` is
  rejected. Nothing plays until the world is launched (next step).

Verify the season and fixtures (authenticated, world-scoped):

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/competitions
curl -c /tmp/jar -b /tmp/jar "localhost:8080/api/competitions/$STARTER_LEAGUE/fixtures"
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/competitions/$STARTER_LEAGUE/standings
# league's season status: SELECT status, season_number FROM competition.seasons WHERE competition_id='...'
```

---

## 7. Start the season running (launch + tick cadences)

Seeding *creates* the season and its schedule; **matches only kick off once the
world is playable and the daily tick fires.** Three things compose:

1. **Launch the world** (`provisioning → active`). The scheduler ignores
   non-playable worlds, so this is when the clock starts.
2. **Daily ticks drive the matchday runner.** Every daily `WORLD_TICK` makes the
   worker `KickoffDue` any matchday whose `scheduled_at` has arrived, then a
   per-world `RunLive` paces those matches in real time and applies results. The
   world's `current_day` also advances +1 per daily tick (OPD-24).
3. **The season reference date** is the world's `launched_at`/`created_at`;
   matchday *n* plays on day *n* — so to see football soon, accelerate the
   cadence (default daily is `0 */8 * * *`).

```bash
# 1. launch
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/status \
  -H 'Content-Type: application/json' -d '{"status":"active"}'

# 2. accelerate the cadences (takes effect on the next scheduler poll, ~15s)
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.daily_cadence","value":"* * * * *"}'
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.weekly_cadence","value":"* * * * *"}'
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.monthly_cadence","value":"* * * * *"}'
```

The weekly tick drives S05-01 training (plan archetypes take effect from the next
weekly tick), the monthly tick drives S05-02 wage posting (`4 × weekly_wage` per
active contract, idempotent ledger write + `WAGE_POSTED` event).

Watch it happen in the worker logs (`daily tick: kicked X matchday(s), Y
fixture(s)`), or poll the match feed:

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/fixtures/<fixture-id>
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/matches/<match-id>/events
```

**Season completion is automatic.** When the final fixture's result is applied,
the standings roll over (promotions/relegations), season #1 is marked
`completed`, and season #2 is created as `upcoming` with fresh fixtures —
`SEASON_COMPLETED`/`SEASON_CREATED` events ride the same transaction. No manual
"next season" step exists.

**Get a human manager into the loop.** To play the starter club yourself, first
create a plain account (Step 3 without `-admin`), then as admin issue it a job
offer for the AI starter club (`OPD-16`):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/offers \
  -H 'Content-Type: application/json' \
  -d '{"club_id":"'"$STARTER_CLUB"'","manager_id":"<unemployed-manager-id>"}'
```

The candidate then sees/accepts the offer (`GET /api/managers/me/offers`,
`POST /api/offers/:id/accept`), and owns the club — from then on club, tactics,
training-plan, finances/ledger/contracts reads are theirs (and only theirs).

---

## 8. Open the transfer window

**Not yet implemented** — the transfer market is the S06 slice.

What exists *today* is only the financial edge: wage commitments, the ledger,
and contract reads (S05-02) all derive from server-side posting, and
`finance.RegisterContract` (contract + wage-commitment rows + `CONTRACT_COMMITTED`
event in one transaction) is implemented as service code. There is **no** HTTP
surface for it and no listable/biddable market, so there is nothing to run here.

The `internal/transfer` package currently holds the planned domain shapes only
(an interface stub — no implementation, no routes):

- `TransferListing` (`status`: active/accepted/rejected/expired), `Bid`
  (pending/accepted/rejected/countered/withdrawn), `Negotiation`
  (active/completed/collapsed), `Clause` (sell_on/buy_back/release), `Loan`.
- Planned service seam: `GetActiveListings`, `PlaceBid`, `RespondToBid`.

When S06 lands, the doc's Step 8 will begin with an admin-action or season-gated
window toggle (to be specified), after which `GET /api/.../listings` and the bid
flow become callable — and transfer fee installments will start feeding the
finance summary's `future_installments` / `committed_spending` (currently 0;
see `docs/design/finance-numerics.md`).

---

## 9. Troubleshooting

| Symptom | Error/cause | Fix |
|---|---|---|
| `ErrRefDataMissing` on bootstrap | `ref-seed` never ran | Run `go run ./cmd/ref-seed` (Step 2) |
| `ErrWorldNotProvisioning` on bootstrap | world already launched or bootstrapped | Bootstrap the world before setting it `active` |
| `ErrAlreadyBootstrapped` | world bootstrapped twice | It is one-shot; new world required |
| `ErrWorldNotBootstrapped` on seeding | no `WORLD_BOOTSTRAPPED` (starter club missing) | Bootstrap first (Step 4) — no world seed = no world seed randomness |
| `ErrCountryWorldMismatch` | country belongs to another world | Reuse the `country_id` returned for *this* world |
| `ErrCountryHasNoLeagues` | seeding a country with no leagues | Create ≥1 league first (Step 5) |
| `ErrInvalidTeamCount` / 400 | `team_count` odd or < 4 | Use an even count ≥ 4 |
| `ErrBadAdjacency` / `ErrAdjacencyMismatch` | promotion/relegation link bad, or counts asymmetric | Link a league in the same country; the league above must relegate exactly as many as the league below promotes |
| `ErrLeagueAlreadySeeded` | league already has a season | One seed per league; no re-seed — fresh world |
| `ErrNoStarterClub` | a world club fits no league | Raise the starter league's `team_count` so every club lands somewhere |
| `ErrDuplicateEntry`-style 409s on offers | manager already has a job (one-job-per-user) | Resign/sack the current assignment first |
| `go run ./cmd/api` exits immediately | `JWT_SECRET` unset | `export JWT_SECRET=…` (set in Step 1) |
| Scheduler fires no ticks | world not `active`/`open_beta` | Launch via Step 7 (playable worlds only) |
| Cadence change "does nothing" | next resample is up to 15s away | Wait one `SCHEDULER_POLL_INTERVAL`; `tick.match_cadence` is deliberately ignored by the world clock (`OPD-17`) |
| Matches never start | daily tick not firing or fixture date in the future | Set `tick.daily_cadence=* * * * *`; fixtures are day-gated by the season reference date |

---

## Appendix — full happy-path cut & paste

```bash
export DATABASE_URL="postgres://touchline@localhost:55432/touchline?sslmode=disable&host=/tmp"
cd backend && migrate -database "$DATABASE_URL" -path migrations up
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names

W=$(psql "$DATABASE_URL" -tAc "SELECT id FROM world.worlds LIMIT 1")
[ -z "$W" ] && W=$(psql "$DATABASE_URL" -tAc "INSERT INTO world.worlds(name,status) VALUES('bootstrap','provisioning') RETURNING id")

ADMIN_PW='change-me' go run ./cmd/user-create -email admin@example.com -password-env ADMIN_PW -world-id "$W" -admin

# start api/scheduler/worker, then:
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' -d '{"email":"admin@example.com","password":"change-me"}'
WORLD_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds \
  -H 'Content-Type: application/json' -d '{"name":"Demo Division"}' | jq -r .id)
CLUB_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/bootstrap \
  -H 'Content-Type: application/json' -d '{"name":"Harbour United","short_name":"HAR"}' | jq -r .club_id)
COUNTRY_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/countries \
  -H 'Content-Type: application/json' \
  -d '{"world_id":"'"$WORLD_ID"'","code":"ENG","name":"England"}' | jq -r .id)
LEAGUE_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Premier Division","tier":1,"team_count":6}' | jq -r .id)
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/seed-competition \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","starter_league_id":"'"$LEAGUE_ID"'"}'
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/status \
  -H 'Content-Type: application/json' -d '{"status":"active"}'
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.daily_cadence","value":"* * * * *"}'
```