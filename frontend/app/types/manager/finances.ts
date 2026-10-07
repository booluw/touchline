interface LedgerCategoryTotal {
  amount: number
  label: string
}

interface BudgetLine {
  allocated: number
  available: number
  committed: number
  season: number
}

export interface ManangerFinanceSummary {
  cash: number
  committed_spending: number
  currency: string
  debt: number
  future_installments: number
  operating_profit: number
  projected_revenue: number
  projected_year_end_balance: number
  factors: LedgerCategoryTotal[]
  cash_history: { balance: number, month: string }[]
  health: {
    stage: "warning" | "restriction" | "emergency" | "administration_risk" | "ownership_intervention" | "bankruptcy"
    started_at: string
  }
  season_breakdown: {
    expenses: LedgerCategoryTotal[]
    revenue: LedgerCategoryTotal[]
    season: number
  }
  transfer_budget: BudgetLine
  wage_budget: BudgetLine
  wage_commitments: {
    annual_wage: number
    count: number
    weekly_wage: number
  }
}