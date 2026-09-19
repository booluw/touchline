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