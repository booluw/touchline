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

export interface LeagueRef {
  id: string,
  name?: string,
}

export interface League {
  id: string,
  world_id: string,
  country: {
    id: string,
    name?: string,
    code?: string,
  },
  name: string,
  tier: number,
  team_count: number,
  status: string,
  promotions: number,
  relegations: number,
  promotes_to: LeagueRef | null,
  relegates_to: LeagueRef | null,
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

export interface CountryStats {
  country: {
    id: string;
    world_id: string;
    code: string;
    name: string;
  };
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

interface OpenListing {
  listing_id: string;
  player: {
    id: string;
    name: string;
  };
  position: string;
  listing_club: Club;
  asking_price: number;
  market_value: number;
  listing_type: string;
  listed_at: string;
}

interface Bid {
  bid_id: string;
  player: {
    id: string;
    name: string;
  };
  bidding_club: Club;
  selling_club: Club;
  fee: number;
  status: string;
  created_at: string;
}

interface Transfer {
  transfer_id: string;
  player: {
    id: string;
    name: string;
  };
  from_club: Club;
  to_club: Club;
  fee: number;
  market_value: number;
  overpay: boolean;
  overpay_pct: number;
  completed_at: string;
}

export interface Club {
  id: string;
  name: string;
  short: string;
  reputation: number
  tier: number
}

