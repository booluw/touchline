// Pinia store: squad.ts — squad list, player detail, morale, playing time
export const useSquadStore = defineStore('squad', {
  state: () => ({
    players: [] as Record<string, unknown>[],
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
  },
})
