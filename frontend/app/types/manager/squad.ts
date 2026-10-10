interface SlotPlayer {
  id?: string
  player_id?: string
  display_name?: string
  squad_number?: number
}

export interface ManagerPlayerSlot {
  slot: number
  position?: string
  player: SlotPlayer
}

export interface ManagerSquadPlayer {
  position?: string
  squad_number?: number
  first_name?: string
  last_name?: string
  overall?: number
  attributes: Record<'goalkeeping' | 'physical' | 'mental' | 'tactical' | 'technical', number>
  player: { id: string, name: string }
  morale?: number
  /** IM62: condition fitness 0–1, the lineup gate and why it failed. */
  fitness?: number
  available?: boolean
  unavailable_reason?: "injured" | "ineligible"
  /** IM40 squad-table columns. */
  nationality?: { code: string, name: string } | null
  age?: number
  contract?: { weekly_wage: number, end_date: string } | null
  squad_role?: SquadRole
  playing_time_pct?: number
  transfer_request_status?: string
  /** IM63: last five rated appearances (1–10), oldest first. */
  recent_ratings?: number[]
  dossier?: { bio?: { market_value: number, status: string } }
}

export type SquadRole = 'key_player' | 'rotation' | 'squad_player' | 'development'

/** GET /clubs/:id/players/:playerID — own-club morale picture + dossier. */
export interface PlayerMoraleDetail {
  player: { id: string, name: string }
  first_name: string
  last_name: string
  position: string
  squad_role: SquadRole
  morale: number
  playing_time_pct: number
  expectations: { label: string, expected: string, current: number, status: 'satisfied' | 'neutral' | 'unhappy' | 'free' }[]
  /** IM64: `why` = morale target vs neutral 50, factors sum to score. */
  explanation: { why?: { subject: string, score: number, factors: { label: string, delta: number }[] } }
  transfer_request?: { id: string, status: string, reason: string, created_at: string, reassured_until?: string }
  relationship_history: { event_type: string, sentiment_delta: number, created_at: string }[]
  dossier: {
    bio: { market_value: number, status: string, secondary_positions: string[] }
    attribute_values: Record<string, Record<string, number>>
    condition: { fatigue: number, fitness: number, sharpness: number } | null
    personality: Record<'professionalism' | 'ambition' | 'loyalty' | 'ego' | 'sociability' | 'adaptability' | 'patience' | 'leadership' | 'emotional_volatility', number> | null
    history?: { appearances: { rating: number | null, minutes: number, goals: number, assists: number }[] }
    private?: {
      contracts: { weekly_wage: number, end_date: string, release_clause: number | null, playing_time_promise: string | null, status: string }[]
      emotional_states: { emotional_state: string, cause: string, intensity: number, occurred_at: string, expires_at: string | null }[]
    }
  }
}

/** GET /clubs/:id/dynamics, the slice the squad panel reads. */
export interface SquadDynamics {
  factions: { label: string, cohesion: number, leader?: { id: string, name: string }, members: { id: string, name: string }[] }[]
}
export type PricePreset = 'quick_sale' | 'valuation' | 'hold_out'

/** GET .../transfer-request/preview (IM65). */
export interface RequestPreview {
  market_value: number
  prices: { preset: PricePreset, label: string, multiplier: number, asking_price: number }[]
  options: { action: 'approve' | 'reassure' | 'deny', effects: { label: string, delta: number }[], notes: string[] }[]
}
