import type { ManagerFixture, ManagerNextFixture } from "~/types/manager"

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
      return await $api.get<{ fixture: ManagerFixture[] }>(`${apiBase}/api/clubs/${clubId}/fixtures?upcoming=true&limit=${n}`)
    } catch (error) {
      console.error(error)
      toast.apiError(error)
    }
  }

  return {
    getNextFixture,
    getNUpcomingFixtures
  }
}