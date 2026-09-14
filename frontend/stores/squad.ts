export interface LineupSlot {
  slot: number
  position: string
  player_id: string
}

export interface LineupView {
  club_id: string
  style: string
  formation: string
  slots: LineupSlot[]
}

// Pinia store: squad.ts — squad list, lineup editor, player detail
export const useSquadStore = defineStore('squad', {
  state: () => ({
    players: [] as Record<string, unknown>[],
    lineup: null as LineupView | null,
    saving: false,
  }),

  actions: {
    async fetchSquad(clubId: string) {
      const { authedFetch } = useAuth()
      // Club detail is the current server-owned squad read model. Keeping this
      // through authedFetch preserves the httpOnly-cookie session contract.
      const response = await authedFetch(`/api/clubs/${clubId}`)
      if (!response.ok) throw new Error('Could not load squad.')
      const detail = await response.json() as { squad?: Record<string, unknown>[] }
      this.players = detail.squad ?? []
    },

    async fetchLineup(clubId: string) {
      const { authedFetch } = useAuth()
      const response = await authedFetch(`/api/clubs/${clubId}/lineup`)
      if (!response.ok) throw new Error('Could not load lineup.')
      this.lineup = await response.json() as LineupView
    },

    async saveLineup(clubId: string, slots: { slot: number; player_id: string }[]) {
      const { authedFetch } = useAuth()
      this.saving = true
      try {
        const response = await authedFetch(`/api/clubs/${clubId}/lineup`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ slots }),
        })
        if (!response.ok) {
          const body = await response.json().catch(() => null)
          throw new Error(body?.error ?? 'Could not save lineup.')
        }
        await this.fetchLineup(clubId)
      } finally {
        this.saving = false
      }
    },
  },
})