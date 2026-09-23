import { useToast } from "~/components/ui/Toast"

export function useManagerDashboard() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

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

  return {
    getDashboardData
  }
}