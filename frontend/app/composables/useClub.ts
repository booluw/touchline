import { useToast } from "~/components/ui/Toast"
import type { ContractView, FinanceSummary, LedgerEntry } from "~/types"


export function useClub() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  const store = useFinanceStore()
  const authStore = useAuthStore()

  const clubId = computed(() => authStore.club?.id).value

  async function getFinance(): Promise<void> {
    try {
      const [summary, ledger, contract] = await Promise.all([
        $api.get<FinanceSummary>(`${apiBase}/api/clubs/${clubId}/finances`),
        $api.get<LedgerEntry>(`${apiBase}/api/clubs/${clubId}/ledger`),
        $api.get<ContractView[]>(`${apiBase}/api/clubs/${clubId}/contracts`)
      ])

      store.setContracts(contract)
      store.setLedger(ledger)
      store.setSummary(summary)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred While Loading Finance",
        type: "danger"
      })

      throw error
    }
  }

  return {
    getFinance
  }
}