import type { ContractView, FinanceSummary, LedgerEntry } from "~/types"

export const useFinanceStore = defineStore('finance', () => {
  const summary = ref<FinanceSummary>()
  const contracts = ref<ContractView>()
  const ledger = ref<LedgerEntry>()

  const setSummary = (payload: FinanceSummary) => summary.value = payload
  const setLedger = (payload: LedgerEntry) => ledger.value = payload
  const setContracts = (payload: ContractView) => contracts.value = payload

  return {
    summary,
    contracts,
    ledger,
    setSummary,
    setLedger,
    setContracts,
  }
}, {
  persist: true
})