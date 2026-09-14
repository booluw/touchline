// Package finance implements the ledger-based financial engine (S05-02).
//
// Principles (PRD §35-37, tech plan §14, finance-numerics.md):
//
//   - The ledger is append-only and signed: every account movement is a
//     credit or a debit in finance.ledger_entries. Cash is always derived
//     as Σcredit − Σdebit — there is no mutable balance column.
//
//   - Budgets are planned capacity, explicitly separate from cash. A club
//     can overspend its allocation; that is a board decision, not a bug.
//
//   - Monthly wages post 4 × weekly_wage per active commitment and are
//     idempotent under river's at-least-once redelivery via the
//     (account_id, dedup_key) unique index added in 0035.
//
//   - The initial balances, contracts and budgets are minted once at world
//     bootstrap by BootstrapClub (called from GenerateAIClub); a value can
//     only flow in or out of the ledger through a documented posting.
package finance
