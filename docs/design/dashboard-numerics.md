# Dashboard numerics (S07-01)

All numbers that decide which items surface on the home dashboard are proposal
data until PM tuning sign-off. Recalibration means editing the constants in
`backend/internal/dashboard/model.go`, never steering logic.

## Expansion windows

| Constant | Value | Meaning |
| --- | --- | --- |
| `ContractExpiryWindowDays` | 30 | Active contract with `end_date` inside the next 30 days surfaces under **Urgent → contracts**. |
| `FixtureUrgencyWindow` | 48h | A still-`scheduled` fixture inside the next 48 hours surfaces under **Urgent → match**. |
| `BoardCriticalTotal` | 25 | Latest board `total_score` at or below 25 surfaces under **Urgent → board**. Mirrors `board.SackThresholdTotal`. |
| `BoardDropAttention` | −15 | Week-over-week `total_score` drop of 15+ points surfaces under **Important → board**. |
| `MoraleUnhappyThreshold` | 0.35 | Player morale at or below 0.35 surfaces under **Important → morale**. Mirrors `player.UnhappyMoraleThreshold`. |
| `MarketNewest` | 5 | Newest listing / withdrawn / completed-transfer world events surfaced under **Interesting → market**. |
| `MaxPerSection` | 12 | Cap per section so a noisy late-season feed stays skimmable. |

## Item IDs (stable dedupe keys)

Each item carries a stable `id` prefixing the primary key of its fact, so the
client can merge `dashboard_update` pushes with GET results without refetching:

- `bids:<bid_id>` — open bid thread where a managed club is the seller
- `contracts:<player_id>` — expiring contract
- `match:<fixture_id>` — imminent fixture
- `board:<club_id>` — critically low board confidence
- `board-drop:<club_id>` — sharp week-over-week confidence drop
- `cash:<club_id>` — negative cash position
- `finance:<club_id>` — unresolved financial-crisis state
- `morale:<player_id>` — unhappy player / open transfer request
- `standings:<club_id>` — active domestic-league position
- `rivals:<fixture_id>` — completed fixture between managed club and a rival
- `market:<event_id>` — world event (listing / withdrawal / completed transfer)

## Scope decisions

- **Read-only:** the dashboard is a pure aggregation over tables owned by
  transfer, finance, board, player, competition, social and match; it never
  writes game state. Postgres is authoritative.
- **Pushes are best-effort:** `dashboard_update` events carry only items not yet
  pushed for that manager+section in this process (dedupe key = item ID).
  Removals are not pushed — the client reconciles on the next GET, which stays
  authoritative. A skipped push only delays an update.
- **Push cadence:** the world-tick worker calls `PushWorldDelta` once per tick
  (after the daily/weekly/monthly passes), and the transfer bid-event subscriber
  calls `PushCategory` for the selling club's manager on `BID_PLACED`,
  `BID_COUNTERED`, `BID_ACCEPTED`, `BID_REJECTED`.
- **No migration:** every source table already exists; the aggregator reads
  them with existing projections (e.g. the transfer `bidSelect` join shape).
- **Multi-club managers:** item clubs are derived from
  `manager.managers.current_club_id` at request time (OPD-15); a club-less
  manager gets empty sections, not an error.
- **Out of scope for this slice:** scouting reports, media stories and injury
  surfaced items (their source events do not persist yet), plus any UI beyond
  the categorized card list.

## Wire format

- `DashboardItem`: `id`, `priority` (`urgent|important|interesting`),
  `category`, `title`, `description`, `created_at`, optional `action`.
- `DashboardAction` deep-link: `kind`
  (`respond_bid|renew_contract|set_lineup|view_board|view_finances|view_player|
  view_standings|view_fixture`) plus the scoping IDs (`club_id`, `player_id`,
  `fixture_id`, `bid_id`).
- `DashboardUpdatePayload` (realtime `dashboard_update`): `category`
  (`urgent|important|interesting`) + `items`.