// Pinia store: squad.ts — squad list, player detail, morale, playing time
export const useSquadStore = defineStore('squad', {
  state: () => ({
    players: [] as Record<string, unknown>[],
  }),

  actions: {
    async fetchSquad(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/clubs/${clubId}/squad`)
      this.players = data.value as Record<string, unknown>[] ?? []
    },
  },
})