export type {
  CompetitionType as CommonCompetitionType,
  CupStage as CommonCupStage
} from './competition';


export interface CommonWorld {
  id: string
  name: string
  status: "provisioning" | "seeding" | "active" | "paused" | "archived"
  created_at: string
}

export interface CommonClub {
  id: string
  name: string
  short: string
  reputation: number
  tier: number
}

export interface CommonCountry {
  id: string
  world_id: string
  code: string
  name: string
}