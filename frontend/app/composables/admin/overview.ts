/* eslint-disable @typescript-eslint/no-explicit-any */
import { useToast } from "~/components/ui/Toast"

export function useAdminOverview({ worldId, countryId }: { worldId: string, countryId: string }) {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const { notify } = useToast()

  async function countryLeaguePyramid() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/pyramid`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  async function countryOverview() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/overview`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  async function countryClubs() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/clubs`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  async function playersFromCountry() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/players`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  async function countryFreeAgents() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/free-agents`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  async function countryEconomics() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/finance`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  async function countryTransferMarket() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/countries/${countryId}/market`)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }
  
  async function seedWorld() {
    try {
      return await $api.post(`${apiBase}/api/admin/worlds/${worldId}/seed`, {})
    } catch (error: any | unknown) {
      console.clear()
      console.error(error)
      notify({
        title: 'An error occurred',
        description: error,
        type: 'danger',
      })
    }
  }

  async function seedWorldStatus() {
    try {
      return await $api.get(`${apiBase}/api/admin/worlds/${worldId}/seed-status`)
    } catch (error) {
      console.error(error)
    }
  }

  return {
    countryLeaguePyramid,
    countryClubs,
    playersFromCountry,
    countryFreeAgents,
    countryEconomics,
    seedWorld,
    seedWorldStatus,
    countryOverview,
    countryTransferMarket
  }
}