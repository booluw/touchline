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

export type CompetitionType = 'league' | 'domestic_cup';

export type CupStage = 'not_started' | 'waiting' | 'playing' | 'eliminated';

export interface SeasonRef {
  id: string;
  label: string;
  number: number;
  status: string;
}

export interface CountryRef {
  id: string;
  name: string;
  code: string;
}

export interface StandingRow {
  club: {
    id: string
    name: string
    short: string
  }
  played: number;
  won: number;
  drawn: number;
  lost: number;
  goals_for: number;
  goals_against: number;
  points: number;
}

export interface Standings {
  season_id: string;
  season_label: string;
  season_number: number;
  status: string;
  rows: StandingRow[];
}

export interface League {
  id: string;
  world_id: string;
  country_id: string;
  name: string;
  tier: number;
  team_count: number;
  status: string;
  promotions: number;
  relegations: number;
  promotes_to: string | null;
  relegates_to: string | null;
}

export interface Cup {
  id: string;
  world_id: string;
  name: string;
  competition_type: 'domestic_cup';
  status: string;
  prize_pool: number;
  country: CountryRef;
  format: string;
  is_home_and_away: boolean;
  first_tier_bye: number;
  survivor_threshold: number;
}

// Shape guessed: the code reads fixtures[i].Competition.ID and .Matchday,
// so it's likely nested rather than the flat OpenAPI CompetitionFixture.
export interface CompetitionFixture {
  id: string;
  world_id: string;
  competition: { id: string; name?: string };
  home_club_id: string;
  home_club_name: string;
  away_club_id: string;
  away_club_name: string;
  matchday: number;
  scheduled_at: string;
  status: 'scheduled' | 'running' | 'finished';
  home_score?: number | null;
  away_score?: number | null;
}

export interface ClubLeagueView {
  competition: League;
  season?: SeasonRef | null;   // absent/null until a season exists
  started: boolean;
  standings?: Standings | null; // null when no season
  next_fixture?: CompetitionFixture | null;
}

export interface ClubCupView {
  competition: Cup;
  season?: SeasonRef | null;
  started: boolean;
  stage: CupStage;
  total_rounds: number;
  current_round: number;
  champion?: unknown | null;   // type comes from cupClubState, not visible here
  next_fixture?: CompetitionFixture | null;
}

export interface ManagerCompetition {
  role: string;
  joined_at: string;           // ISO timestamp
  competition_type: CompetitionType;
  league?: ClubLeagueView | null; // set only when competition_type === 'league'
  cup?: ClubCupView | null;       // set only when competition_type === 'domestic_cup'
}

export interface Fixture {
  id: string
  world_id: string,
  competition: {
    id: string,
    name: string
  },
  home_club: Club,
  away_club: Club,
  matchday: number,
  scheduled_at: string,
  status: FixtureStatus
}

export interface NextFixture {
  fixture: Fixture;
  gameweek: number;
  home_or_away: HomeOrAway;
  derby: boolean;
  golden_goal: boolean;
  opponent: Opponent;
}

interface Opponent {
  club: Club;
  country: Country;
  is_ai_controlled: boolean;
  manager: Manager;
  reputation: number;
  tier: number;
  league_position: number;
  form: Form;
  squad_count: number;
  top_players: TopPlayer[];
}

interface Country {
  id: string;
  name: string;
  code: string;
}

interface Manager {
  id: string;
  is_policy_bot: boolean;
}

interface Form {
  form_string: string;
  current_rating: number;
}

interface TopPlayer {
  id: string;
  person_id: string;
  first_name: string;
  last_name: string;
  display_name: string;
  primary_position: PlayerPosition;
  rating: number;
}

export interface Board {
  confidence: number
  snapshot: {
    manager_id: string
    club: Club,
    world_tick: number
    scores: {
      performance_score: number
      expectations_score: number
      financial_score: number
      board_relationship_score: number
      club_dna_alignment_score: number
      supporter_sentiment_score: number
      alternatives_score: number
      total_score: number
    }
  }
  explanation: {
    factors:
    {
      delta: number
      label: string
    }[]
    score: number
    subject: string
  }
  mandates: null | unknown
}

type HomeOrAway = "home" | "away";

type FixtureStatus =
  | "scheduled"
  | "live"
  | "completed"
  | "postponed"
  | "cancelled";

type PlayerPosition =
  | "GK"
  | "CB"
  | "LB"
  | "RB"
  | "LWB"
  | "RWB"
  | "CDM"
  | "CM"
  | "CAM"
  | "LM"
  | "RM"
  | "LW"
  | "RW"
  | "ST"
  | "CF";