# S06-01 — Implement transfer listings, bids, and human negotiation workflow

**Status:** Not started  
**Sprint:** 06 — Multiplayer market and board consequences  
**Source:** PRD §§20–22, 60; technical plan §§6, 12, 16; OPENCODE.md  
**Depends on:** S05-02

## What to do

Build the core transfer engine (`internal/transfer`) and schema (`transfer.transfer_listings`, `transfer.bids`, `transfer.negotiations`, `transfer.clauses`, `transfer.loans`). Implement listing players, submitting bids, a human-to-human negotiation state machine (accept, reject, counter-offer), bid expiration on deadlines, and basic AI-club counterpart response behavior. Ensure all completed transfers emit `TRANSFER_COMPLETED` events with explanation breakdowns and record financial movements as ledger entries in `finance.ledger_entries`.

## Acceptance criteria

- `transfer` schema tables (`transfer_listings`, `bids`, `negotiations`, `clauses`, `loans`) are created and carry `world_id`.
- Managers can list players for transfer or loan, set asking prices, and browse market listings filtered by position, age, and asking price.
- Buying managers can submit cash bids or structured bids (including sell-on clauses, instalment payments, or performance add-ons).
- Selling managers receive realtime notifications/dashboard items for incoming bids and can accept, reject, or counter within configurable expiration windows.
- Accepting a bid initiates player contract negotiations; upon contract agreement, ownership transfers, player contract updates, and financial ledger entries are appended atomically.
- Basic AI-club counterpart policy responds to bids on AI players based on player valuation and club DNA parameters.
- Every transfer action emits typed `world.events` (`TRANSFER_LISTED`, `TRANSFER_BID_SUBMITTED`, `TRANSFER_COUNTERED`, `TRANSFER_COMPLETED`, `TRANSFER_CANCELLED`) containing `Explanation` objects detailing market valuation and negotiation rationale.

## Delivery evidence

- Pending.
