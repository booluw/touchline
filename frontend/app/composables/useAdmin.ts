/* eslint-disable @typescript-eslint/no-explicit-any */
import { useToast } from "~/components/ui/Toast"
import type { World } from "~/types"

export function useAdmin() {
  const { public: { apiBase } } = useRuntimeConfig()
  const { $api } = useNuxtApp()
  const store = useAdminStore()
  const { notify } = useToast()

  async function createWorld(payload: { name: string }) {
    try {
      const resp = await $api.post<World>(`${apiBase}/api/admin/worlds`, payload)
      store.addWorld(resp)
    } catch (error: any | unknown) {
      notify({
        type: 'danger',
        description: error.string(),
        title: 'An error occurred'
      })
      console.error(error)
    }
  }

  async function fetchWorlds() {
    try {
      const resp = await $api.get<World[]>(`${apiBase}/api/admin/worlds`)
      store.setWorlds(resp)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error.string(),
        title: 'An error occurred'
      })
    }
  }

  async function fetchCountry(world_id: string) {
    try {
      const { countries } = await $api.get<Country[]>(`${apiBase}/api/admin/countries?world_id=${world_id}`)
      store.setCountries(countries)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error.string(),
        title: 'An error occurred'
      })
    }
  }

  async function createCountry(payload: { name: string, code: string, world_id: string }) {
    try {
      const resp = await $api.post<Country>(`${apiBase}/api/admin/countries`, payload)
      store.addCountry(resp)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        title: 'An error occurred',
        description: error.string(),
        type: 'danger'
      })
    }
  }

  async function fetchWorldLeagues(world_id: string) {
    try {
      const { leagues } = await $api.get<League[]>(`${apiBase}/api/admin/leagues?world_id=${world_id}`)
      store.setLeagues(leagues)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error.string(),
        title: 'An error occurred'
      })
    }
  }

  async function createLeague(payload: League) {
    try {
      const resp = await $api.post<League>(`${apiBase}/api/admin/leagues`, payload)
      store.addLeague(resp)
    } catch (error: any | unknown) {
      console.error(error)
      notify({
        type: 'danger',
        description: error,
        title: 'An error occurred'
      })
    }
  }

  return {
    createWorld,
    fetchWorlds,
    fetchCountry,
    createCountry,
    fetchWorldLeagues,
    createLeague
  }
}