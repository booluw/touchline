# IM42 — Finances: health state, season breakdown, cash history, ledger filter

**Status:** Implemented
**Owner:** Claude Code
**Sprint:** Improvements (UI redesign endpoints)
**Source:** New UI design (claude.ai/design project 244e00dd…, `Touchline Finances Board.dc.html`), gap analysis 2026-10-06. See `UI-ENDPOINTS-HANDOFF.md`.
**Depends on:** S05 finance

## What to do

1. `GET /api/clubs/:id/finances` adds `health` (open `financial_crisis_states` row: `stage`, `started_at`; null when healthy), `season_breakdown` {`revenue`, `expenses`} (season-to-date ledger totals by category, credit vs debit) and `cash_history` (month-end balance for each month with ledger activity).
2. `GET /api/clubs/:id/ledger` accepts `?category=` and each entry adds `balance_after` (running balance).

## Open questions (design needs data the engine does not model)

- Health score /100 and its factors, "in effect now" restrictions, next-state triggers: crisis stages exist, no score.
- Runway in weeks and projected monthly cash: needs a burn-rate rule (product decision).
- Counterparty column and "Installment" tag: not stored on ledger rows.
- £500k emergency floor: confirm the crisis thresholds before showing a line.

## Recorded decisions

Read-only aggregations over `finance.ledger_entries`; no new writes.

## Delivery evidence

### Files

internal/finance/{model,store,service}.go (`OpenCrisis`, `SeasonBreakdown`, `CashHistory`, ledger window balance + category), finance_handlers.go `?category=`, openapi FinancialSummary/LedgerEntry/ledger param; also corrected the stale LedgerEntry `entry_type` enum and example to credit/debit. service_integration_test.go.

### Verification (2026-10-06, embedded Postgres 16)

gofmt clean; `go build ./...`, `go vet ./...`, `go vet -tags integration ./internal/... ./pkg/...`, `go test ./...` pass; the touched integration test passes; `TestDocsCoverRouter`/`TestDocsOpenAPIValid` pass. Full serial integration run: see `UI-ENDPOINTS-HANDOFF.md`.
