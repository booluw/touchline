import type { CommonWorld, CommonClub } from "../common";
import type { ManagerBudgetLine, ManagerLedgerCategoryTotal } from "./finances";

interface ManagerCommitmentLine { count: number; weekly_wage: number; annual_wage: number }

interface ManagerOfferSquad {
  size: number
  top_player: {
    id: string
    name: string
    overall: string
    position: string
  }
}

export interface ManagerFinanceSummary {
  currency: string
  cash: number
  operating_profit: number
  committed_spending: number
  projected_revenue: number
  projected_year_end_balance: number
  future_installments: number
  debt: number
  transfer_budget: ManagerBudgetLine
  wage_budget: ManagerBudgetLine
  wage_commitments: ManagerCommitmentLine
  factors: ManagerLedgerCategoryTotal[]
}

export interface ManagerOffer {
  id: string,
    world_id: string
    status: 'proposed',
    created_at: string
    club: CommonClub
    world: CommonWorld
    finance: ManagerFinanceSummary
    squad: ManagerOfferSquad
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