import type { CommonCompetitionType, CommonCupStage } from "../common";

export interface ManagerCompetition {
  role: string
  joined_at: string
    competition_type: CommonCompetitionType;
    league?: LeagueView | null; // set only when competition_type === 'league'
    cup?: CupView | null;       // set only when competition_type === 'domestic_cup'
}

export interface CupView {
  competition: Cup;
  season?: SeasonRef | null;
  started: boolean;
  stage: CommonCupStage;
  total_rounds: number;
  current_round: number;
  champion?: unknown | null;   // type comes from cupClubState, not visible here
  next_fixture?: CompetitonFixture | null;
}

interface CompetitonFixture {
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
interface LeagueView {
  competition: League;
  season?: SeasonRef | null;   // absent/null until a season exists
  started: boolean;
  standings?: Standings | null; // null when no season
  next_fixture?: CompetitonFixture | null;
}