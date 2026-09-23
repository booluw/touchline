import type { World, Club } from "./admin"

export interface FinanceFactor { label: string; amount: number }
export interface BudgetLine { season: number; allocated: number; committed: number; available: number }
export interface CommitmentLine { count: number; weekly_wage: number; annual_wage: number }

export interface Offer {
  id: string,
  world_id: string
  status: 'proposed',
  created_at: string
  club: Club
  world: World
  finance: FinanceSummary
  squad: OfferSquad
  league: {
    id: string
    name: string
    tier: number
    played: number
    won: number
    drawn: number
    lost: number
    points: number
    position?: number
  }
  board: {
    season: number,
    persona: string,
    mandates: {
      category: string
      description: string
      target_type: string,
      target_value: string
    }[]
  }
  manager: { id: string }
  supporters: { sentiment: 50, type: "working_class" }
}

export interface OfferSquad {
  size: number
  top_player: {
    id: string
    name: string
    overall: string
    position: string
  }
}

export interface FinanceSummary {
  currency: string
  cash: number
  operating_profit: number
  committed_spending: number
  projected_revenue: number
  projected_year_end_balance: number
  future_installments: number
  debt: number
  transfer_budget: BudgetLine
  wage_budget: BudgetLine
  wage_commitments: CommitmentLine
  factors: FinanceFactor[]
}

export interface LedgerEntry {
  id: string
  entry_type: 'credit' | 'debit'
  category: string
  amount: number
  description: string
  related_event_id?: string
  occurred_at: string
}
export interface ContractView {
  id: string
  player: { id: string; name: string }
  weekly_wage: number
  signing_bonus: number
  start_date: string
  end_date: string
  release_clause?: number
  status: string
}