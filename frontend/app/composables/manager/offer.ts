import { useToast } from "~/components/ui/Toast"

export function useManagerOffer() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  async function getOffers() {
    try {
      return await $api.get(`${apiBase}/api/managers/me/offers`)
    } catch (error) {
      console.error(error)
      notify({
        type: "danger",
        description: "An Error Occurred While fetching Offers",
        title: "Error"
      })

      throw error
    }
  }

  async function acceptOffer(jobId: string) {
    try {
      await $api.post(`${apiBase}/api/offers/${jobId}/accept`, {})
      return
    } catch (error) {
      console.log(error)
      notify({
        title: "An Error Occurred while accepting offer",
        type: "danger"
      })

      throw error
    }
  }

  async function rejectOffer(jobId: string) {
    try {
      await $api.post(`${apiBase}/api/offers/${jobId}/decline`, {})
      return
    } catch (error) {
      console.log(error)
      notify({
        title: "An Error Occurred while rejecting offer",
        type: "danger"
      })

      throw error
    }
  }

  return {
    getOffers,
    acceptOffer,
    rejectOffer
  }
}