# Chapter 21 — The transfer market

> **There is no transfer window.** The market runs continuously while a world is
> playable. Listings never expire; an unanswered bid expires after **3 world
> days**; an ignored transfer request auto-lists the player after **21 world
> days**.

## 21.1 Market value

Recomputed daily for every active, contracted player
(`RecomputeValuations`; one `MARKET_VALUATIONS_REFRESHED` event per world when
anything changed):

```
valuation = round10k( overall³ × 0.08 × positionMult × ageFactor × contractFactor )
```

| Term | Values |
| --- | --- |
| `overall` | mean attack/defence from the canonical recipe ([Ch. 10](10-players.md)) |
| `positionMult` | GK 0.90 · CB 1.00 · LB/RB 0.85 · DM 1.00 · CM 1.00 · AM 1.05 · LM/RM 0.90 · ST 1.05 · LW/RW 1.00 · unknown 0.90 |
| `ageFactor` | ≤ 17 → 0.80; 18–23 rises 0.85 → 1.00; 24–28 → 1.00; 29–35 −0.05/yr; > 35 → 0.65 − 0.03/yr |
| `contractFactor` | ≤ 45 days left → 0.50; else `0.50 + clamp01(daysLeft / 1460) × 0.60`, capped 1.10 |

`round10k` keeps identical situations identical. A typical top-flight XI player
(overall ≈ 75) is worth ~£8–35M.

## 21.2 Listings and bids

- **Listing** (`transfer.listings`): one active per player; status `active` or
  withdrawn. Created by a manager, by approving a transfer request (at market
  value, `open_to_offers`), or by auto-listing (`auto_listed`)
  ([Ch. 18](18-morale-and-transfer-requests.md)).
- **Bid** (`transfer.bids`): a negotiation **thread**; every counter appends a
  `transfer.negotiations` round and the latest round is the live offer.
  Statuses `pending → accepted | rejected | countered | withdrawn | expired`.
  One open bid per listing.
- **Terms ride with the bid** (OPD-05): fee, weekly wage, contract length,
  signing bonus, release clause, sell-on %, buy-back £. Valid clauses are
  `sell_on` and `buy_back` only.

### Bid TTL

`BidTTLWorldDays = 3` = 3 × `tick.day_length` of real time (IM26). Expired
lazily on the next `respond` (`ErrBidExpired`, saved first) and eagerly by the
daily sweep (`ExpireStale`, one `BID_EXPIRED` per world). Withdrawing a listing
expires its bids; completing a transfer expires competing bids.

## 21.3 AI counterparties (deterministic)

**AI seller** (reacting to your bid): `target = max(asking, valuation × 1.10)`.
Fee ≥ target → accept; ≥ `0.90 × valuation` → counter at target; else reject.

**AI buyer** (proactive, daily and on listing):

- fee sampled per `(listingID, worldTick)` — `seed = fnv64(listingID) ^ (7919 ×
  worldTick)` — uniform in `[0.75, 0.95] × valuation`, never above asking;
- pays at most `0.95 × valuation`; bids only with `cash ≥ 1.25 × fee`;
- shortlist = top **2** AI clubs by thinnest positional squad (id tie-break);
- never bids while an open bid exists;
- goes through **the same bid command as a manager** (`placeBidTx`, IM26) — a
  refused AI bid is skipped, not an error;
- contract tail: current wage (fallback £20k/week), 24 months, no extras.

**AI reacting to your counter** (AI is buyer): ≤ `0.95 × valuation` accept; ≤
`1.10 ×` counter at `0.95 ×`; else reject. After **3** rounds it rejects.

**Absent human seller**: PolicyBot accepts ≥ 120% of valuation, rejects < 100%,
otherwise counters at the accept threshold ([Ch. 25](25-policybot-and-absence.md)).

## 21.4 Completion — the atomic ownership flip

`acceptBid` (`internal/transfer/completion.go`) inside the caller's transaction:

1. relock bid + latest terms; re-validate;
2. buyer cash ≥ fee;
3. `player.players.club_id` → buyer;
4. end seller contracts & wage commitments on the **world date**;
5. buyer contract + wage commitment (`start = world date`, `end = start +
   contract_length_months`);
6. `BID_ACCEPTED` with `transfer_value` explanation (market value + premium =
   agreed fee, so it validates);
7. `transfer.completed_transfers`;
8. dedup-keyed ledger: debit `transfer_fee` (buyer), credit `player_sale`
   (seller);
9. clauses → `transfer.clauses`;
10. `player_history` row;
11. close listings, expire competing bids, mark accepted.

Hooks in the same transaction: `OnPlayerTransferred` (fresh-start morale 0.85,
share reset, request withdrawn) and `OnPlayerSold` (dressing-room
`former_teammate` edges against the pre-sale squad, [Ch. 19](19-dressing-room.md)).

## 21.5 The daily market sweep

Order on every daily tick: `Policy.RespondToBidsForAbsent` → `ExpireStale` →
`RecomputeValuations` → `AIBidActivity`.

## 21.6 Notifications

Every bid-thread event carries `buying_club_id` and `selling_club_id`; the
worker pushes the **urgent** dashboard section to the human manager on each
side (AI clubs skipped) (IM26, OPD-55).

## 21.7 API

| Route | Meaning |
| --- | --- |
| `POST /api/transfers/listings` | create listing |
| `GET /api/transfers/listings` | list (filters: status, position, type, club) |
| `GET /api/transfers/listings/:id` | listing + open bids |
| `POST /api/transfers/listings/:id/withdraw` | withdraw |
| `POST /api/transfers/bids` | place bid |
| `GET /api/transfers/bids` | incoming + outgoing threads |
| `POST /api/transfers/bids/:id/respond` | accept / reject / counter |
| free-agent signing | via `playerpool` ([Ch. 11](11-player-lifecycle-and-academy.md)) |

Errors: 404 missing/foreign; 403 non-participant/self-bid; 400 invalid terms /
duplicate / non-transferable; 409 funds, resolved or expired.

## 21.8 Events

`PLAYER_LISTED`, `PLAYER_LISTING_WITHDRAWN`, `BID_PLACED`, `BID_COUNTERED`,
`BID_ACCEPTED`, `BID_REJECTED`, `BID_WITHDRAWN`, `BID_EXPIRED`,
`TRANSFER_COMPLETED`, `MARKET_VALUATIONS_REFRESHED`.

## 21.9 Not built yet

Agents and player-side negotiation (S10-01), loans, instalments, release-clause
triggers, exchange deals, anti-abuse trade monitoring (S10-04).

## Connections

- Finance: [Chapter 20](20-finance.md). Requests: [Chapter 18](18-morale-and-transfer-requests.md).
- Code: `internal/transfer/{valuation,ai,bid,listing,completion,daily}.go`.
- Source: `docs/how-to/transfer-market.md`, `docs/design/transfer-numerics.md`, OPD-05, OPD-55, IM26.

---
[← Finance](20-finance.md) · [Contents](the-touchline-book.md) · [Next: Managers & job offers →](22-managers-and-job-offers.md)
