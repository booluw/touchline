<script lang="ts" setup>
import { useToast } from '~/components/ui/Toast';
import { useAdminOverview } from '~/composables/admin/overview';
import type { Country, League, World } from '~/types';

definePageMeta({
  name: 'country-page',
  path: '/admin/world/:id/countries/:countryId'
})

const route = useRoute()
const store = useAdminStore()
const { notify } = useToast()

const worldId = computed(() => route.params.id)
const countryId = computed(() => route.params.countryId)

const {
  countryLeaguePyramid,
  countryClubs,
  playersFromCountry,
  countryFreeAgents,
  countryEconomics,
  seedWorld,
  seedWorldStatus
} = useAdminOverview({ worldId: worldId.value, countryId: countryId.value })

const world = computed(() => store.worlds?.find((w: World) => w.id === worldId.value ))
const country = computed(() => store.countries?.find((c: Country) => c.id === countryId.value))
const leagues = computed(() => store.leagues?.filter((l: League) => l.country_id === countryId.value) ?? [])

const status = reactive<Record<string, 'loading' | 'error' | 'loaded'>>({
  pyramid: 'loading',
  clubs: 'loading',
  players: 'loading',
  agents: 'loading',
  economics: 'loading',
  seed: 'loaded'
})

const economics = ref()
const freeAgents = ref()
const leagueClubs = ref()
const countryPlayers = ref()
const leaguePyramids = ref()

async function loadLeaguePyramid() {
  console.log("Hello")
  try {
    status.pyramid = 'loading'
    leaguePyramids.value = await countryLeaguePyramid()
    status.pyramid = 'loaded'
  } catch (error) {
    console.error(error)
    status.pyramid = 'error'
  }
}

async function loadCountryClubs() {
  try {
    status.clubs = 'loading'
    leagueClubs.value = await countryClubs()
    status.clubs = 'loaded'
  } catch (error) {
    console.error(error)
    status.clubs = 'error'
  }
}

async function loadAllPlayersFromCountry() {
  try {
    status.players = 'loading'
    countryPlayers.value = await playersFromCountry()
    status.players = 'loaded'
  } catch (error) {
    console.error(error)
    status.players = 'error'
  }
}

async function loadFreeAgents() {
  try {
    status.agents = 'loading'
    freeAgents.value = await countryFreeAgents()
  } catch (error) {
    console.error(error)
    status.agents = 'error'
  }
}

async function loadCountryEconomics() {
  try {
    status.economics = 'loading'
    economics.value = await countryEconomics()
    status.economics = 'loaded'
  } catch (error) {
    console.error(error)
    status.economics = 'error'
  }
}

async function seedWorldAndCountry() {
  try {
    status.seed = 'loading'
    const resp = await seedWorld()
    notify({
      title: 'World Seeded',
      description: 'World has been seeded',
      type: 'success'
    })

    console.log(resp)
    getSeedStatus()
    initCountryDashboard()
    status.seed = 'loaded'
  } catch (error) {
    status.seed = 'error'
    console.log(error)
  }
}

async function getSeedStatus() {
  const timeout = setInterval(async () => {
    const { world_seeded } = await seedWorldStatus()

    if (world_seeded) {
      clearInterval(timeout)
      alert("Seeded")
    }
  }, 30000);
}

async function initCountryDashboard() {
  await Promise.all([loadLeaguePyramid(), loadCountryClubs(), loadAllPlayersFromCountry(), loadFreeAgents(), loadCountryEconomics()])
}

onMounted(() => initCountryDashboard())
</script>
<template>
  <section>
    <div class="flex justify-between items-center">
      <div class="flex flex-col gap-3 items-start mb-5">
        <span class="pill pill--success">{{ world.status }}</span>
        <h2 class="page__header">{{ country.name }} <span class="uppercase">[{{ country.code }}]</span>, {{ world.name }}</h2>
      </div>

      <div class="flex gap-5">
        <button class="button" @click="seedWorldAndCountry()">
          Seed Country
        </button>
        <nuxt-link
          :to="{ name: 'admin-CountryLeagues-create', params: { id: worldId, countryId }}"
          class="button button--outline"
        >
          Create New League
        </nuxt-link>
      </div>
    </div>

    <div class="border-b">
      {{ leaguePyramids }}
    </div>

    {{ status }}
  </section>
  <NuxtPage />
</template>