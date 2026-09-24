import { useToast } from "~/components/ui/Toast"

export function useManagerDashboard() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  const clubStore = useClubStore()

  async function getDashboardData() {
    try {
      return $api.get(`${apiBase}/api/dashboard`)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred while fetching Dashboard",
        type: "danger"
      })

      throw error
    }
  }

  async function getBoardStatus() {
    try {
      const resp = await $api.get(`${apiBase}/api/managers/me/board`)
      clubStore.setBoard(resp)
    } catch (error) {
      console.error(error)
      notify({
        title: "Error",
        description: "An Error Occurred",
        type: "danger"
      })

      throw error
    }
  }

  return {
    getDashboardData,
    getBoardStatus
  }
}