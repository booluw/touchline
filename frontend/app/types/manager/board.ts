import type { CommonClub } from "../common"

export interface ManagerBoard {
  confidence: number
  snapshot: {
    manager_id: string
    club: CommonClub,
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