import type { World, Country } from "~/types"

export const useAdminStore = defineStore('admin', () => {
  const worlds = ref<World[]>()
  const countries = ref<Country[]>()
  const leagues = ref<League[]>()

  const addWorld = (payload: World) => worlds.value.push(payload)
  const setWorlds = (payload: World[]) => worlds.value = payload

  const addCountry = (payload: Country) => countries.value.push(payload)
  const setCountries = (payload: Country[]) => countries.value = payload

  const addLeague = (payload: League) => leagues.value.push(payload)
  const setLeagues = (payload: League[]) => leagues.value = payload

  return {
    worlds,
    countries,
    leagues,
    addWorld,
    setWorlds,
    addCountry,
    setCountries,
    addLeague,
    setLeagues
  }
}, {
  persist: {
    key: 'admin-v2',
  },
})