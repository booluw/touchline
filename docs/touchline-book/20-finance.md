# Chapter 20 — Finance: ledger, budgets, wages and contracts

## 20.1 The append-only ledger

Money lives in `finance.ledger_entries` — **append-only**, one row per credit or
debit on a club's `finance.accounts` row. There is **no balance column**
anywhere: cash and every derived metric are `SUM(entries)` at read time.

- Money is integer **pence** at service boundaries, `NUMERIC(14,2)` in columns,
  displayed as USD.
- Each entry carries a `category` (e.g. `wages`, `academy`, `transfer_fee`,
  `player_sale`, `other`) and an optional **`dedup_key`** with a plain unique
  index `(account_id, dedup_key)` (migration 0035) — the `ON CONFLICT` arbiter
  that makes every recurring posting idempotent under redelivery.

| Posting | Dedup key |
| --- | --- |
| opening capital | `genesis:opening_capital` |
| monthly wages | `wage:<tick>:<contractID>` |
| academy maintenance | `academy:maintenance:<club>:<tick>` |
| transfer completion | `transfer:<completionID>:buyer` / `:seller` |

## 20.2 Genesis and budgets

At club creation (`finance.BootstrapClub`, same transaction):

| Item | Effect | Value |
| --- | --- | --- |
| Opening capital | credit / `other` | **$40,000,000** |
| Transfer budget | `finance.budgets` (no ledger row) | **$10,000,000** per season |
| Wage budget | `finance.budgets` | **$25,000,000** per season |

Budgets are **capacity**, separate from cash: `available = allocated −
committed`. Operating metrics exclude the genesis row; cash includes it.

## 20.3 Wages

```
WeeklyWage(position, attrMean) = positionBase[position] + bump(attrMean)
bump = round((attrMean − 50)² / 20)   if attrMean > 50, else 0      (max 125)
```

`positionBase` (weekly USD): GK 9,000 · CB 8,000 · LB/RB 7,000 · DM 7,500 ·
CM 8,500 · AM 8,000 · LM/RM 7,500 · LW/RW 8,000 · ST 9,500 · unknown 7,500.

**Contract length** at signing — `ContractSeasons(age)`: 4 (≤ 22), 3 (23–28),
2 (> 28). Startup contracts run from the nearest 1 July at least a full season
ahead. Youth contracts: fixed 3 seasons at the academy tier's wage
([Ch. 11](11-player-lifecycle-and-academy.md)).

### Contracts and commitments

`player.contracts` and `finance.wage_commitments` are kept in lockstep by
`RegisterContract` (one transaction, `CONTRACT_COMMITTED`). Guards: owning
manager, active world, `weekly_wage > 0`, `signing_bonus ≥ 0`, `end > start`,
player belongs to the club. Dates use the **world date**
([Ch. 4](04-world-clock-and-time.md)).

### Monthly wage posting

On each month boundary, `Finance.ApplyMonthlyWages`:

- per active commitment (`status = 'active'`, `end_date ≥ world date`): one
  debit `wages` of **`4 × weekly_wage`**, dedup-keyed;
- per club that actually posted: one `WAGE_POSTED` event (system actor) in the
  same transaction, explanation `monthly_wages` with a factor per player
  summing to `−total`;
- a redelivered tick posts nothing and emits nothing.

Annualisation uses `WeeksPerSeason = 52`.

## 20.4 The finance summary (`GetSummary`)

| Field | Computation |
| --- | --- |
| `cash` | Σ credit − Σ debit |
| `operating_profit` | same, current season, **excluding** `genesis:opening_capital` |
| `transfer_budget` / `wage_budget` | `{season, allocated, committed, available}` from the latest season |
| `wage_commitments` | `{count, weekly_wage, annual_wage = weekly × 52}` |
| `future_installments` | Σ future instalments on completed transfers (0 today) |
| `committed_spending` | annual wage + both budgets' committed + future instalments |
| `projected_revenue` | **0** (no revenue producers yet) |
| `projected_year_end_balance` | cash − committed spending + projected revenue |
| `debt` | **0** (no lending) |
| `factors` | net per category — **sums exactly to cash** (tested) |

A club with no account returns a zeroed, valid summary.

## 20.5 Where finance meets other systems

| System | Interaction |
| --- | --- |
| Board ([23](23-board-and-job-security.md)) | `financial_score` (wage ratio + profit sign); `wage_structure` and `operating_balance` mandates |
| Transfers ([21](21-transfer-market.md)) | buyer must have cash ≥ fee; AI buys only with cash ≥ 1.25 × fee; ledger rows at completion |
| Academy ([11](11-player-lifecycle-and-academy.md)) | monthly maintenance, upgrade costs, youth wages |
| Dashboard ([26](26-dashboard-news-scouting-realtime.md)) | negative cash / crisis items |
| Admin ([28](28-admin-console.md)) | country finance panels, crisis stages |

## 20.6 Not built yet

Revenue (gate, TV, sponsorship, prizes), debt/lending, instalments, financial
statements, advanced sponsorship (S13-03). Prize pools are specified but not
implemented ([Ch. 30](30-roadmap-and-open-decisions.md)).

## Connections

- Code: `internal/finance/{finance,model,wage,academy,seed,service,store}.go`.
- Source: `docs/design/finance-numerics.md`, S05-02.

---
[← Dressing room](19-dressing-room.md) · [Contents](the-touchline-book.md) · [Next: Transfer market →](21-transfer-market.md)
