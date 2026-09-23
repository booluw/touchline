import type { World, Club } from "./admin"

export interface Offer {
  id: string,
  world_id: string
  status: 'proposed',
  created_at: string
  club: Club
  world: World
  finance: OfferFinance
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

export interface OfferFinance {
  cash: number
  committed_spending: number
  currency: "USD"
  debt: number
  future_installments: number
  factors: { label: string, amount: number }[]
  operating_profit: number
  projected_revenue: number
  projected_year_end_balance: number
  transfer_budget: {
    season: number
    allocated: number
    committed: number
    available: number
  }
  wage_budget: {
    season: number
    allocated: number
    committed: number
    available: number
  }
  wage_commitments: {
    count: number
    weekly_wage: number
    annual_wage: number
  }
}