# Transfer Numerics (S06-01)

Source of truth for the implemented numbers behind the transfer market:
valuation, AI club buy/sell policy, and bid lifetime. The product task
(`docs/tasks/S06-01-transfers-listings-bids-and-negotiation.md`, PRD §§20–22,
technical plan §16) publishes intent; this document fixes the implemented
values and binds them to the `transfer.*` schema (shipped unused since 0008).
All numbers here are **proposal** data until PM tuning sign-off — every block
is config expressed as constants in `internal/transfer` (never freeform in
engine code), like `squad.DefaultPositionWeights`.

Money is **pence** (`int64`) at the service boundary and `NUMERIC(14,2)`
pounds in `finance.*` columns.

## 1. Market value (fills `player.players.market_value`)

The "never written" valuation gap this slice closes. Recomputed for every
active, contracted player on the **daily** world tick (`RecomputeValuations`),
one `MARKET_VALUATIONS_REFRESHED` system event per world with changes.

```
valuation = round10k( overall³ × 0.08 × positionMult × ageFactor × contractFactor )
```

- `overall` = mean of the player's `attack` and `defence` ratings from
  `squad.BuildSquadRatings` over a single-member XI (the same recipe matchsim
  consumes).
- `positionMult` (`PositionMarketMultiplier`): GK 0.90 · CB 1.00 · LB/RB 0.85 ·
  DM 1.00 · CM 1.00 · AM 1.05 · LM/RM 0.90 · ST 1.05 · LW/RW 1.00. Unknown
  positions fall back to 0.90.
- `ageFactor` (`ageFactor`): ≤17 → 0.80; 18–23 → 0.85 rising to 1.00; 24–28 →
  1.00 (peak); 29–35 → −0.05/yr; >35 → 0.65 − 0.03/yr.
- `contractFactor` (`contractFactor`): `daysLeft ≤ 45` → 0.50 (expiring deal);
  else `0.50 + clamp01(daysLeft / (365×4)) × 0.60`, capped at 1.10.
- `round10k` rounds to the nearest £10,000 so identical situations always agree
  and AI anchoring is stable.

Properties (unit-testable): monotonic in `overall`, a typical top-flight first
XI (`overall ≈ 75`) values around £8–35M depending on position/age/contract.

## 2. AI counterpart policy

Both directions are covered: an **AI seller** receives human bids; an **AI
buyer** counters a human seller's counter-proposal. All decisions are
deterministic (unit-tested), seeded so replays converge.

### 2.1 AI seller (reaction to a bid on its player)

- `target = max(asking_price, valuation × 1.10)` — what the AI holds out for.
- fee ≥ target → **accept** (transfer completes in-transaction).
- else fee ≥ `valuation × 0.90` → **counter** at `target`.
- else → **reject**.

### 2.2 AI buyer (proactive bids + reaction to a counter)

Proactive bids (on listing and on the daily sweep):
- Fee sampled deterministically per `(listingID, worldTick)`:
  `seed = fnv64(listingID) ^ (7919 × worldTick)`, uniform in
  `[valuation × 0.75, valuation × 0.95]`, never above the asking price.
- The AI will pay at most `valuation × 0.95` (`aiBuyMaxMultiple`).
- Funds guard: AI only bids when club `cash ≥ fee × 1.25` (`aiBuyFundsMargin`).
- Shortlist: the **top-K = 2** most interested AI clubs
  (`aiInterestClubs`), ranked by thinnest positional squad first (club id as
  the stable tie-break) — so the market never stacks bids from every club.
- A listing's guard: AI bids are never placed when an open bid already exists.

Reacting to a human counter (AI is the buyer):
- fee ≤ `valuation × 0.95` → **accept**.
- else fee ≤ `valuation × 1.10` → **counter** at `valuation × 0.95`.
- else → **reject**.

### 2.2.1 Round limit

After `aiMaxRounds = 3` negotiation rounds the AI **rejects** rather than
countering again.

### 2.3 AI contract tail on proactive bids

`weekly_wage` defaults to the player's **current wage** (`Wage`), falling back
to `defaultAIDealWage = 20,000`; `contract_length_months = 24`; no signing
bonus/clauses.

## 3. Bid lifetime (OPD-05 resolution)

- `BidTTLWorldDays = 3`. An open bid (`pending` or `countered`) that goes
  unanswered past the wall-clock deadline is expired:
  - lazily, on the next `respond` attempt (`bidExpired`), and
  - by the **daily** sweep (`ExpireStale`, SQL `created_at < now() - interval`),
    which emits one `BID_EXPIRED` system event per world with a pending change.
- Withdrawing a listing expired its open bids immediately; completing a transfer
  expires every competing open bid on the player.
- Listings have **no expiry** (a manager may leave one up indefinitely).

## 4. Completion transaction (the atomic ownership flip)

`acceptBid` runs entirely in the caller's transaction, in order:

1. Relock the bid + decode the latest (agreed) terms; re-run `validateTerms`.
2. Verify buyer cash ≥ fee (account may be minted for seeded clubs).
3. `player.players`: flip `club_id` to the buyer.
4. Terminate the seller's active contracts; end their `finance.wage_commitments` today.
5. Insert the buyer contract + wage commitment from the agreed terms
   (`end = today + contract_length_months`).
6. Emit `BID_ACCEPTED` (*related_event_id* captured) with an `Explanation`.
7. Insert `transfer.completed_transfers` (`related_event_id` set).
8. Post ledger, **dedup-keyed**: debit `transfer_fee` on the buyer account,
   credit `player_sale` on the seller account, keys `transfer:<ctID>:buyer` /
   `transfer:<ctID>:seller` — idempotent under redelivery.
9. Capture clauses (`sell_on` %, `buy_back` £ for the seller) into `transfer.clauses`.
10. Append `player.player_history` row (event_type `transfer`) on the buyer.
11. Close the player's active listings; expire competing open bids; mark the
    bid `accepted`.

## 5. Accounting terms

| Ledger row | Entry | Category | Direction |
| --- | --- | --- | --- |
| Buying club | `transfer_fee` | `TRANSFER_OUT` on the ledger read model | debit |
| Selling club | `player_sale` | `TRANSFER_IN` on the ledger read model | credit |

`finance.FutureInstallments` already reads `transfer.completed_transfers.installments`
(JSONB); S06-01 writes **no** installments (all cash up front). Clauses capture
future obligation shapes (sell-on/buy-back) without monetising them yet.

## 6. Events

Actor types: `manager` / `policy_bot` (from the acting `manager.managers` row
— AI clubs act through their policy-bot manager) / `system` (sweeps).

| Event | Actor | Notes |
| --- | --- | --- |
| `PLAYER_LISTED` | manager | listing created |
| `PLAYER_LISTING_WITHDRAWN` | manager | listing withdrawn |
| `BID_PLACED` | manager / policy_bot | incl. AI buyer proactive bids |
| `BID_ACCEPTED` | manager / policy_bot | explanation: `transfer_value` (market value + bid fee) |
| `BID_REJECTED` | manager / policy_bot | |
| `BID_COUNTERED` | manager / policy_bot | |
| `BID_WITHDRAWN` | manager | buyer abandons |
| `BID_EXPIRED` | system | daily sweep / listing withdraw / completion cascade |
| `TRANSFER_COMPLETED` | (via completion) | persisted with the completed-transfer record |
| `MARKET_VALUATIONS_REFRESHED` | system | daily sweep, only when valuations changed |

## 7. Migrations

- `0038`: integrity guards on the dormant tables — partial unique
  `idx_listings_active_player (player_id) WHERE status = 'active'`; partial
  `idx_bids_open (status, created_at) WHERE status IN ('pending','countered')`
  for the expiry sweep; unique `idx_completed_bid (bid_id)` (a thread completes
  at most once).