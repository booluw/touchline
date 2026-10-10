import type { ManagerFixture, ManagerNextFixture, MatchSummary, SeasonFixture } from "~/types/manager"

export function useClubFixtures() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const toast = useToaster()

  const clubstore = useClubStore()
  const clubId = clubstore.club!.id

  async function getNextFixture() {
    try {
      const { next_fixture } = await $api.get<{ next_fixture: ManagerNextFixture}>(`${apiBase}/api/clubs/${clubId}/next-fixture`)
      clubstore.setNextFixture(next_fixture)
    } catch (error) {
      console.error(error)
      toast.apiError(error)
    }
  }

  async function getNUpcomingFixtures(n: number) {
    try {
      return await $api.get<{ fixtures: ManagerFixture[] }>(`${apiBase}/api/clubs/${clubId}/fixtures?upcoming=true&limit=${n}`)
    } catch (error) {
      console.error(error)
      toast.apiError(error)
    }
  }

  async function getSeasonFixtures() {
    try {
      const { fixtures } = await $api.get<{ fixtures: SeasonFixture[] }>(`${apiBase}/api/clubs/${clubId}/fixtures?season=current`)
      return fixtures
    } catch (error) {
      console.error(error)
      toast.apiError(error)
    }
  }

  async function getMatchSummary(matchId: string) {
    try {
      return await $api.get<MatchSummary>(`${apiBase}/api/matches/${matchId}/events`)
    } catch (error) {
      console.error(error)
      toast.apiError(error)
    }
  }

  return {
    getNextFixture,
    getNUpcomingFixtures,
    getSeasonFixtures,
    getMatchSummary,
  }
}