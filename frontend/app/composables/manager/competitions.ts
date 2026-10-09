import type { CompetitionStanding } from "~/types"

export function useManagerCompetition() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const toast = useToaster()

  const clubstore = useClubStore()


  async function getLeagueTable(id: string) {
    try {
      return await $api.get<CompetitionStanding>(`${apiBase}/api/competitions/${id}/standings`)
    } catch (error) {
      toast.apiError(error)
    }
  }

  async function getCompetitions() {
    try {
      const resp = await $api.get(`${apiBase}/api/managers/me/competitions`)
      clubstore.setCompetitions(resp.competitions)
    } catch (error) {
      toast.apiError(error)
    }
  }


  return {
    getLeagueTable,
    getCompetitions
  }
}