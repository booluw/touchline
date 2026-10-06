interface FixtureClub {
  id: string
  name: string
  short: string
}

interface FixtureCompetition {
  id: string
  name: string
}

interface FixtureGame {
  id: string
  world_id: string
  competition: FixtureCompetition
  home_club: FixtureClub
  away_club: FixtureClub
  matchday: number
  gameweek: number
  scheduled_at: string
  status: "scheduled" | "completed"
}

interface FixtureCountry {
  id: string
  name: string
  code: string
}

interface FixtureManager {
  id: string,
  is_policy_bot: boolean
}

interface FixtureForm {
  form_string: string
  current_rating: number
}

interface FixturePlayer {
  id: string
  person_id: string
  first_name: string
  last_name: string
  display_name: string
  primary_position: "GK" | "CB" | "RB" | "LB" | "DM" | "CM" | "LM" | "RM" | "AM" | "RW" | "LW" | "ST"
  rating: number
}

export interface ManagerNextFixture {
  fixture: FixtureGame
  gameweek: number
  home_or_away: "home" | "away"
  derby: boolean
  golden_goal: boolean
    opponent: {
    club: FixtureClub
    country: FixtureCountry
    is_ai_controlled: boolean
    manager: FixtureManager
    reputation: number
    tier: number
    league_position: number
    form: FixtureForm
    squad_count: number
    top_players: FixturePlayer[]
  }
}