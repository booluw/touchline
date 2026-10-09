import type { CommonClub, CommonCompetitionType, CommonCupStage } from "../common";
import type { ManagerFixture } from "./fixtures";

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

export interface OutlookCup { id: string, name: string, scope: string }

export interface ManagerClubOutlook {
  season: { id: string, label: string, number: number, status: string }
  club: CommonClub
  position: number
  points: number
  games_left: number
  finish: { best: number, worst: number }
  stakes: OutlookRace | null
  races: OutlookRace[]
  attachments: OutlookRace[]
  guaranteed_at_least: OutlookCup | null
  next_match: {
    fixture: ManagerFixture
    if_win: number
    if_draw: number
    if_loss: number
  } | null
  projection: {
    position: number
    range: [number, number]
    runs: number
    factors: { label: string, delta: number, detail: string }[]
  }
}

export interface OutlookRace {
  kind: "title" | "promotion" | "relegation" | "qualification"
  cup?: OutlookCup
  from_position: number
  to_position: number
  status: "clinched" | "alive" | "eliminated"
  inside: boolean
  gap: number
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