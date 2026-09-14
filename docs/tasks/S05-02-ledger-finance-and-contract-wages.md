# S05-02 — Deliver ledger finance, budgets, contracts, and wages

**Status:** Done  
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

- **Ledger engine** (`internal/finance`): append-only credit/debit postings;
  cash/operating profit/ledger all computed as `SUM(entries)`, never a stored
  balance. Migration `0035` adds `finance.ledger_entries.dedup_key` + a unique
  `(account_id, dedup_key)` index as the `ON CONFLICT` arbiter for idempotent
  wage postings.
- **Bootstrap minting**: `GenerateAIClub` now calls `finance.BootstrapClub` —
  one finance account, $40M opening capital, transfer/wage budget allocations
  ($10M/$25M), and starter contracts + wage commitments for the full generated
  squad (dedup key `genesis:opening_capital`; `CLUB_CREATED` payload gains
  `contract_count`). Idempotent under redelivery.
- **Wage formula**: `WeeklyWage(position, attrMean) =
  positionBase[pos] + round((attrMean−50)²/20)` when mean > 50 (`docs/design/
  finance-numerics.md`). Monotonic, bounded, budget-compatible — unit-tested.
- **Server-side processing**: the worker's monthly `WORLD_TICK` branch posts
  `4 × weekly_wage` per active commitment with per-contract dedup keys
  (`wage:<tick>:<contractID>`) — replay is a no-op, no duplicate `WAGE_POSTED`
  events. Explanations (`subject: monthly_wages`, one factor per player, score
  = −total) make the posting auditable.
- **Summary explains state**: `GetSummary` returns labeled `factors`
  (net credit−debit per category) that **sum exactly to cash** — enforced by an
  integration test — plus budgets (allocated/committed/available), wage
  commitments, committed spending, operating profit (genesis excluded),
  projected revenue/year-end, future installments, debt. A club with no account
  gets a valid zeroed summary.
- **API**: `GET /api/clubs/:id/{finances,ledger,contracts}`,
  ownership-gated (401/403 for strangers, 404 club/world, 400 bad id); frontend
  `pages/finances.vue` renders cash, budgets, commitments, factor table, ledger,
  contracts from `stores/finance.ts`.
- **Gates**: backend `go build`/`go vet`/unit ✓; full integration suite
  `-p 1 -tags integration -count=1 ./...` ✓ (incl. finance integration tests +
  regression on bootstrap/squad/competition/matchday/training/cmd-api);
  frontend `pnpm run typecheck` + `pnpm run lint` ✓.
- **Numerics contract**: `docs/design/finance-numerics.md`.

Scope notes: crisis ladder, revenue producers, and transfer-fee installments
are deliberately out of scope — `operating_profit`/`projected_revenue`/`debt`
stay 0- or skeleton-valued until those producers post real entries (see
`OPENCODE.md` "What is NOT built yet" item 14).
