# S05-02 — Deliver ledger finance, budgets, contracts, and wages

**Status:** Not started  
**Sprint:** 05 — Manager controls and finance  
**Source:** PRD §§35–37, 67, 74, 81; technical plan §§6, 14, 16  
**Depends on:** S01-01, S01-03

## What to do

Implement append-only club financial transactions, derived financial summaries, and MVP player contracts/wage commitments. Distinguish cash from budget and expose the reasons behind financial state.

## Acceptance criteria

- Financial transactions are written as append-only `finance.ledger_entries`; no mutable balance is introduced.
- Finance summary exposes cash, budget, committed spending, future installments, projected revenue, wage commitments, debt, operating profit, and projected year-end balance when their underlying data exists.
- Contracts and wage commitments are world-scoped and generate ledger effects through server-side processing.
- Finance APIs explain the composition of reported financial state rather than returning an unexplained total.
- Property/integration tests show reconciliation is preserved and no supported operation creates or destroys money unexpectedly.

## Delivery evidence

- Pending.
