import { useToast } from "~/components/ui/Toast"
import type { ContractView, FinanceSummary, LedgerEntry, NextFixture } from "~/types"


export function useClub() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  const store = useFinanceStore()
  const clubStore = useClubStore()

  const clubId = clubStore.club?.id

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

  async function getCompetitions(): Promise<void> {
    try {
      const { competitions } = await $api.get(`${apiBase}/api/managers/me/competitions`)
      clubStore.setCompetitions(competitions)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred While Loading Competitions",
        type: "danger"
      })

      throw error
    }
  }

  async function getFixtures(): Promise<void> {
    try {
      const fixtures = await $api.get(`${apiBase}/api/clubs/${clubId}/fixtures`)
      clubStore.setFixtures(fixtures)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred While Loading Fixtures",
        type: "danger"
      })

      throw error
    }
  }

  async function getNextFixture(): Promise<NextFixture> {
    try {
      const { next_fixture } = await $api.get<{ next_fixture: NextFixture }>(`${apiBase}/api/clubs/${clubId}/next-fixture`)
      return next_fixture
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Ocurred While Loading Next Fixture",
        type: "danger"
      })
      throw error
    }
  }

  async function getDynamics() {
    try {
      return await $api.get(`${apiBase}/api/clubs/${clubId}/dynamics`)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred while fetching Squad Dynamics",
        type: "danger"
      })

      throw error
    }
  }

  // async

  return {
    getFinance,
    getCompetitions,
    getFixtures,
    getNextFixture,
    getDynamics
  }
}