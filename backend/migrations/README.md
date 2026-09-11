# Touchline database migrations

This folder is `backend/migrations` in the repo layout from the implementation
plan. It contains the full Touchline schema (14 domain schemas plus the `river`
job-queue schema, ~60 tables)
split into ordered, reversible migration files, one pair per schema.

Every `up`/`down` pair in this folder was generated from, and tested against,
the same schema in `touchline_schema.sql` — applying all `.up.sql` files in
order produces the identical database as running that file directly. Both the
full up-sequence and a full reverse down-sequence (and a re-up afterward) have
been run against a real Postgres 16 instance with no errors.

## Tooling: golang-migrate

These files use the naming convention expected by
[`golang-migrate`](https://github.com/golang-migrate/migrate):

```
{version}_{description}.up.sql
{version}_{description}.down.sql
```

Install it:

```bash
# macOS
brew install golang-migrate

# Linux (or use the Go install method below)
curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xz
sudo mv migrate /usr/local/bin/migrate

# via Go
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

`atlas` is a fine alternative if the team prefers a declarative diff-based
tool instead — these `.sql` files are plain, standard DDL and can be fed to
`atlas migrate import` as a starting point if you switch later.

## Connection string

Per the current infra decision, local development points at **Neon**
(serverless Postgres), not a local Postgres instance. Get your connection
string from the Neon dashboard — it looks like:

```
postgres://<user>:<password>@<endpoint>.neon.tech/<database>?sslmode=require
```

Export it once per shell session (or put it in a local `.env` that's
git-ignored):

```bash
export TOUCHLINE_DB_URL="postgres://<user>:<password>@<endpoint>.neon.tech/touchline?sslmode=require"
```

## Running migrations

From `backend/migrations`:

```bash
# Apply every migration that hasn't run yet, in order
migrate -database "$TOUCHLINE_DB_URL" -path . up

# Roll back the single most recent migration
migrate -database "$TOUCHLINE_DB_URL" -path . down 1

# Roll back everything (full teardown — careful in shared environments)
migrate -database "$TOUCHLINE_DB_URL" -path . down -all

# Jump straight to a specific version, e.g. after a fresh clone
migrate -database "$TOUCHLINE_DB_URL" -path . goto 14

# Check which version the database is currently at
migrate -database "$TOUCHLINE_DB_URL" -path . version
```

If a migration fails partway through, `golang-migrate` marks the schema
`dirty` and refuses to run further migrations until you fix the underlying
issue and run `migrate ... force <version>` to clear the dirty flag. Since
every migration in this set has already been verified end-to-end, a failure
on a clean Neon database most likely means the target database isn't actually
empty — check `migrate ... version` first.

## Migration order and what's in each file

Order matters: later files add foreign keys that point at tables created by
earlier ones (e.g. `manager` adds the FK from `club.clubs.current_manager_id`
to `manager.managers`, since `manager.managers` doesn't exist until its own
migration runs). Don't reorder these or run them out of sequence.

| # | Migration | Adds |
|---|---|---|
| 0000 | `extensions` | `pgcrypto` (for `gen_random_uuid()`) |
| 0001 | `auth` | User accounts, sessions |
| 0002 | `ref` | Static nationality/name-pool reference data used by player generation |
| 0003 | `world` | World lifecycle, tick config, the event log, news stories |
| 0004 | `person` | Shared identity for anyone who can hold a role (player, manager, later: coach/scout/agent) |
| 0005 | `club` | Clubs, Club DNA, boards, mandates, facilities, academies, supporters, history, rivalries, ownership |
| 0006 | `player` | Players, attributes, hidden traits, personality, preferences, emotional states, contracts, history, injuries |
| 0007 | `manager` | Managers, the one-active-club-at-a-time constraint, reputation log, career history, job-security snapshots |
| 0008 | `transfer` | Listings, bids, negotiations, completed transfers, clauses, loans |
| 0009 | `finance` | Ledger-based accounts, budgets, wage commitments, financial crisis states |
| 0010 | `social` | Relationship graph, messages, promises, trust events |
| 0011 | `match` | Fixtures, matches, match events |
| 0012 | `competition` | Competitions, rules, seasons, entries, standings |
| 0013 | `notification` | Per-category, per-channel notification preferences and dispatch log |
| 0014 | `moderation` | Anti-abuse flags and device fingerprints |
| 0016–0022 | `river` (event bus) | River v0.44.0 job-queue schema, exported one migration per river version |

### River migrations (0016–0022)

Migrations 0016 through 0022 are the **River** job-queue schema (used by the
event bus in `pkg/eventbus`), not hand-written — they are exported from
`github.com/riverqueue/river` (driver `riverpgxv5`) **v0.44.0**, one
golang-migrate pair per River's own versioned migrations.

- The exported SQL runs in the `river` schema: each file begins with
  `SET search_path TO river;` and River's `/* TEMPLATE: schema */` placeholder
  has been resolved to the qualified name `river.`.
- `0016_river_migration.up.sql` also creates the `river` schema; its `.down.sql`
  drops it (`DROP SCHEMA river CASCADE`), mirroring the per-schema pattern above.
- River's own `river_migration` bookkeeping table is created but not populated —
  golang-migrate's `schema_migrations` is the source of truth.
- **On a River upgrade:** re-export the new driver's migration files. Pin with:

  ```bash
  go mod download github.com/riverqueue/river/riverdriver/riverpgxv5@v0.44.0
  ```

  then diff `$(go env GOMODCACHE)/github.com/riverqueue/river/riverdriver/riverpgxv5@<version>/migration/main/*.sql`
  against the files here, add/remove versions as needed (keep the per-version
  numbering strictly increasing), and re-verify with the CI `migrations` job.
  Keep the "Migration exported from …" header comment in sync first.

Each `.down.sql` does `DROP SCHEMA <name> CASCADE`, which also removes any
foreign keys later migrations added pointing into that schema — this only
works correctly if downs are applied in reverse numeric order (which is what
`migrate down` does automatically).

## Before you can insert players or managers

`player.players` and `person.people` both have `NOT NULL` foreign keys into
`ref.nationalities`, and `person.people` needs at least one row there before
anything else in the world can reference a nationality. In practice this
means: **seed `ref.nationalities` and `ref.name_pool` before generating any
world content.** That seed data isn't part of these migrations on purpose —
it's sourced/curated data (per implementation plan section 7), not schema, so
it belongs in a separate seed script (e.g. `backend/migrations/seed/` or a
`cmd/seed` binary) rather than a versioned migration.

## Adding a new migration

```bash
migrate create -ext sql -dir . -seq add_something_descriptive
```

This creates the next-numbered `.up.sql`/`.down.sql` pair. Keep each
migration scoped to one schema or one clear change, mirroring the pattern
above, and always write the `.down.sql` before committing — every migration
here was verified to roll back cleanly, and new ones should be too.

## CI/CD

Per the implementation plan, migrations run as a Helm pre-install/pre-upgrade
hook against whichever environment is being deployed. Locally and in CI
against a throwaway database, the safe verification loop is:

```bash
migrate -database "$TOUCHLINE_DB_URL" -path . up
migrate -database "$TOUCHLINE_DB_URL" -path . down -all
migrate -database "$TOUCHLINE_DB_URL" -path . up
```

If all three steps succeed, the migration set is internally consistent.
