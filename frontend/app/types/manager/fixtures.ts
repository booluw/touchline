import type { CommonClub, CommonCountry } from "../common"
import type { PlayerPosition } from "../player"

interface FixtureCompetition {
  id: string
  name: string
}

interface FixtureGame {
  id: string
  world_id: string
  competition: FixtureCompetition
  home_club: CommonClub
  away_club: CommonClub
  matchday: number
  gameweek: number
  scheduled_at: string
  status: "scheduled" | "completed"
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
  primary_position: PlayerPosition
  rating: number
}

export interface ManagerFixtureDossier {
  club: CommonClub
  country: CommonCountry
  is_ai_controlled: boolean
  manager?: FixtureManager
  reputation: number
  tier: number
  league_position?: number
  form: FixtureForm
  squad_count: number
  top_players: FixturePlayer[]
}

export interface ManagerNextFixture {
  fixture: FixtureGame
  gameweek: number
  home_or_away: "home" | "away"
  derby: boolean
  golden_goal: boolean
  /** The manager's own club, same shape as the opponent. */
  club: ManagerFixtureDossier
  opponent: ManagerFixtureDossier
}

export interface ManagerFixture {
  away_club: CommonClub
  home_club: CommonClub
  away_score: number
  gameweek: number
  id: string
  match_id: string
  matchday: number
  scheduled_at: string
  status: "scheduled" | "running" | "finished"
  world_id: string
  competition: { id: string, name: string }
  difficulty: {
    factors: { delta: number, detail: string, label: string }[]
    level: 1 | 2 | 3 | 4 | 5
    label: "Very easy" | "Easy" | "Even" | "Hard" | "Very hard"
  }
}
/** One row of GET /clubs/:id/fixtures?season=current (IM67). */
export interface SeasonFixture {
  id: string
  world_id: string
  competition: { id: string, name: string }
  home_club: CommonClub
  away_club: CommonClub
  /** League matchday, or the cup round. */
  matchday: number
  scheduled_at: string
  status: "scheduled" | "live" | "completed" | "postponed"
  home_score?: number
  away_score?: number
  match_id?: string
  /** Crowd (IM66); null until kickoff and for matches played before crowds were modelled. */
  attendance: number | null
  /** League position once this matchday is in; league matches only. */
  position_after: number | null
  /** Unplayed fixtures only. */
  last_meeting: { home_club: CommonClub, away_club: CommonClub, home_score?: number, away_score?: number, scheduled_at: string } | null
  difficulty: ManagerFixture["difficulty"] | null
}

export interface MatchSideStats {
  goals: number
  chances_created: number
  yellow_cards: number
  red_cards: number
  xg: number | null
}

export interface MatchSummary {
  events: { minute: number, type: string, club?: { id: string }, player?: { id: string, name?: string } }[]
  stats: { home: MatchSideStats, away: MatchSideStats }
}
