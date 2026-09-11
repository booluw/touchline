# S07-04 — Execute MVP end-to-end flow validation and load testing suite

**Status:** Not started  
**Sprint:** 07 — MVP experience, operations, and validation  
**Source:** PRD §85; technical plan §§14, 16; OPENCODE.md  
**Depends on:** S07-01, S07-02, S07-03

## What to do

Build and run end-to-end integration tests and performance load testing suites (using `k6`) targeting the MVP core loop. Test multi-manager world creation, squad management, match simulation execution, human-to-human transfer negotiation, financial ledger balance reconciliation, and WebSocket fan-out under simulated concurrent traffic. Evaluate success against the PRD §85 MVP exit question ("Is managing among real managers more compelling than managing alone?").

## Acceptance criteria

- E2E test suite simulates a 10-manager league completing a multi-fixture matchday cycle, transfer negotiation, and financial settlement cleanly.
- `k6` load test suite executes 500+ concurrent simulated manager sessions performing API commands and receiving WebSocket match ticks without event drop or memory leaks.
- Property-based tests confirm financial ledger reconciliation (`SUM(ledger_entries)`) remains exact across all simulated load transactions.
- Zero panics, database deadlocks, or unhandled promise rejections occur during 24-hour continuous tick simulation run.
- MVP completion evidence report is generated documenting system throughput, latency profiles, and operational readiness.

## Delivery evidence

- Pending.
