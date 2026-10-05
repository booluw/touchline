# Chapter 28 — The admin console

Admins (`auth.users.is_admin`) run the global console: they create and shape
worlds, monitor countries, and intervene. Every route is under `/api/admin/*`
behind `requireAuth` + `requireAdmin`; admin sessions are **world-less** — the
world and country come from the URL.

**Every admin configuration change records a typed `world.events` row in the
same transaction** (system actor, IM27/OPD-52): `WORLD_CONFIG_CHANGED`,
`COUNTRY_CREATED`, `LEAGUE_CREATED`, `LEAGUE_ADJACENCY_SET`,
`LEAGUE_REPUTATION_SET`, `REGION_CREATED`, `REGION_DELETED`,
`COUNTRY_REGION_SET`, `CUP_CREATED`, `CUP_QUALIFICATION_SET`,
`CUP_FINAL_DATE_POLICY_SET` (plus `CLUB_JOINED_LEAGUE`,
`LEAGUE_CAPACITY_CHANGED`). No news or realtime push is added for them.

## 28.1 Write surface (by chapter)

| Area | Routes | Chapter |
| --- | --- | --- |
| Worlds | `POST /admin/worlds`, `POST /admin/worlds/:id/status`, `POST /admin/worlds/:id/config` | [5](05-worlds-and-setup.md), [4](04-world-clock-and-time.md) |
| Seeding | `POST /admin/worlds/:id/seed`, `GET …/seed-status` | [5](05-worlds-and-setup.md) |
| Geography | `POST /admin/countries`, regions CRUD, country↔region | [5](05-worlds-and-setup.md) |
| Leagues | `POST /admin/leagues`, `PATCH …/adjacency`, `PATCH …/capacity`, `PATCH …/scheduling`, league reputation | [6](06-leagues-and-scheduling.md) |
| Membership | `POST /admin/worlds/:id/countries/:countryID/clubs/:clubID/league` | [7](07-seasons-and-rollover.md) |
| Country scheduling | `PATCH /admin/worlds/:id/countries/:countryID/scheduling` | [6](06-leagues-and-scheduling.md) |
| Seasons | `POST /admin/worlds/:id/leagues/:leagueID/season` | [7](07-seasons-and-rollover.md) |
| Cups | `POST /admin/cups`, `POST /admin/cups/preview`, `PATCH …/qualification`, `…/scheduling`, `…/final-date`, campaign start | [8](08-cups.md) |
| Offers | `POST /admin/offers` | [22](22-managers-and-job-offers.md) |
| Players | `POST /admin/worlds/:id/players/bulk` | [11](11-player-lifecycle-and-academy.md) |
| Renames | club/competition renames (`internal/admin/rename.go`) | — |

## 28.2 Competition dossier (IM15, OPD-41)

`GET /api/admin/competitions/:id/detail` returns one `CompetitionDetail` for a
league **or** cup: shared base (scope, tier, team count, reputation, seasons
total), history (`past_winners` — standings leader for leagues, `champion`
entry for cups), top scorers (current season and all-time top 10 from
`match.match_events`, penalties included), and type-specific detail.

## 28.3 The country dashboard (read-only)

Under `/api/admin/worlds/{id}/countries/{countryID}/…`. A country is resolved
through three explicit lenses:

| Lens | Key |
| --- | --- |
| players / population | `player.players.country_id` (origin pool, hard FK) |
| leagues / pyramid | `competition.competitions.country_id` |
| clubs / finance / transfers | `club.clubs.country` **text** matched to `world.countries.name` (best-effort) |

Two buckets are always surfaced: **world-pool players** (`country_id IS NULL`)
and **orphan clubs** (country text matches nothing).

| Pane | Shows |
| --- | --- |
| `overview` | population split, clubs (+ crisis subset), leagues by tier, open listings/bids, wage bill, budgets, cash, unassigned buckets; headlines (top fees 90 d, worst crisis, latest intake) |
| `pyramid` | leagues by tier with P/R quotas, sizes, latest season |
| `clubs` | per club: league, squad size, finances, crisis stage, top player by value |
| `players` | pool population: supply vs demand, intake history, nationality mix, per-position average overall + potential |
| `free-agents` | paged pool with position/age/nationality filters |
| `market?days=90` | listings, bids received/made, completed transfers; `overpay` flag when fee > market value (window ≤ 365) |
| `finance` | crisis clubs, wage bills, budget capacity and utilisation |
| `timeline?days=14&limit=200` | country-scoped events with resolved titles (limit ≤ 500) |

Money is summed from the ledger; budgets are capacity. A country outside the
world → `404 country not found in world`.

## 28.4 Using manager routes as an admin

Manager-scoped routes return `403 "no world context"` for an admin session.
Give the admin a `manager.managers` row in that world (setup guide Step 4) to
use them.

## Connections

- Code: `internal/admin/{service,clubs,finance,market,players,pyramid,timeline,rename}.go`, `internal/httpapi/admin_*_handlers.go`.
- Source: `backend/docs/admin/how-to.md`, OPD-41, OPD-52, IM15, IM27.

---
[← Accounts & auth](27-accounts-and-auth.md) · [Contents](the-touchline-book.md) · [Next: Engineering workflow →](29-engineering-workflow.md)
