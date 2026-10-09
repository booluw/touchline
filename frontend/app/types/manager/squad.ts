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
}