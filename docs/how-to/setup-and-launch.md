# How to: set up and launch a local game (init → DB → admin → world → seed → season)

A step-by-step walkthrough for turning a fresh machine (or fresh database) into a
running Touchline world with a working league and an active season. Every
command here was verified against the current build.

It assumes the bare-metal path from `docs/development.md`; the steps are the same
for the Docker Compose stack, just with the backend `go run` processes replaced
by the compose services.

**Scope:** initializing the application, syncing the DB, creating an admin
account, creating a world, declaring countries/leagues, seeding the world, and
starting a season.

Terminology note: **"seed the world"** and **"start a season"** are two separate
steps. Phases 1–5 encode a two-step launch model:

**Related how-tos (same directory):** calendars, tick granularities, and the
season/rollover lifecycle are [cadences-and-time.md](cadences-and-time.md) +
[seasons.md](seasons.md); the (always-open) transfer market is
[transfer-market.md](transfer-market.md); offers, accepting, and declining are
[job-offers-and-decisions.md](job-offers-and-decisions.md); every term and
derivation is indexed in [glossary.md](glossary.md).

1. **Declare** the world, its countries, and its leagues — pure metadata, no
   clubs yet.
2. **Seed** the whole world in one admin call — every league is filled up to its
   `team_count` with **AI clubs + squads** (all AI; no human starter club, no
   seasons, no fixtures). Re-seeding is incremental and idempotent.
3. **Start a season per league** afterwards via the admin endpoint
   `POST /api/admin/worlds/:id/leagues/:leagueID/season` — this is what creates
   the `in_progress` season and its deterministic fixture list.

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

Backend — one process runs the whole game (API :8080 + world clock + engine
consumer). The interactive API reference is then at `http://localhost:8080/api/docs`:

```bash
cd backend
set -a; source ../.env 2>/dev/null; set +a   # optional: load repo-root .env
export JWT_SECRET=dev-secret-change-me
export APP_ORIGIN=http://localhost:3000
export ENV=development
go run ./cmd/touchline        # serve :8080 (api + scheduler + worker)
```

The same binary splits roles for pod isolation (`go run ./cmd/touchline api`
on :8080, `scheduler` on :8081, `worker` on :8082).

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

Confirm you are on the latest migration (0048 as of the launch restructure):

```bash
psql "$DATABASE_URL" -tAc "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1;"
# 48 | f
```

Then seed the curated reference data. **This is mandatory** — seeding generates
names/squads from these pools and fails with `ErrRefDataMissing` otherwise. Two
pools: generic name data (squads) and the club-name corpus (seed tables):

```bash
cd backend
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names -clubdata data/clubs
```

---

## 3. Create an admin account

The product signup flow is `POST /api/auth/register` (Step 8b): it creates a
plain manager account, auto-joins the single playable world, and auto-offers a
first AI-club job. `cmd/user-create` is the **dev/admin bootstrap**, not the
signup flow — it creates the `auth.users` row and, **only if you pass
`-world-id`**, an unemployed `manager.managers` row in that world. No world
needs to exist first — the first admin can (and should) be created standalone,
then creates the world through the admin API:

```bash
cd backend
ADMIN_PW='change-me' go run ./cmd/user-create \
  -email admin@example.com \
  -password-env ADMIN_PW \
  -admin
```

`-admin` marks `auth.users.is_admin`; every `/api/admin/*` route requires it.
Without `-admin` the same command creates a plain (jobless) account — only
needed to bootstrap someone before any playable world exists, since regular
signup needs at least one playable world. Pass `-world-id` to also mint the
manager row up front if you prefer.

**Login resolution (OPD-15(4)):** login is no longer admin-only. An admin
always gets a **world-less** console session (admins run the global console, not
a manager's world). Any other account is resolved against its
`manager.managers` memberships (non-archived worlds only):

- the world where the account holds an **active job** always wins;
- a jobless account may post login with an explicit `world_id`;
- with **exactly one** joined world the session is minted for it;
- with **several** joined worlds the response is the world picker
  `{"status":"worlds","worlds":[{world_id,name,status}]}` — no cookies set, the
  client re-posts with the chosen `world_id`;
- with **no** joined world login is refused
  `403 {"error":"no world joined — …"}`.

Verify by logging in (an `is_admin` flag shows up on the response):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"change-me"}'
```

---

## 4. Create a world

Use the admin API from here on.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds \
  -H 'Content-Type: application/json' \
  -d '{"name":"Touchline Test Division"}'
```

This returns the new world in **`provisioning`** state — not yet playable. Save
its `id` as `$WORLD_ID`.

Optional: `POST /api/admin/worlds/:id/status` flips a world's lifecycle later
(`active`/`open_beta` = playable, `paused`, `archived` terminal). Seeding works
from `provisioning`/`active`/`paused`; only `archived` is rejected. But lifecycle
status **does** gate the scheduler: only playable worlds get `WORLD_TICK`s, and
matches only kick off once the world is launched. Launching happens in Step 7.

A world is an empty shell until it is **seeded** — there is no one-club bootstrap
step anymore. Seeding (Step 5) materializes all clubs + squads at once as AI
clubs, and mints the world's deterministic replay seed on its first successful
run.

**World-scoped reads.** The manager-facing routes (`/api/clubs`, `/api/competitions`,
`/api/countries`, fixtures/standings, finances, tactics, …) resolve the caller's
world from their session's `ManagerID`. Admin console sessions are world-less, so
they return `403 "no world context"`. To exercise them as this admin, give the
admin a manager row in the world (or mint a manager session; the integration
tests use `pkgauth.GenerateTokenPair`):

```bash
psql "$DATABASE_URL" -tAc "INSERT INTO manager.managers (world_id, user_id, is_policy_bot, status)
  SELECT '$WORLD_ID', id, FALSE, 'active' FROM auth.users WHERE email = 'admin@example.com' RETURNING id;"
```

Reuse the returned id as `$ADMIN_MANAGER` wherever the guide needs a manager
session cookie.

---

## 5. Declare countries and leagues

Leagues are world-scoped and admin-declared per country (`OPD-20`: nothing is
invented — the admin fixes every number). This is **metadata only**; no clubs are
created by these calls.

**5.1 Create a country** (world-scoped):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/countries \
  -H 'Content-Type: application/json' \
  -d '{"world_id":"'"$WORLD_ID"'","code":"GB","name":"England"}'
# 201: {id, world_id, code, name}
```

Save the `id` as `$COUNTRY_ID`. Listing reads:
`GET /api/admin/countries?world_id=$WORLD_ID` (admin) and
`GET /api/countries` (manager-scoped, this world only).

A country code is matched **case-insensitively** against the player-generator
nationality slugs (`ref.nationalities`): `BR` → Brazilian names in that
country's street/academy intakes, `GB` → English names, `SCO` → Scottish.
England ships as `data/names/gb.json` (ISO 3166-1 alpha-2). After adding or
renaming a nationality file, re-run `go run ./cmd/ref-seed` so the reference
tables and name pools pick it up.

**5.2 Create league(s)** — one per tier. `team_count` must be **even and ≥ 4**; a
4-team league is the smallest playable season.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Premier Division","tier":1,"team_count":6}'
# 201: {id, world_id, country_id, name, tier, team_count, status, ...}
```

Save the `id` as `$LEAGUE_ID`. Optional fields: `promotions`, `relegations`,
`promotes_to`, `relegates_to`. Links must reference a league in the **same
country** (`ErrBadAdjacency` otherwise), and movement counts must be **symmetric
across the pair** (`ErrAdjacencyMismatch`: the league above must relegate exactly
as many as the league below promotes). Because adjacencies validate at seed time,
wire links once both sides exist — a correct two-tier pyramid:

```bash
# tier 1 first (relegations:1; its link comes after tier 2 exists)
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Premier Division","tier":1,"team_count":6,"relegations":1}'
# tier 2 promotes 1 up into tier 1 (tier 1 already exists, so the link is valid)
TIER2_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Championship","tier":2,"team_count":6,"promotions":1,"promotes_to":"'"$LEAGUE_ID"'"}' \
  | jq -r .id)
# close the loop: tier 1 relegates down to tier 2
curl -c /tmp/jar -b /tmp/jar -X PATCH localhost:8080/api/admin/leagues/$LEAGUE_ID/adjacency \
  -H 'Content-Type: application/json' -d '{"relegates_to":"'"$TIER2_ID"'"}'
```

---

## 6. Seed the whole world

One admin call materializes the entire world into playable shape (`OPD-18`):
for **every** league it guarantees `team_count` entries — it reuses the world's
existing clubs first, and generates the rest as AI clubs with squads, managers,
and `club_competitions` memberships. No seasons or fixtures yet. The seed runs
as an **async job** (the `seed` queue): the POST returns **202 queued** and the
worker materializes the world in the background.

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/seed
# 202: {"status":"queued","world_id":"…","job_id":…}

# Poll until the world is fully seeded (or the job ends in a terminal state):
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/admin/worlds/$WORLD_ID/seed-status
# {"world_id":"…","world_seeded":false,"leagues":{"total":1,"seeded":1},"clubs":6,
#  "pool_size":100,"job":{"id":…,"state":"completed","attempt":1,"max_attempts":5,
#   "attempted_at":"…","finished_at":"…","last_error":null}}
```

Notes:

- **Async by design.** `POST /seed` validates synchronously (`404 ErrWorldNotFound`,
  `422 ErrWorldArchived`/`ErrWorldHasNoLeagues`) and then queues a
  `seed_world` river job, returning `202`. The job **must be consumed by a
  worker** — the all-in-one `go run ./cmd/touchline serve` runs one, as does the
  isolated `worker` role. `GET /api/admin/worlds/$WORLD_ID/seed-status` reports
  the job's state plus real progress (`world_seeded`, `leagues.total/seeded`,
  `clubs`, `pool_size`). Duplicate POSTs for the same world coalesce while a
  job is pending/running (unique by args); after it finishes a follow-up is a
  cheap no-op.
- **All AI.** Every seeded club is `is_ai_controlled = true` (policy-bot
  manager, generated 24-player squad, short name drawn from the club-name
  corpus). There is no human starter club.
- **Incremental + idempotent.** The call fills every league up to `team_count`,
  creating only the clubs that are missing. Re-running it is a `202` whose job
  completes with `new_clubs: 0` (`seed-status` keeps reporting the same counts).
  Leagues added later are picked up by the next run.
- **Seeding has no season side effects.** Clubs are placed in as many leagues as
  declared (league memberships); season/fixture creation is the `StartSeason`
  seam (Step 7).
- **First successful job mints `world_seed`.** The world's replay seed
  (`random_seed`, a crypto-random int64) is generated on first successful run
  and stored on `world.worlds`; per-league name scrambling is derived from
  `seed ⊕ leagueID`. A failed run rolls back atomically and leaves it unset —
  the error surfaces on `seed-status` under `job.last_error`.
- **Country free-agent pool is minted before the first draft.** Seeding tops
  each country's pool up to 100 free agents (`playerpool.PoolTargetSize`)
  *before* the first AI club of that country drafts from it, then tops it back
  up after every club — so a whole multi-league seed never runs the pool dry.
  A missing mint fails the first club with `ErrPoolTooSmall` (see Step 10).
- Progress is logged per phase on the backend (`seed world=<id> …`), e.g.
  `seed world=… country=England league=Premier: creating AI club "…" (2/6)`.
- Errors: the synchronous POST only 4xxes validation problems; runtime failures
  show up in the logged job error and on `seed-status`.

Verify the seeded clubs (needs a **manager-scoped** session — this world's
`$ADMIN_MANAGER`; see Step 4):

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/clubs                                # 4 clubs (this world only)
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/clubs/<club-id>                      # detail: squad, manager, is_ai_controlled
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/competitions                          # declared leagues
# memberships: SELECT competition_id FROM competition.club_competitions WHERE club_id='<club-id>'
```

Player lifecycle additions (A01–A11, OPD-26..29):
- `internal/playerpool` mints the country-wide player pool on world seed; free agents (A08) + AI auto-fill backstop (A09) live here.
- `internal/lifecycle` owns aging/retirement/aftermath and the A07 eligibility gate (professional contract; street origin 18+).
- `internal/academy` feeds street-kids (13–15, origin `street`) and club-academy (origin `academy`) intakes into the pool.
- `internal/squad` sets the 24-player OPD-29 target; admin bulk create (A10): `POST /api/admin/worlds/:id/players/bulk`.
- Schema: migration `0036_player_lifecycle`; design: `backend/docs/design/player-lifecycle.md`.

---

## 7. Start a season (admin endpoint)

Start each league's season #1 through the admin API:

```bash
curl -c /tmp/jar -b /tmp/jar -X POST \
  localhost:8080/api/admin/worlds/$WORLD_ID/leagues/$LEAGUE_ID/season
# 201 → { "id": ..., "competition": {...}, "season_label": "2026/27",
#          "season_number": 1, "status": "in_progress" }
```

The endpoint wraps the `Service.StartSeason` seam: it creates the season
(`in_progress`), its `competition_entries`, and a deterministic double
round-robin fixture list — one matchday per game-day from the world's boot
reference date (fixtures are spaced ≥2 game-days apart to satisfy the phase-2
rest rule). Errors: `404` unknown world/league, `409` for an archived world, a
league from another world, an already-seeded league, or a league with no clubs
(run Step 6's seed first). Executable samples live in the integration suite
(`internal/competition/competition_integration_test.go`).

Later seasons need no manual step: the rollover creates them automatically
after the off-season gap (`season.off_season_ticks`, default 30 daily ticks;
per-league override via `competition_rules.scheduling_rules`) — see
[seasons.md](seasons.md) §3.

---

## 8. Launch the world and let the ticks run

Seeding materializes the clubs and `StartSeason` schedules the matches; **matches
only kick off once the world is playable and the daily tick fires.** Three things
compose:

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
weekly tick) and the player weekly pass (morale/playing-time/transfer-request
assessment), rivalry reconciliation, and the board's weekly confidence review.
The monthly tick drives S05-02 wage posting (`4 × weekly_wage` per active
contract, idempotent ledger write + `WAGE_POSTED` event) **and** academy
facility maintenance (`annual_cost / 12`). See
[cadences-and-time.md](cadences-and-time.md) for the full per-tick map and how
the granularities relate.

Watch it happen in the worker logs (`daily tick: kicked X matchday(s), Y
fixture(s)`), or poll the match feed (manager-scoped session):

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/competitions/$LEAGUE_ID/standings
curl -c /tmp/jar -b /tmp/jar "localhost:8080/api/competitions/$LEAGUE_ID/fixtures"
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/matches/<match-id>/events
```

**Season completion is automatic.** When the final fixture's result is applied,
the standings roll over (promotions/relegations), the season is marked
`completed`, and the next season is created as `upcoming` with fresh fixtures —
`SEASON_COMPLETED`/`SEASON_CREATED` events ride the same transaction. The next
season's fixtures start after the configured off-season gap
(`season.off_season_ticks`, default 30 daily ticks; see seasons.md §3) and are
activated on the daily tick. No manual "next season" step exists.

**Human managers play for real.** With login resolution open (Step 3), a
registered manager (Step 8b) or `user-create` account can log in with their own
session and accept the AI-club offer they were auto-offered on joining — or an
admin can issue further offers (`POST /api/admin/offers`) that the candidate
accepts from their manager session (`POST /api/offers/:id/accept`).

---

## 8b. Sign up a human manager (self-service registration)

Once exactly one playable world exists, `POST /api/auth/register` is the product
signup flow (dev/admin bootstrap stays `cmd/user-create`):

```bash
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"player@example.com","password":"change-me","display_name":"Player"}'
```

With a single playable world the response joins the account to it as an
unemployed manager and auto-issues a first job offer from the first available AI
club:

```json
{"id":"…","email":"player@example.com","display_name":"Player","is_admin":false,
 "world":{"world_id":"…","name":"…","status":"active"},
 "offer":{"id":"…","club_id":"…","club_name":"…","status":"proposed"}}
```

Notes:
- **Zero or two or more playable worlds** → the account is created world-less
  (`world: null`) and an admin must join it later (`cmd/user-create -world-id`).
- **No AI club** → `offer: null`; an admin can offer later.
- No session is minted by registration — the new manager logs in via Step 3's
  login resolution.
- No email verification/recovery yet (`OPD-02`), and no signup rate limiting.

---

## 9. The transfer market (always open)

**There is no transfer window.** As soon as the world is playable, the market
runs continuously: club-owning managers can list players
(`POST /api/transfers/listings`), bid on others (`POST /api/transfers/bids`),
and answer inbound bids (`POST /api/transfers/bids/:id/respond`). AI clubs
participate deterministically via the policy engine, and the daily tick
sweeps stale bids (`BidTTLWorldDays = 3`) and recomputes valuations. Full
model, wire format, and examples: [transfer-market.md](transfer-market.md).

```bash
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/transfers/listings                       # browse
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/transfers/bids \
  -H 'Content-Type: application/json' \
  -d '{"listing_id":"<id>","fee":7000000,"terms":{"weekly_wage":15000,"contract_length_months":36}}'
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/transfers/bids/<bid-id>/respond \
  -H 'Content-Type: application/json' -d '{"action":"accept"}'
```

---

## 10. Troubleshooting

| Symptom | Error/cause | Fix |
|---|---|---|
| `ErrRefDataMissing` on seed | `ref-seed` never ran | Run `go run ./cmd/ref-seed -data data/names -clubdata data/clubs` (Step 2) |
| `ErrWorldHasNoLeagues` on seed | world has no declared leagues | Create ≥1 country+league first (Step 5) |
| `POST /seed` returns `202` but nothing is created | no worker is consuming the `seed` queue | Run the all-in-one `go run ./cmd/touchline serve` (or the `worker` role); poll `GET /api/admin/worlds/$WORLD_ID/seed-status` |
| Seed job fails `load world: timeout: context deadline exceeded` | job outlived river's default 1-minute `JobTimeout` (river applies it when the client sets none), so the seed's context is cancelled mid-run; the retry then blocks on the previous attempt's world-row lock until its own deadline expires | Re-queue `POST /api/admin/worlds/$WORLD_ID/seed` with the current build — `SeedWorldWorker` overrides `Timeout` to 30 minutes (event jobs keep the default). Discarded jobs aren't retried; a fresh POST makes a new one |
| `free-agent pool has too few players to draft a squad` on seed | first AI club of a country drafted against an empty/pool-drained free-agent pool (old builds seeded squads before minting the country pool) | Re-queue `POST /api/admin/worlds/$WORLD_ID/seed` — seeding now mints the country pool to 100 before the first draft; the failure shows under `job.last_error` on `seed-status` |
| Seed failed / where's the result? | runtime seed error (see Step 6) | Poll `GET /api/admin/worlds/$WORLD_ID/seed-status` — `job.state` (e.g. `retryable`/`discarded`) and `job.last_error` carry the chained error (e.g. `generate AI club for …`) |
| Seed created `0` clubs | re-seed of an already-full world | Expected; idempotent. Add leagues/raise `team_count`, re-seed |
| `ErrLeagueAlreadySeeded` on StartSeason | league already has a season | One season per league; a completed season rolls to the next automatically |
| `ErrCountryWorldMismatch` | country belongs to another world | Reuse the `country_id` returned for *this* world |
| `ErrInvalidTeamCount` / 400 | `team_count` odd or < 4 | Use an even count ≥ 4 |
| `ErrBadAdjacency` / `ErrAdjacencyMismatch` | promotion/relegation link bad, or counts asymmetric | Link a league in the same country; the league above must relegate exactly as many as the league below promotes |
| `/api/clubs`, `/api/competitions`, … → `403 no world context` | admin console session is world-less | Use a manager-scoped session (grant the admin a manager row, Step 4) |
| Plain account can't log in (`403 no world joined`) | account has no `manager.managers` row in a non-archived world | Admin joins it: `cmd/user-create … -world-id`, or wait for signup to join the single playable world |
| Login returns a world *picker* (`status: worlds`) | jobless account joined to two or more worlds | Re-post login with the chosen `world_id` (OPD-15(4)(b)) |
| `POST /api/auth/register` returns `world: null` | zero (or two or more) playable worlds | Create/launch exactly one playable world (Step 8) then the account can be joined |
| Registered account got no auto-offer | world has no AI club without a human manager yet | Admin issues one later (`POST /api/admin/offers`) |
| `ErrDuplicateEntry`-style 409s on offers | manager already has a job (one-job-per-user) | Resign/sack the current assignment first |
| `go run ./cmd/touchline serve` exits immediately | `JWT_SECRET` unset | `export JWT_SECRET=…` (set in Step 1) |
| Scheduler fires no ticks | world not `active`/`open_beta` | Launch via Step 8 (playable worlds only) |
| Cadence change "does nothing" | next resample is up to 15s away | Wait one `SCHEDULER_POLL_INTERVAL`; `tick.match_cadence` is deliberately ignored by the world clock (`OPD-17`) |
| Matches never start | no season started, or daily tick not firing | Run `POST /api/admin/worlds/$WORLD_ID/leagues/$LEAGUE_ID/season` (Step 7); set `tick.daily_cadence=* * * * *`; fixtures are day-gated by the season reference date |

---

## Appendix — full happy-path cut & paste

```bash
export DATABASE_URL="postgres://touchline@localhost:55432/touchline?sslmode=disable&host=/tmp"
cd backend && migrate -database "$DATABASE_URL" -path migrations up
go run ./cmd/ref-seed -database "$DATABASE_URL" -data data/names -clubdata data/clubs

ADMIN_PW='change-me' go run ./cmd/user-create -email admin@example.com -password-env ADMIN_PW -admin

# start api/scheduler/worker, then:
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' -d '{"email":"admin@example.com","password":"change-me"}'
WORLD_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds \
  -H 'Content-Type: application/json' -d '{"name":"Demo Division"}' | jq -r .id)
COUNTRY_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/countries \
  -H 'Content-Type: application/json' \
  -d '{"world_id":"'"$WORLD_ID"'","code":"GB","name":"England"}' | jq -r .id)
LEAGUE_ID=$(curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/leagues \
  -H 'Content-Type: application/json' \
  -d '{"country_id":"'"$COUNTRY_ID"'","name":"Premier Division","tier":1,"team_count":6}' | jq -r .id)
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/seed
# poll until world_seeded:true (the seed runs as an async job)
until curl -s -c /tmp/jar -b /tmp/jar localhost:8080/api/admin/worlds/$WORLD_ID/seed-status \
  | grep -q '"world_seeded":true'; do sleep 1; done
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/status \
  -H 'Content-Type: application/json' -d '{"status":"active"}'
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/config \
  -H 'Content-Type: application/json' -d '{"key":"tick.daily_cadence","value":"* * * * *"}'
# then start season #1, per league (Step 7)
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/admin/worlds/$WORLD_ID/leagues/$LEAGUE_ID/season
```