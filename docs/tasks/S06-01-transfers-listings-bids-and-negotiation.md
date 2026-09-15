# S06-01 — Implement transfer listings, bids, and human negotiation workflow

**Status:** In progress  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§20–22, 60; technical plan §§6, 12, 16; OPENCODE.md  
**Depends on:** S05-02

## What to do

Build the core transfer engine (`internal/transfer`) on the existing
`transfer.*` schema (shipped unused since 0008): listings, bid threads with
counter-negotiation rounds, bid expiration, a deterministic AI counterpart
policy (AI clubs both sell and buy), and the atomic completion that moves
player/contract/ledger/history/events in one transaction. Completed transfers
emit `TRANSFER_COMPLETED` events with explanation breakdowns and post ledger
entries (debit `transfer_fee` / credit `player_sale`) in `finance.ledger_entries`.

## Decisions (recorded)

- **Terms ride with the bid** (S10-01 agent negotiations stay out of scope): every
  offer carries the full contract tail — fee, weekly wage, length, signing bonus,
  release clause, sell-on %, buy-back — and is stored as `transfer.negotiations.terms`
  JSONB. Accepting the other side's proposal binds those terms.
- **AI clubs are active buyers** — top-K (`aiInterestClubs = 2`) AI clubs bid on
  human listings, both immediately on listing and during the daily sweep for
  unsold listings.
- **Loans: listing only.** `listing_type = loan_available` is marketable; the loan
  lifecycle (recall, wage split, convert to permanent) is a later slice.
- Bid TTL is **3 world days** (OPD-05 resolution); listings have no expiry.
- Unanswered-bid expiry, valuation refresh (`player.players.market_value`), and AI
  bid activity run on the **daily** world tick.
- Integration tests cannot run locally (no Postgres/Docker); compile verified via
  `go vet -tags integration ./...`.

## Acceptance criteria — state

- ✅ `transfer` schema carries `world_id`; guards added (`0038` migration).
- ✅ Managers can list players (open-to-offers / actively-shopped / loan-available),
  set asking prices, and browse market listings (filtered by status, position,
  listing type, club).
- ⏳ Structured bids with sell-on/buy-back are implemented at the API/terms level;
  **instalment payments and performance add-ons are not yet modelled** (deferred —
  `transfer.completed_transfers.installments` is forward-compatible).
- ✅ Buying managers submit cash+contract bids; selling managers accept, reject, or
  counter within the 3-day window; buyers may also accept/reject.
- ✅ Accepting a bid completes ownership transfer, contract swap, wage-commitment
  end/start, ledger entries, clause capture, and history row **atomically**.
- ✅ Basic AI counterpart policy responds to bids on AI players based on valuation
  (accept/counter/reject thresholds) and shortlist interest.
- ✅ Every action emits typed `world.events` (`PLAYER_LISTED`, `PLAYER_LISTING_WITHDRAWN`,
  `BID_PLACED`, `BID_ACCEPTED`, `BID_REJECTED`, `BID_COUNTERED`, `BID_WITHDRAWN`,
  `BID_EXPIRED`, `TRANSFER_COMPLETED`, `MARKET_VALUATIONS_REFRESHED`) with
  `Explanation` objects (market valuation + negotiation rationale) where the domain
  has one.
- ⏳ Realtime dashboard notifications for incoming bids (S07-01 aggregator surface).

## Implementation state

Done: `internal/transfer` (model/valuation/store/service), `0038` integrity
migration, httpapi routes + `transfer_handlers.go` (7 endpoints), openapi.yaml
spec, daily-tick wiring in `internal/app/app.go`, design numerics doc.
Pending: integration test suite (compile-checked), final verification.

## Delivery evidence

- Pending (integration tests).