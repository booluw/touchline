// Pinia store: club.ts — current club identity, DNA, board, facilities
export const useClubStore = defineStore('club', {
  state: () => ({
    club: null as null | Record<string, unknown>,
    clubDna: null as null | Record<string, unknown>,
    boardConfidence: null as null | number,
  }),

  actions: {
    async fetchClub(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch<Record<string, unknown>>(`${apiBase}/api/clubs/${clubId}`, {
        deep: true
      })
      this.club = data.value ?? null
    },
    async fetchBoardConfidence(clubId: string) {
      // Explanation object returned with the score (PRD section 54)
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch<Record<string, unknown>>(`${apiBase}/api/clubs/${clubId}/board`, {
        deep: true
      })
      this.boardConfidence = (data.value as Record<string, unknown> | null)?.score as number ?? null
    },
  },
})