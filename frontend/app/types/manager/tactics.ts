import type { CommonClub } from "../common"

export type TacticStyle = "balanced" | "possession" | "gegenpress" | "low_block" | "direct"

/** IM61 team instructions; the middle option of each dial is neutral. */
export interface TeamInstructions {
  mentality: "cautious" | "balanced" | "positive"
  pressing: "low" | "mid" | "high"
  width: "narrow" | "normal" | "wide"
  tempo: "patient" | "normal" | "direct"
}

export interface ClubTactics {
  club: CommonClub
  style: TacticStyle
  formation: string
  allowed_formations: string[]
  instructions: TeamInstructions
  updated_at?: string
}

export interface TacticalAdvice {
  fixture_id: string
  scheduled_at: string
  home_or_away: "home" | "away"
  opponent: CommonClub
  profile: {
    style: TacticStyle
    matches: number
    goals_for: number
    goals_against: number
    xg_for?: number
    xg_against?: number
  }
  headline: string
  summary: string
  suggested: TeamInstructions
  current: TeamInstructions
  fit: { score: number, factors: { label: string, delta: number }[] }
}

export interface LineupSlotView {
  slot: number
  position: string
  player?: { id: string, name: string }
  player_position?: string
  fit?: number
}

export interface ClubLineup {
  style: TacticStyle
  formation: string
  slots: LineupSlotView[]
}
