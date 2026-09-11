# S10-04 — Implement anti-abuse trade monitoring and commissioner adjudication tooling

**Status:** Not started  
**Sprint:** 10 — Agents, competitions, identity, and ownership  
**Source:** PRD §§49–51; technical plan §§13, 16; OPENCODE.md  
**Depends on:** S06-01, S06-04

## What to do

Build the asynchronous trade collusion analyzer worker and commissioner admin panel. The worker listens to `TRANSFER_COMPLETED` events, evaluating suspicious patterns (e.g. transfers far above/below market valuation, repeated circular trades, rapid transfers between newly created accounts, IP/device co-location). Build commissioner adjudication views allowing league administrators to inspect flagged transactions, freeze suspicious accounts, or reverse collusive transfers.

## Acceptance criteria

- Trade monitoring worker asynchronously scores completed transfers against anomaly heuristics without blocking transfer execution.
- Suspicious trades generate audit flags with detailed heuristic explanations in `social.manager_trust_scores` and commissioner queues.
- Multi-account detection triggers on device fingerprint clusters and shared network subnet patterns.
- Commissioner admin interface allows authorized administrators to inspect event trails, freeze accounts, issue warnings, or void illicit transfers.
- Voiding a transfer emits a `TRANSFER_VOIDED_BY_COMMISSIONER` event and reverses ledger entries cleanly.

## Delivery evidence

- Pending.
