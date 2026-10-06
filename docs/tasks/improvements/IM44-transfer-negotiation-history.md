# IM44 — Transfers: negotiation rounds on bids

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Transfers.dc.html`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S03 transfers

## What to do

1. `GET /api/transfers/bids` items add `negotiation` (rounds from `transfer.negotiations`: `round`, `proposed_by`, `terms`, `created_at`).

## Open questions (design needs data the engine does not model)

- Acceptance chance + factors for an offer, bidder interest score, "would go to £1.4m": no such score is exposed.
- Offer extras (payment schedule, add-ons, playing-time promise in a bid): bid `Terms` has fee, wage, length, bonus, release clause, sell-on, buy-back only.
- Max 4 rounds, counter expiry countdown: confirm engine limits before exposing.
- Transfer-window open date, fit score: not modelled.

## Recorded decisions

Only stored rounds are returned.

## Delivery evidence

### Files

internal/transfer/{model,store}.go (`NegotiationRound`, `attachRounds`), openapi Bid.rounds, transfer_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.
