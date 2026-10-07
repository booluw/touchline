import type { ManagerNextFixture } from "~/types/manager"

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
      toast.error("Error")
    }
  }

  return {
    getNextFixture
  }
}