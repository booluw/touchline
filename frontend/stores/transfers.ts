// Pinia store: transfers.ts — transfer listings, bids, negotiations
export const useTransfersStore = defineStore('transfers', {
  state: () => ({
    listings: [] as Record<string, unknown>[],
    incomingBids: [] as Record<string, unknown>[],
  }),

  actions: {
    async fetchListings() {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/transfers/listings`)
      this.listings = data.value as Record<string, unknown>[] ?? []
    },
    async placeBid(clubId: string, playerId: string, amount: number) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/transfers/bids`, {
        method: 'POST',
        body: { clubId, playerId, amount },
      })
      return data.value
    },
    async respondToBid(bidId: string, response: string) {
      const { public: { apiBase } } = useRuntimeConfig()
      const { data } = await useFetch(`${apiBase}/api/transfers/bids/${bidId}/respond`, {
        method: 'POST',
        body: { response },
      })
      return data.value
    },
  },
})