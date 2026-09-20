export interface World {
  id: string,
  name: string,
  status: "provisioning" | "seeding" | "active" | "paused" | "archived",
  created_at: Date
}

export interface Country {
  id: string
  world_id: string
  code: string
  name: string
}

export interface League {
  id: string,
  world_id: string,
  country_id: string,
  name: string,
  tier: number,
  team_count: number,
  status: string,
  promotions: number,
  relegations: number,
  promotes_to: string,
  relegates_to: string,
}

export interface CountryFinance {
  cash: number,
  country: Country,
  crisis_clubs: unknown[]
  top_wage_bills: unknown[]
  wage_bill: number
  transfer_budget: { allocated: number, committed: number, available: number, utilized_pct: number }
  wage_budget: { allocated: number, committed: number, available: number, utilized_pct: number }
}

export interface LeaguePyramid {
  country: Country,
  leagues: League[]
}

export interface CountryClubs {
  country: Country
  clubs: unknown[]
}

export interface CountryPlayers {
  avg_age: number
  by_origin: unknown
  by_position: unknown[]
  by_status: unknown
  intakes: unknown[]
  nationalities: unknown[]
  total: number
  country: Country
}

export interface CountryFreeAgents {
  items: unknown[]
  total: number
}