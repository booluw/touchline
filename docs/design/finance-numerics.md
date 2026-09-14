# Finance Numerics (S05-02)

Source of truth for the implemented numbers behind the ledger finance,
budget, contract, and wage systems. The product tasks
(`docs/tasks/S05-02-ledger-finance-and-contract-wages.md`, PRD §§35–37, 67,
74, 81) publish ranges; this document fixes the implemented values and binds
them to the existing `0009-finance` schema. All numbers here are **proposal**
until PM tuning sign-off; every block is config/data expressed as constants in
`internal/finance` (never freeform in engine code).

The ledger is append-only (`finance.ledger_entries`). There is **no stored
balance column** — cash and every derived metric is `SUM(entries)` computed at
read time.

## 1. Genesis & budgets (world bootstrap)

When `GenerateAIClub` (`internal/bootstrap`) creates a club it calls
`finance.BootstrapClub` inside the same transaction and mints:

| Item | Ledger effect | Value |
| --- | --- | --- |
| Opening capital | `credit` / `other` | **$40,000,000** |
| Budget allocations | `finance.budgets`, no ledger rows | transfer **$10,000,000**, wage **$25,000,000** (per season) |

Constants: `OpeningCapital`, `TransferBudget`, `WageBudget` in
`internal/finance/model.go`.

The opening-capital credit carries `dedup_key = 'genesis:opening_capital'`.
Derived metrics that represent operating performance (operating profit)
**exclude** that row by `dedup_key`; balance-style metrics (cash) include it.
Bootstrap is idempotent (`ON CONFLICT` on accounts/club; deduped ledger write),
so it is safe under event redelivery.

## 2. Wage formula

```
WeeklyWage(position, attrMean) = positionBase[position] + bump(attrMean)
```

- `bump(attrMean) = round((attrMean − 50)² / 20)` — **only** when
  `attrMean > 50`, else `0`.
- `positionBase` (weekly, USD):
  GK 9,000 · CB 8,000 · LB/RB 7,000 · DM 7,500 · CM 8,500 · AM 8,000 ·
  LM/RM 7,500 · LW/RW 8,000 · ST 9,500.
- Unknown positions (not in the map) fall back to a 7,500 base.
- `attrMean` = arithmetic mean of the player's attribute map, rounded to the
  nearest integer (`MeanAttribute`); for generated squads this is the
  full attribute set.

Properties (unit-tested): monotonic in `attrMean`, bounded (max bump for
`attrMean ≤ 100` is `round(50²/20) = 125`, so an 100-mean striker costs
9,625/wk), and sum-of-squad for a typical 24-player generated first team sits
comfortably inside the $25M wage budget.

### 2.1 Contract length

`ContractSeasons(age)` — standard full-season term at signing: **4** seasons
(≤22), **3** (>22 and ≤28), **2** (>28). Startup contracts run from the
closest 1 July *at least one full season ahead*, so the whole squad's first
commitments span a season boundary.

## 3. Monthly wage posting (worker)

The worker's **monthly** `WORLD_TICK` branch calls
`finance.ApplyMonthlyWages(worldID, tick)` for every playable world.

- Per active wage commitment — `status = 'active'` and `end_date ≥ today` — it
  posts one **debit / `wages`** entry of:
  `4 × weekly_wage`  (`WeeksPerMonth`)
- `dedup_key = 'wage:<tick>:<contractID>'` → the whole run is **idempotent**
  under river's at-least-once redelivery. A redelivered tick posts nothing new
  (`ON CONFLICT DO NOTHING`, `RowsAffected() == 0`) and emits **no duplicate
  `WAGE_POSTED` event**.
- Each club that actually received postings commits the `WAGE_POSTED` event
  (actor `system`, `world_tick` set) in the **same transaction** as the ledger
  rows, carrying an `explanation` (`subject: monthly_wages`) with one
  per-player factor (−`4 × weekly_wage`) and `score = −total`.

No active commitments → nothing posted, no event, no commit.

## 4. Annualization & commitments

- `WeeksPerSeason = 52`. Annualised wage commitment = `sum(weekly_wage) × 52`.
- `CommittedSpending = AnnualWage + TransferBudget.Committed
  + WageBudget.Committed + FutureInstallments`.
- `ProjectedYearEndBalance = Cash − CommittedSpending + ProjectedRevenue`.

## 5. Summary read model (`GetSummary`)

| Field | Computation |
| --- | --- |
| `currency` | `USD` |
| `cash` | `SUM(credit) − SUM(debit)` over the club's account |
| `operating_profit` | same sum, current season (`occurred_at ≥ 1 Jan`) **excluding** `genesis:opening_capital` |
| `transfer_budget` / `wage_budget` | `{season, allocated, committed, available = allocated − committed}` from highest seeded season |
| `wage_commitments` | `{count, weekly_wage = Σ, annual_wage = Σ × 52}` over active commitments |
| `future_installments` | `SUM(installments[].amount)` with `due_date > today` from `transfer.completed_transfers` (S16 forward-compat; 0 today) |
| `committed_spending` | §4 |
| `projected_revenue` | **0** — no phase-1 revenue producers yet (summary factors explain why) |
| `projected_year_end_balance` | §4 |
| `debt` | **0** — no lending subsystem |
| `factors` | net `credit − debit` per `category`, labeled; **sums exactly to `cash`** (enforced by integration test) |

A club with no finance account (manually seeded pre-bootstrap test clubs)
returns a zeroed, valid summary — it simply never touched the ledger.

## 6. Contracts

`player.contracts` + `finance.wage_commitments` are kept in lockstep by
service code (`RegisterContract` inserts both in one tx, emitting
`CONTRACT_COMMITTED` in-tx). Guards: club ownership (current owning manager),
active world, `weekly_wage > 0`, `signing_bonus ≥ 0`, `end > start`, player
must belong to that club. `ContractSeasons` lengths become the defaults the
bootstrap seeding passes in.

## 7. Events

`WAGE_POSTED` (system actor; world_tick; explanation `monthly_wages`) and
`CONTRACT_COMMITTED` (actor type `manager`|`policy_bot` from the invoking
`manager.managers` row). Bootstrap minting itself emits no finance events —
the surrounding `CLUB_CREATED` covers it.

## 8. Migrations

- `0035`: `finance.ledger_entries.dedup_key` (nullable `TEXT`) + a **plain**
  unique index on `(account_id, dedup_key)`, the `ON CONFLICT` arbiter for
  deduped wage postings. A partial index (`WHERE dedup_key IS NOT NULL`) was
  rejected at runtime: `ON CONFLICT (account_id, dedup_key)` requires a
  matching non-partial index or an inference `WHERE`. NULLs never collide in a
  plain unique index, so a plain index is both correct and all that's needed.