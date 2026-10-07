import type { ContractView, LedgerEntry } from "~/types"
import type { ManangerFinanceSummary } from "~/types/manager/finances"

export const useFinanceStore = defineStore('finance', () => {
  const summary = ref<ManangerFinanceSummary>()
  const contracts = ref<ContractView[]>()
  const ledger = ref<LedgerEntry[]>([])

  const setSummary = (payload: ManangerFinanceSummary) => summary.value = payload
  const setLedger = (payload: LedgerEntry[]) => ledger.value = payload
  const setContracts = (payload: ContractView[]) => contracts.value = payload

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