export interface LineupSlotPlayer {
  id: string
  name?: string
}

export interface LineupSlot {
  slot: number
  position: string
  player: LineupSlotPlayer | null
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
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()
      // Club detail is the current server-owned squad read model. Keeping this
      // through authedFetch preserves the httpOnly-cookie session contract.
      const detail = await $api.get(`${apiBase}/api/clubs/${clubId}`)
      this.players = detail.squad ?? []
    },

    async fetchLineup(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()

      this.lineup = await $api.get(`${apiBase}/api/clubs/${clubId}/lineup`)
    },

    async saveLineup(clubId: string, slots: { slot: number; player_id: string }[]) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { $api } = useNuxtApp()
      
      this.saving = true
      try {
        const response = await $api.put(`${apiBase}/api/clubs/${clubId}/lineup`, { slots })
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