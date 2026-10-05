# Chapter 5 — Worlds: lifecycle, geography, setup and seeding

## 5.1 What a world is

A **world** is one self-contained game server run: its own clock
([Ch. 4](04-world-clock-and-time.md)), config, replay seed, geography, clubs,
players, managers and history. Many worlds can run in one database; nearly every
row is world-scoped. One user account can join many worlds (one
`manager.managers` row per world) but holds **at most one job globally**
([Ch. 27](27-accounts-and-auth.md)).

## 5.2 World lifecycle

| Status | Meaning | Ticks? |
| --- | --- | --- |
| `provisioning` | created, being declared/seeded | no |
| `active` / `open_beta` | **playable** | yes |
| `paused` | halted (resumable) | no |
| `archived` | terminal — nothing leaves it | no |

Transitions: `POST /api/admin/worlds/:id/status`. Config defaults
(`tick.*`, `calendar.*`, `season.*`) are seeded at launch. Offers can only be
issued and accepted in playable worlds ([Ch. 22](22-managers-and-job-offers.md)).

## 5.3 Geography: regions → countries → leagues

```
World ──► Region (optional, IM06) ──► Country ──► League (tier 1..n)
                                          └────► Domestic cup
Region ─────────────────────────────────────────► Regional (continental) cup
```

- **Country** (`world.countries`): admin-declared `code` + `name`. The code is
  matched case-insensitively against `ref.nationalities` slugs, so a `BR`
  country's intakes get Brazilian names. Countries also carry
  `default_scheduling_rules` (e.g. `allowed_weekdays`, [Ch. 6](06-leagues-and-scheduling.md)).
- **Region** (`world.regions`, IM06/OPD-30): a world-scoped grouping of
  countries; a country belongs to at most one region
  (`world.countries.region_id`). Deleting a region unassigns its countries — it
  never deletes them. Regions exist so **regional cups** have a scope
  ([Ch. 8](08-cups.md)).
- **League** ([Ch. 6](06-leagues-and-scheduling.md)): per country, a `tier`,
  an even `team_count ≥ 4`, optional promotion/relegation links.
- **League reputation** (0–100, `competition.competitions.reputation`): admin
  managed; drives the default continental qualification band. The match engine
  never reads it.

> **Club ↔ country is a text match.** `club.clubs.country` holds the country
> *name*; the admin dashboards match it against `world.countries.name`
> (best-effort). Players carry a hard `country_id` FK (their origin pool).

## 5.4 Reference data

`ref.nationalities` (with `generation_weight`) and `ref.name_pool` are
**world-independent** reference data (OPD-11, OPD-13), ingested idempotently by
`cmd/ref-seed` from curated `backend/data/names/<code>.json` files plus the
club-name corpus (`data/clubs`). Seeding fails with `ErrRefDataMissing` if they
are empty. Codes are lowercase slugs; `eng`/`sco` are project-reserved.

## 5.5 Launching a world end to end

The canonical sequence (full commands: `docs/how-to/setup-and-launch.md`):

| Step | Call | Effect |
| --- | --- | --- |
| 0 | `migrate … up`; `cmd/ref-seed` | schema + reference data |
| 1 | `cmd/user-create -admin` | first admin (world-less) |
| 2 | `POST /api/admin/worlds` | world in `provisioning` |
| 3 | `POST /api/admin/countries`, `POST /api/admin/leagues`, `PATCH …/adjacency` | declare geography (metadata only, OPD-20) |
| 4 | `POST /api/admin/worlds/:id/seed` → poll `…/seed-status` | async seed job fills every league with AI clubs + squads |
| 5 | `POST /api/admin/worlds/:id/leagues/:leagueID/season` per league | season #1 + fixtures ([Ch. 7](07-seasons-and-rollover.md)) |
| 5a | `POST /api/admin/cups` + campaign (optional) | cups ([Ch. 8](08-cups.md)) |
| 6 | `POST /api/admin/worlds/:id/status {"status":"active"}` | clock starts |
| 7 | `POST /api/auth/register` | humans join, get an auto-offer ([Ch. 22](22-managers-and-job-offers.md)) |

### Seeding in detail (OPD-18, OPD-22)

- **Asynchronous.** `POST /seed` validates synchronously (`404` unknown world,
  `422` archived / no leagues) then queues a `seed_world` River job and returns
  `202`. A worker must be running. `GET …/seed-status` reports
  `world_seeded`, league progress, club count, pool size and the job's state /
  `last_error`. Duplicate POSTs coalesce. The seed worker's timeout is raised to
  30 minutes (River's default 1-minute `JobTimeout` would kill large seeds).
- **All AI.** Every seeded club is `is_ai_controlled = true` with a PolicyBot
  manager row, a generated 24-player squad and a name drawn from the corpus.
  There is no human starter club.
- **Incremental + idempotent.** Each run fills leagues up to `team_count`,
  creating only missing clubs; re-runs report `new_clubs: 0`.
- **Replay seed.** The first successful run mints `world.worlds.random_seed`;
  per-league name scrambling uses `seed ⊕ leagueID`. A failed run rolls back and
  leaves it unset.
- **Pool first.** Each country's free-agent pool is topped up to
  **100** (`playerpool.PoolTargetSize`) *before* its first AI club drafts, and
  back up after every club ([Ch. 11](11-player-lifecycle-and-academy.md)).
- **No season side effects.** Seeding creates memberships only; fixtures come
  from `StartSeason`.

### What a generated club gets

`bootstrap.GenerateAIClub` (inside the seed transaction) creates:

- the club row, a **DNA** profile from a weighted archetype draw, and a **board
  persona** derived from it ([Ch. 9](09-clubs-dna-supporters.md));
- a supporter group with an identity and starting sentiment;
- a finance account with **$40M opening capital** and season budgets
  ([Ch. 20](20-finance.md));
- a drafted squad (`SquadSizeDefault = 24`: 2 GK / 7 DEF / 7 MID / 6 FWD + 2
  flexible) with contracts and wage commitments;
- a PolicyBot manager row; `CLUB_CREATED` event.

## 5.6 Growing a running pyramid (IM14)

Both controls bind at the **next** season — a live season is never edited:

- `POST /api/admin/worlds/:id/countries/:countryID/clubs/:clubID/league` adds a
  **league-less** club (declared member). Refused when the league is
  structurally full (`realSize + pending + 1 > team_count`).
- `PATCH /api/admin/leagues/:id/capacity` raises `team_count` (never lowers it,
  `ErrLeagueShrink`) and/or restates promotions/relegations; the neighbour's
  reciprocal count is auto-adjusted in the same transaction.

Details in [Chapter 7](07-seasons-and-rollover.md).

## 5.7 Troubleshooting quick table

| Symptom | Fix |
| --- | --- |
| `ErrRefDataMissing` | run `cmd/ref-seed` |
| `202` but nothing seeded | no worker running |
| seed `context deadline exceeded` | re-queue on current build (30-min seed timeout) |
| `free-agent pool has too few players` | re-queue; current build mints pools first |
| `403 no world context` on manager routes | admin sessions are world-less; give the admin a manager row |
| no ticks | world not `active`/`open_beta` |
| matches never start | no season started, or the clock hasn't reached `scheduled_at` |

## Connections

- Clock and config keys: [Chapter 4](04-world-clock-and-time.md).
- Admin surfaces for monitoring a world: [Chapter 28](28-admin-console.md).
- Source: `docs/how-to/setup-and-launch.md`, OPD-11, OPD-13, OPD-16, OPD-18, OPD-20, OPD-22, OPD-30, IM06, IM14.

---
[← World clock](04-world-clock-and-time.md) · [Contents](the-touchline-book.md) · [Next: Leagues & scheduling →](06-leagues-and-scheduling.md)
