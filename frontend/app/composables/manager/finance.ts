export function useManagerFinance() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const toast = useToaster()

  const finstore = useFinanceStore()
  const clubstore = useClubStore()

  const clubId = clubstore.club!.id

  async function getFinancialSummary() {
    try {
      const summary = await $api.get(`${apiBase}/api/clubs/${clubId}/finances`)
      finstore.setSummary(summary)
    } catch (error) {
      console.error(error)
      toast.error("Error")
    }
  }

  return {
    getFinancialSummary
  }
}