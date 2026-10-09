import type { CommonClub } from "./common";

export type CompetitionType = 'league' | 'domestic_cup';
export type CupStage = 'not_started' | 'waiting' | 'playing' | 'eliminated';

interface StandingRow {
  club: CommonClub
  drawn: number
  form: string[]
  goals_against: number
  goals_for: number
  lost: number
  played: number
  points: number
  position: number
  won: number
}

export interface CompetitionStanding {
  rows: StandingRow[]
  season: {
    id: string
    label: string
    number: number
    status: string
  }
}