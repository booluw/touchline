import { useToast } from "~/components/ui/Toast"

/**
 * One player's detail card (IM20). The wire shape is the backend's
 * `PlayerDetail`: identity, current club, the six attribute-category means plus
 * the position-weighted `overall` the squad screen shows, the career record, and
 * — only for the caller's own club — the weekly wage.
 */
export interface PlayerRef {
  id: string
  name?: string
}

export interface ClubRef {
  id: string
  name?: string
}

export interface PlayerAttributes {
  technical: number
  physical: number
  mental: number
  tactical: number
  goalkeeping: number
  positional: number
}

export interface PlayerCareer {
  appearances: number
  goals: number
  assists: number
  average_rating: number
}

export interface PlayerDetail {
  player: PlayerRef
  club?: ClubRef
  first_name: string
  last_name: string
  display_name: string
  nationality: string
  date_of_birth: string
  position: string
  squad_number?: number
  attributes: PlayerAttributes
  overall: number
  career: PlayerCareer
  /** Omitted by the API unless the player is at the caller's own club. */
  weekly_wage?: number
}

export const playerAttributeCategories: { key: keyof PlayerAttributes; label: string }[] = [
  { key: "technical", label: "TECH" },
  { key: "physical", label: "PHY" },
  { key: "mental", label: "MEN" },
  { key: "tactical", label: "TAC" },
  { key: "goalkeeping", label: "GK" },
  { key: "positional", label: "POS" }
]

export function usePlayer() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  /**
   * Reads one player of the caller's own world. Any player of the world is
   * readable — a cup opponent or a signing target included; the wage is the one
   * field the API withholds when the player is not at the caller's own club.
   */
  async function getPlayer(playerId: string): Promise<PlayerDetail> {
    try {
      return await $api.get<PlayerDetail>(`${apiBase}/api/players/${playerId}`)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred While Loading The Player",
        type: "danger"
      })

      throw error
    }
  }

  return {
    getPlayer
  }
}
