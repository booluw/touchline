# IM26 — Transfer market integrity pass

**Status:** Implemented
**Owner:** opencode agent
**Sprint:** Improvements (transfer market, closes S06-01 gaps)
**Source:** Doc-vs-code audit against `docs/design/transfer-numerics.md`,
S06-01 and the Tech Plan §10 PolicyBot rule; running the `internal/transfer`
integration suite against a real Postgres for the first time (S06-01's evidence
was "pending") surfaced further defects. Findings:
1. **Age curve inverted.** `ageFactor` gave 18 → 1.00 falling to 0.85 at 23; the
   design doc says 18–23 → 0.85 rising to 1.00.
2. **Bid notifications only for new bids, and only to the seller.** Counter,
   accept and reject payloads lacked `selling_club_id`, so the dashboard hook
   dropped them, and a buyer was never told of an answer.
3. **AI buyers bypassed the bid command.** `placeAIBid` wrote bids directly,
   skipping the duplicate-open-bid, funds, listing and transferability checks a
   manager's bid passes.
4. **Listing and bid reads were broken.** Both projections selected
   `p.display_name` with no `person.people` join — every listing/bid read failed.
5. **Bid expiry never ran.** `ExpireStale` passed an int into a text
   concatenation and failed; expiry also used real days, not world days.
6. **Responding to a stale bid lost the expiry.** The status change was rolled
   back with the error.
7. **Transfer explanations failed the contract.** Factor deltas did not sum to
   the score (`pkg/explanation.Validate`).
**Depends on:** S06-01, S07-01 dashboard push, IM16 clock scale.

## What to do

Fix each finding without changing market rules.

## Delivery evidence

### Backend

- `transfer/valuation.go` — `ageFactor`: `0.85 + 0.03*(age-18)` for 18–23.
- `transfer/bid.go` — new `placeBidTx` (the whole bid command inside the
  caller's tx); `PlaceBid` wraps it; stale-bid expiry is committed before
  `ErrBidExpired` is returned; withdrawals carry both club ids.
- `transfer/ai.go` — `placeAIBid` builds a `BidInput` and calls `placeBidTx`
  with the AI club's policy-bot actor; rule refusals (duplicate, funds, listing
  gone, not transferable, self) skip that club instead of failing the sweep.
- `transfer/daily.go` — `ExpireStale` uses `make_interval(secs => ttl)` with
  `bidTTL` = 3 × `tick.day_length`; `AIBidActivity` counts only placed bids.
- `transfer/helpers.go` — `bidClubs`, `bidTTL`; `completionExplanation`
  (score = fee = market value + premium) and `counterExplanation`
  (score = asked fee = valuation + premium) now validate.
- `transfer/completion.go` — reject/counter/accept payloads carry
  `buying_club_id` + `selling_club_id`.
- `transfer/store.go` — listing/bid projections join `person.people`.
- `app/worker.go` — `subscribeBidEvents` handles placed / countered /
  accepted / rejected / withdrawn and pushes the urgent dashboard section to
  **both** clubs' human managers.

### Tests

- `transfer/valuation_test.go` — curve spot values per the design doc, compared
  with a tolerance (the old `int(x*100)` truncation failed on 0.65).
- `transfer/service_integration_test.go` — new `TestBidEventsCarryBothClubs`,
  `TestAIBidsFollowTheBidCommandRules`; two test bugs fixed (`pickPlayer`
  returned the same player twice → `pickPlayerExcept`; a second bid on a player
  with an open countered bid now targets another player); `TestListingsAndBidsReads`
  expects 2 incoming bids (loan-available listings draw no AI buyers, per
  S06-01).
- The whole `internal/transfer` integration suite passes against Postgres 16.

### Verification

See [IM29](IM29-single-backend-image-deploy.md#verification).

## Recorded decisions

- **Bid TTL is three world days**, scaled by `tick.day_length` (OPD-05 said
  "world days"; the code measured real days).
- **AI buyers use the manager's bid command** (Tech Plan §10); a refused AI bid
  is skipped, not an error.
- **Both sides are notified** of every bid-thread event; `PushCategory` only
  pushes items new to that manager, so the acting side sees no noise.
- **Transfer explanations score the fee**; factors are market value plus the
  premium/discount paid over it.
