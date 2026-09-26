export interface World {
  id: string;
  name: string;
  status: "provisioning" | "seeding" | "active" | "paused" | "archived";
  created_at: Date;
}

export interface Country {
  id: string;
  world_id: string;
  code: string;
  name: string;
}

export interface LeagueRef {
  id: string;
  name?: string;
}

export interface League {
  id: string;
  world_id: string;
  country: {
    id: string;
    name?: string;
    code?: string;
  };
  name: string;
  tier: number;
  team_count: number;
  status: string;
  promotions: number;
  relegations: number;
  promotes_to: LeagueRef | null;
  relegates_to: LeagueRef | null;
}

export interface Club {
  id: string;
  name: string;
  short: string;
  reputation: number;
  tier: number;
}

export interface Player {
  id: string;
  name: string;
}

/* -------------------------------------------------------------------------- */
/* Country                                                                     */
/* -------------------------------------------------------------------------- */

export interface CountryFinance {
  cash: number;
  country: Country;
  crisis_clubs: unknown[];
  top_wage_bills: unknown[];
  wage_bill: number;
  transfer_budget: Budget;
  wage_budget: Budget;
}

export interface Budget {
  allocated: number;
  committed: number;
  available: number;
  utilized_pct: number;
}

export interface LeaguePyramid {
  country: Country;
  leagues: League[];
}

export interface CountryClubs {
  country: Country;
  clubs: unknown[];
}

export interface CountryPlayers {
  avg_age: number;
  by_origin: unknown;
  by_position: unknown[];
  by_status: unknown;
  intakes: unknown[];
  nationalities: unknown[];
  total: number;
  country: Country;
}

export interface CountryFreeAgents {
  items: unknown[];
  total: number;
}

export interface CountryStats {
  country: Country;
  as_of: string;

  population: {
    total: number;
    active: number;
    free_agents: number;
    retired: number;
    by_status: {
      active: number;
      free_agent: number;
    };
  };

  clubs: {
    total: number;
    in_crisis: number;
    orphan_clubs: number;
    unassigned_clubs: number;
  };

  leagues: {
    total: number;
    by_tier: Record<string, number>;
  };

  market: {
    open_listings: number;
    bids_received: number;
    bids_made: number;
    transfers_in: number;
    transfers_out: number;
    free_agent_signings: number;
  };

  economy: {
    crisis_clubs: number;
    wage_bill: number;
    wage_allocated: number;
    wage_committed: number;
    transfer_allocated: number;
    transfer_committed: number;
    cash: number;
  };

  unassigned: {
    world_pool_players: number;
    orphan_clubs: number;
  };

  headlines: unknown[];
}

/* -------------------------------------------------------------------------- */
/* Country Market                                                              */
/* -------------------------------------------------------------------------- */

export interface CountryMarketData {
  country: Country;
  window_days: number;
  open_listings: OpenListing[];
  bids_received: Bid[];
  bids_made: Bid[];
  transfers_in: Transfer[];
  transfers_out: Transfer[];
  signings: Transfer[];
}

export interface OpenListing {
  listing_id: string;
  player: Player;
  position: string;
  listing_club: Club;
  asking_price: number;
  market_value: number;
  listing_type: string;
  listed_at: string;
}

export interface Bid {
  bid_id: string;
  player: Player;
  bidding_club: Club;
  selling_club: Club;
  fee: number;
  status: string;
  created_at: string;
}

export interface Transfer {
  transfer_id: string;
  player: Player;
  from_club: Club;
  to_club: Club;
  fee: number;
  market_value: number;
  overpay: boolean;
  overpay_pct: number;
  completed_at: string;
}

/* -------------------------------------------------------------------------- */
/* Competition                                                                 */
/* -------------------------------------------------------------------------- */

export interface Competition {
  id: string;
  world_id: string;
  name: string;
  competition_type: "league" | string;
  status: string;

  country: Country;

  region: Region;

  tier: number;
  team_count: number;
  reputation: number;
  seasons_total: number;

  past_winners: PastWinner[];

  top_scorers: TopScorers;

  league: LeagueCompetition | null;
  cup: Cup | null;
}

export interface Region {
  id: string;
  name: string;
}

/* -------------------------------------------------------------------------- */
/* Seasons                                                                     */
/* -------------------------------------------------------------------------- */

export interface Season {
  season_id: string;
  season_label: string;
  season_number: number;
  status: string;
}

export interface PastWinner {
  season: Season;
  champion: Club;
}

/* -------------------------------------------------------------------------- */
/* Top Scorers                                                                 */
/* -------------------------------------------------------------------------- */

export interface TopScorers {
  current_season: TopScorer[];
  all_time: TopScorer[];
}

export interface TopScorer {
  rank: number;
  player: Player;
  club: Club;
  goals: number;
  penalties: number;
}

/* -------------------------------------------------------------------------- */
/* League Competition                                                          */
/* -------------------------------------------------------------------------- */

export interface LeagueCompetition {
  season: LeagueSeason;
  seasons: Season[];
  standings: LeagueStanding[];
  movement: LeagueMovement;
}

export interface LeagueSeason {
  id: string;
  label: string;
  number: number;
  status: string;
  fixtures_played: number;
  fixtures_total: number;
}

export interface LeagueStanding {
  club: {
    id: string;
    name: string;
    short: string;
  };

  played: number;
  won: number;
  drawn: number;
  lost: number;
  goals_for: number;
  goals_against: number;
  points: number;
}

export interface LeagueMovement {
  promoted_in: Club[];
  relegated_out: Club[];
}

/* -------------------------------------------------------------------------- */
/* Cup                                                                          */
/* -------------------------------------------------------------------------- */

export interface Cup {
  season: Season;
  late_entry_round: number;
  total_rounds: number;
  champion: Club | null;
  rounds: CupRound[];
  clubs: CupClub[];
}

export interface CupRound {
  round: number;
  scheduled_at: string;
  ties: CupTie[];
  byes: Club[];
}

export interface CupTie {
  id: string;

  home_club: Club;
  away_club: Club;

  scheduled_at: string;
  status: string;

  home_score: number;
  away_score: number;

  winner: Club | null;
}

export interface CupClub {
  club: Club;
  country: Country;

  eliminated: boolean;
  current_round: number;

  streak: Streak;

  next_fixture: NextFixture | null;
}

export interface Streak {
  outcome: "W" | "D" | "L" | string;
  length: number;
}

export interface NextFixture {
  fixture: Fixture;
  gameweek: number;
  home_or_away: "home" | "away";
  derby: boolean;
  golden_goal: boolean;
  opponent: Opponent;
}


export interface Fixture {
  id: string;
  world_id: string;

  competition: {
    id: string;
    name: string;
  };

  home_club: FixtureClub;
  away_club: FixtureClub;

  matchday: number;
  gameweek: number;

  scheduled_at: string;
  status: string;
}

export interface FixtureClub {
  id: string;
  name: string;
  short: string;
}

export interface Opponent {
  club: FixtureClub;
  country: OpponentCountry;

  is_ai_controlled: boolean;

  manager: Manager;

  reputation: number;
  tier: number;

  form: ClubForm;

  squad_count: number;
  top_players: OpponentPlayer[];
}

export interface OpponentCountry {
  id: string;
  name: string;
  code: string;
}

export interface Manager {
  id: string;
  is_policy_bot: boolean;
}

export interface ClubForm {
  form_string: string;
  current_rating: number;
}

export interface OpponentPlayer {
  id: string;
  person_id: string;

  first_name: string;
  last_name: string;
  display_name: string;

  primary_position: string;
  rating: number;
}
