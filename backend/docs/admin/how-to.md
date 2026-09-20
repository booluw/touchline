# Admin country dashboard — how to use it

The per-country admin dashboard is a **read-only** surface for monitoring and
drilling into a single world country. Every endpoint is admin-only
(`requireAuth` + `requireAdmin`) and lives under
`/api/admin/worlds/{id}/countries/{countryID}/…`.

The interactive spec (`/api/openapi.yaml`, rendered at `/api/docs`) is the
source of truth for request/response shapes. This doc explains what each pane
is for and how the numbers are derived.

## How a "country" is defined

One country pane is anchored on `world.countries` (the admin-declared
geography). Because of the way the schema links data to a country, the
dashboard resolves three separate keys and keeps them explicit:

| Lens | Table | Key |
| --- | --- | --- |
| Players / population | `player.players` | `country_id` (origin pool; hard FK, set at generation, never updated on sign/release) |
| Leagues / pyramid | `competition.competitions` | `country_id` (hard FK) |
| Clubs / finance / transfers | `club.clubs` | `country` **text name** matched to `world.countries.name` (best-effort; orphan clubs surface as unassigned) |

Nationality (`person.people.nationality_code` → `ref.nationalities`) is a
separate axis — `world.countries.code` is admin-defined and is allowed to
differ from ISO codes.

Two "nowhere to count" buckets are always surfaced, never hidden:

- **World pool players** — free agents with `country_id IS NULL` (the
  world-level bootstrap pool).
- **Orphan clubs** — clubs whose `club.clubs.country` text matches no
  `world.countries` row for their world.

## Endpoints and use cases

### GET `/api/admin/worlds/{id}/countries/{countryID}/overview`

The home pane — compact counters on every axis plus the top headlines.

- **Use cases:** the "is this country healthy" question. Population status
  split (active / free agent / retired), club count with the crisis subset,
  league count by tier, open listings and bids, wage bill & budgets, cash, and
  the unassigned buckets.
- **Headlines:** top fee transfers from the last 90 days (in/out/signing), the
  most advanced current financial crisis, and the latest youth intake.
- **Response:** `CountryOverview`.

### GET `/api/admin/worlds/{id}/countries/{countryID}/pyramid`

The country's leagues ordered by tier, each with its promotion/relegation
quota, registered club count, and latest season.

- **Use cases:** sanity-check league configuration (every tier present and
  sized as declared), confirm promotion/relegation quotas before a season
  rolls, and spot leagues with no season or no members.
- **Response:** `CountryPyramid` (rows `LeagueRow`, `season` optional).

### GET `/api/admin/worlds/{id}/countries/{countryID}/clubs`

One row per country club (name-matched) with league, squad size, finances, and
crisis state.

- **Use cases:** find clubs in trouble (active crisis stage), clubs with no
  league membership, bloated wage bills vs budget envelope, and the highest
  market-value player per club. Budget figures use the club's **latest season**
  per budget type, matching the wallet's `MAX(season)` rule.
- **Response:** `CountryClubsPanel` (`ClubRow`).

### GET `/api/admin/worlds/{id}/countries/{countryID}/players`

The country's origin-pool population, aggregated.

- **Use cases:** check the free-agent supply vs club demand, see the youth
  intake history per season, spot nationality imbalances, and get a
  per-position average overall (via the position-weighted recipe
  `squad.PositionalOverall`) with average potential.
- **Response:** `CountryPlayerSummary`.

### GET `/api/admin/worlds/{id}/countries/{countryID}/free-agents`

The country's free-agent pool, paged, with optional `position`, `age_min`,
`age_max`, and `nationality` filters.

- **Use cases:** the drill-down behind the players pane — who is actually
  available to sign, door-by-door, with rating and market value per player.
- **Response:** `AdminFreeAgentList` (reuses the manager-facing `FreeAgent`
  read model, minus world scoping since admins are world-less).

### GET `/api/admin/worlds/{id}/countries/{countryID}/market?days=90`

The transfer market drill-down.

- **Use cases:** rebalance/influence a market. Shows open listings for country
  clubs, bids **received** (selling club in the country) and **made** (bidding
  club in the country) as separate lists, and the completed-transfer ledger
  split into paid arrivals, departures, and free-agent signings over a rolling
  window (`days`, default 90, cap 365).
- **Fee-vs-value read:** every transfer row carries the player's current
  `market_value`; rows where fee > market value are flagged with `overpay` and
  an `overpay_pct`.
- **Response:** `CountryMarketPanel`.

### GET `/api/admin/worlds/{id}/countries/{countryID}/finance`

The country economy.

- **Use cases:** identify clubs in an active financial crisis (warning →
  bankruptcy), see the aggregate weekly wage bill and top wage bills, and the
  country-wide budget capacity (`allocated − committed`, utilization %) for
  both wage and transfer axes.
- **Response:** `CountryFinancePanel`.

### GET `/api/admin/worlds/{id}/countries/{countryID}/timeline?days=14&limit=200`

Recent world events scoped to the country.

- **Use cases:** the "what just happened here" audit trail. An event is
  in-scope when it references a country club, a player whose origin pool is
  this country (or who currently plays for a country club), or the country
  itself (youth intakes). Titles are built from resolved player/club names.
- **Response:** `CountryTimeline` (`TimelineItem` with `event_type`, `title`,
  and the raw `payload`).

## Access control

All endpoints sit under the `admin` group, so a session must be authenticated
and `auth.users.is_admin` must be true. Admins are world-less — the world and
country are taken from the URL path, not the session. A country that does not
belong to the given world returns `404 { "error": "country not found in world" }`.

## Notes on the SQL

- Club↔country is by best-effort **name match**, so a club whose `country`
  text is misspelled or unregistered counts as an orphan — fix it via admin
  country/league administration, not the dashboard (the dashboard is read-only).
- Money is summed from `finance.ledger_entries` (credits − debits) because the
  ledger is append-only and authoritative. Budgets are **capacity**, separate
  from cash.
- The `days`/`limit` window params are capped in the handler (365 / 500) so a
  fat-fingered query can't scan the whole world.