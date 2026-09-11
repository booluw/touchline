// Pinia store: finance.ts — ledger, cash vs budget breakdown (PRD section 36)
export const useFinanceStore = defineStore('finance', {
  state: () => ({
    ledger: [] as Record<string, unknown>[],
    summary: null as null | Record<string, unknown>,
  }),

  actions: {
    async fetchLedger(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/finance/ledger?clubId=${clubId}`)
      this.ledger = data.value as Record<string, unknown>[] ?? []
    },
    async fetchSummary(clubId: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/finance/summary?clubId=${clubId}`)
      this.summary = data.value as Record<string, unknown> | null
    },
  },
})