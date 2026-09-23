# How to: the transfer market

How the transfer market works, when it is open, how long bids live, and the
full route surface. The short version of the headline question:

> **There is no transfer window.** The market runs **continuously** from the
> moment the world is playable. Listings have no expiry; an unanswered bid
> expires after **3 world days**; a dormant transfer request auto-lists the
> player after **21 world days**.

Relates to: [cadences-and-time.md](cadences-and-time.md) (daily market
maintenance), [glossary.md](glossary.md) (valuation + accounting terms),
[policybot-numerics](../design/policybot-numerics.md) (delegated seller
responses).

---

## 1. Market model

The market is a thin layer over listings and bid threads:

- **Listing** (`transfer.listings`) — one active listing per player
  (`idx_listings_active_player`), status `active` (withdrawn closes it).
  Listings have **no expiry**; a manager may leave one up indefinitely.
- **Bid** (`transfer.bids`) — a thread attached to a listing. Statuses:
  `pending` → `accepted | rejected | countered | withdrawn | expired`.
  Every listing has one open thread at a time, one open `pending`/`countered`
  bid per listing max.
- **Completion** — accepting a bid performs the atomic ownership flip
  (see §5), which also closes the player's other listings and expires all
  competing open bids on that player.

## 2. When it is open

There is **no seasonal gate written into the engine**. As long as a world is
playable (not paused/archived), listings can be created, bids placed, and
transfers completed. The daily tick performs market maintenance regardless of
any date (see §6) — nothing in the codebase toggles the market on/off per
season or month.

## 3. How listings come into existence

| Path | Trigger |
| --- | --- |
| Manual | Club manager `POST /api/transfers/listings` (requires an active club). |
| Transfer request approved | `POST /api/clubs/:id/players/:playerID/transfer-request/approve` opens the player at **market value** as `open_to_offers`. |
| Auto-list | An unaddressed transfer request is auto-listed after **21 world days** (`AutoListTTLWorldDays` from `internal/player/numerics.go`), status `auto_listed`. |

## 4. Bid lifetime (the TTL)

`BidTTLWorldDays = 3` (`internal/transfer/valuation.go`, OPD-05 resolution).
An open bid (`pending` or `countered`) that goes **unanswered** past the
wall-clock deadline is expired:

- lazily, the next time someone tries to `respond` to it
  (`bidExpired` → `ErrBidExpired`), and
- eagerly, by the **daily sweep** (`ExpireStale`, SQL
  `created_at < now() − interval`), which emits one `BID_EXPIRED` system event
  per world with a change.

Cascades: withdrawing a listing expires its open bids immediately; completing
a transfer expires every competing open bid on the player.

## 5. What a transfer completion actually does (atomic)

`acceptBid` runs inside the caller's transaction, in order
(`internal/transfer/service.go`, doc'd in
[transfer-numerics](../design/transfer-numerics.md) §4):

1. Relock the bid + latest agreed terms; re-validate.
2. Verify the **buyer has cash ≥ fee**.
3. Flip `player.players.club_id` to the buyer.
4. Terminate the seller's active contracts + wage commitments (end today).
5. Insert the buyer's contract + wage commitment from the agreed terms (`end =
   today + contract_length_months`).
6. Emit `BID_ACCEPTED` (with explanation).
7. Record `transfer.completed_transfers`.
8. Post ledger, **dedup-keyed**: debit `transfer_fee` on the buyer, credit
   `player_sale` on the seller (`transfer:<completionID>:buyer|seller`).
9. Capture clauses (`sell_on`, `buy_back`) into `transfer.clauses`.
10. Append a `player_history` row on the buyer.
11. Close the player's active listings; expire competing bids; mark the bid
    `accepted`.

`OnPlayerTransferred` (player hook, landed atomically): fresh-start morale
0.85, playing-time share reset, cooldown cleared, any open request →
`withdrawn`. `OnPlayerSold` (faction hook) settles dressing-room relationships
against the pre-sale squad.

## 6. The daily tick and the market

Each **daily** `WORLD_TICK`, in order (`internal/app/app.go`):

1. `Policy.RespondToBidsForAbsent` — the PolicyBot answers pending bids on
   away-managed clubs' players **before** the sweep (so a transferred player
   can't be "sold out from under" an absent manager's back-and-forth).
2. `Transfers.DailyTick`:
   - `ExpireStale` — expire bids past the 3-day TTL.
   - `RecomputeValuations` — refresh `player.players.market_value` for every
     active, contracted player (one `MARKET_VALUATIONS_REFRESHED` event on
     change). See the valuation formula in the glossary.
   - `AIBidActivity` — AI clubs place proactive bids per their policy (§7).

## 7. AI counterpart policy (deterministic)

- **AI seller** reacting to your bid (`target = max(asking, valuation × 1.10)`):
  fee ≥ target → accept; fee ≥ `valuation × 0.90` → counter at `target`;
  otherwise → reject.
- **AI buyer** bidding proactively: fee sampled deterministically per
  `(listingID, worldTick)` uniform in `[valuation × 0.75, valuation × 0.95]`,
  never above asking; pays at most `valuation × 0.95`; only bids when
  `cash ≥ fee × 1.25`; the top-2 AI clubs by squad thinness shortlist; never
  bids while an open bid exists.
- AI reacts to your counter: fee ≤ `valuation × 0.95` → accept; ≤
  `valuation × 1.10` → counter at `valuation × 0.95`; else reject. After
  **3 rounds** it rejects.
- AI contract tail on proactive wins: current wage (fallback £20k/week), 24
  months, no bonus/clauses.

All of this is tunable constant data in `internal/transfer` — recalibration is
a data edit, never engine logic.

## 8. HTTP surface (world-scoped to your manager)

| Route | Meaning |
| --- | --- |
| `POST /api/transfers/listings` | create a listing |
| `GET /api/transfers/listings` | list (default `active`; filters: status, position, listing_type, selling club) |
| `GET /api/transfers/listings/:id` | one listing + open bids |
| `POST /api/transfers/listings/:id/withdraw` | withdraw it |
| `POST /api/transfers/bids` | place a bid |
| `GET /api/transfers/bids` | incoming + outgoing bid threads |
| `POST /api/transfers/bids/:id/respond` | accept / reject / counter (with terms) |

Transient errors map to HTTP statuses via `transferStatus`:
404s for missing/foreign entities; 403 for non-participants/self-bids; 400 for
invalid terms/duplicates/non-transferable players; 409 for insufficient funds,
resolved bids, or expired bids.

Example — list the market, place a bid, respond:

```bash
# list active listings in your world
curl -c /tmp/jar -b /tmp/jar localhost:8080/api/transfers/listings

# bid on a listing
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/transfers/bids \
  -H 'Content-Type: application/json' \
  -d '{"listing_id":"<id>","fee":7000000,"terms":{"weekly_wage":15000,"contract_length_months":36}}'

# answer an inbound bid
curl -c /tmp/jar -b /tmp/jar -X POST localhost:8080/api/transfers/bids/<bid-id>/respond \
  -H 'Content-Type: application/json' -d '{"action":"accept"}'
```

## 9. Common questions

**When does the transfer period open?** It is always open once the world is
playable. There is no calendar-based window.

**How long does a bid stay alive?** 3 world days from placement; the daily
sweep and the lazy guard both enforce it.

**Can a listing expire?** Listings themselves never expire; only stale bids
do.

**Who prices players?** The daily tick recomputes `market_value` (formula in
the glossary) — nobody hand-edits valuations.