# Financial Sustainability & FFP Audit System Design Ledger

This document is the authoritative design ledger for Touchline's Financial Sustainability, Profitability & Sustainability Rules (PSR), and Financial Audit / "115 Charges" Investigation System (`internal/finance/ffp`).

---

## 1. Core Principles & Philosophy

1. **Append-Only Auditability**: All financial health calculations run directly on `finance.ledger_entries` (`SUM(entries)`). No arbitrary stored balances.
2. **Rolling 3-Season PSR Evaluation**: Clubs must operate within financial loss thresholds over a rolling 3-season window.
3. **High-Risk Creative Accounting**: Managers facing financial crises can choose high-risk financial shortcuts (inflated owner sponsorships, off-book agent fees) that boost short-term spending power while increasing audit risk.
4. **Consequences with Drama**: Financial breaches trigger public Independent Commission investigations, legal defense choices, transfer embargoes, and standings points deductions.

---

## 2. Rolling 3-Season PSR Calculations

During season rollover ([Chapter 7](file:///Users/bfree/Desktop/booluw/touchline/docs/touchline-book/07-seasons-and-rollover.md)), `finance.EvaluatePSR` audits every active club:

```
3SeasonOperatingLoss = SUM(ledger_entries) where category IN ('wages', 'transfer_fee', 'academy', 'other_debit')
                        over seasons [CurrentSeason - 2, CurrentSeason]
```

### PSR Spending Caps by League Reputation Tier:

| League Reputation Tier | Max 3-Season Operating Loss | Warning Threshold | Audit Trigger |
| --- | --- | --- | --- |
| **Tier 1 (Top Tier)** | $35,000,000 | $25,000,000 | > $35,000,000 |
| **Tier 2 (Second Tier)** | $20,000,000 | $14,000,000 | > $20,000,000 |
| **Tier 3 (Lower Tier)** | $10,000,000 | $7,000,000 | > $10,000,000 |

---

## 3. The "115 Charges" High-Risk Financial Arc (`high_risk_finance`)

### 3.1 Creative Accounting Dilemmas

When a club's cash reserves drop below $2,000,000 or wage committed ratio exceeds 90%, the engine offers **High-Risk Financial Choices**:

1. **Option 1: Inflated Owner Sponsorship Deal**
   - *Immediate Benefit*: +$20,000,000 added to transfer budget.
   - *Audit Risk Added*: +35% risk score.
   - *Board Alignment*: +10 Financial factor score.
2. **Option 2: Off-Book Agent Commission Structuring**
   - *Immediate Benefit*: Reduces reported wage bill on ledger by 25%.
   - *Audit Risk Added*: +40% risk score.
   - *Board Alignment*: Neutral.

---

## 4. Investigation & Sanctions Progression

When a club's `audit_risk_score` reaches **≥ 60%** or 3-Season Operating Loss exceeds the PSR Cap, an **Independent Commission Investigation Arc** is triggered:

```
[PSR Breach / Audit Risk ≥ 60%] ──► [STAGE 1: AUDIT WARNING]
                                            │
                                     (14 World Days)
                                            ▼
                                    [STAGE 2: INDEPENDENT COMMISSION TRIAL]
                                            │
                                     (Manager Defense Choice)
                                            ▼
                                    [STAGE 3: VERDICT & SANCTIONS]
                                    - Points Deduction (-6 to -12)
                                    - Transfer Embargo
                                    - League Fine
                                            │
                                     (Legal Appeal Arc)
                                            ▼
                                    [STAGE 4: APPEAL RESOLUTION]
```

### 4.1 Stage 3 Verdict Sanctions Matrix

| Breach Severity | Points Deduction Penalty | Transfer Embargo Duration | Cash Fine Penalty |
| --- | --- | --- | --- |
| **Minor PSR Overspend (≤15%)** | **-6 Points** | 1 Transfer Window | $5,000,000 |
| **Major PSR Overspend (>15%)** | **-10 Points** | 2 Transfer Windows | $12,000,000 |
| **Creative Accounting / Charges** | **-12 Points** | 2 Transfer Windows | $25,000,000 |

Points deductions are immediately applied to current league standings (`league.standings.points_deducted`), generating a major world news headline (`announcement` category).

---

## 5. Legal Defense & Appeal Dilemma

During **Stage 2 (Trial)**, the manager faces a crucial decision:

- **Option A: Hire Elite Legal Defense Counsel**
  - *Cash Cost*: $3,000,000 (from ledger).
  - *Effect*: 60% probability of reducing points deduction by 4 points on appeal.
- **Option B: Plead Guilty & Cooperate**
  - *Effect*: Guaranteed 2-point reduction on penalty; Supporter Sentiment -10.
- **Option C: Challenge League Authority in Court**
  - *Effect*: 30% chance of complete acquittal, but 70% chance penalty increases by +3 points; Board Relationship score -20.

---

## 6. Table of Constants & Parameters

| Constant Name | Value | Purpose |
| --- | --- | --- |
| `PSRWindowSeasons` | `3` | Rolling season audit window |
| `Tier1Max3SeasonLoss` | `$35,000,000` | Tier 1 PSR spending loss cap |
| `HighRiskAuditThreshold` | `60` | Audit risk score triggering trial |
| `MinorBreachPointsDeduction` | `6` | Points deduction for minor PSR breach |
| `MajorBreachPointsDeduction` | `10` | Points deduction for major PSR breach |
| `ChargesBreachPointsDeduction` | `12` | Points deduction for creative accounting breach |

---
*Source: Touchline Product Architecture, `docs/touchline-book/20-finance.md`, `docs/design/board-numerics.md`.*
