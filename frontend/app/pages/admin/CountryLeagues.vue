<script lang="ts" setup>
import { useToast } from '~/components/ui/Toast';
import { useAdminOverview } from '~/composables/admin/overview';
import { useAuth } from '~/composables/useAuth';
import type { Country, League, World } from '~/types';

definePageMeta({
  name: 'country-page',
  path: '/admin/world/:id/countries/:countryId'
})

const route = useRoute()
const store = useAdminStore()
const { notify } = useToast()
const { authedFetch } = useAuth()

const worldId = computed(() => route.params.id as string)
const countryId = computed(() => route.params.countryId as string)

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
  news: 'loading',
  seed: 'loaded'
})

const economics = ref()
const freeAgents = ref()
const leagueClubs = ref<{ country: unknown; clubs: ClubRow[] }>()
const countryPlayers = ref()
const leaguePyramids = ref()
const newsStories = ref<NewsStory[]>([])

interface ClubRow {
  id: string
  name: string
  short_name: string
  is_ai_controlled: boolean
  tier: number
  league_id?: string | null
  league_name?: string
  league_tier: number
  squad_size: number
  top_player_name?: string
  top_player_market_value: number
  wage_bill: number
  wage_allocated: number
  wage_committed: number
  transfer_allocated: number
  transfer_committed: number
  crisis_stage?: string | null
}

interface NewsStory {
  id: string
  world_id: string
  headline: string
  body: string
  category: string
  related_event_id?: string | null
  published_at: string
}

const renameTarget = ref<ClubRow | null>(null)
const renameForm = reactive({
  name: '',
  short_name: '',
  headline: '',
  body: ''
})
const renameBusy = ref(false)
const renameError = ref('')

function openRename(club: ClubRow) {
  renameTarget.value = club
  renameForm.name = club.name
  renameForm.short_name = club.short_name
  renameForm.headline = ''
  renameForm.body = ''
  renameError.value = ''
}

async function submitRename() {
  const club = renameTarget.value
  if (!club) return
  renameBusy.value = true
  renameError.value = ''
  try {
    const res = await authedFetch(
      `/api/admin/worlds/${worldId.value}/countries/${countryId.value}/clubs/${club.id}`,
      {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: renameForm.name,
          short_name: renameForm.short_name,
          news: { headline: renameForm.headline, body: renameForm.body }
        })
      }
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err?.error ?? 'Rename failed')
    }
    notify({ title: 'Club renamed', description: 'New name published with the news story.', type: 'success' })
    renameTarget.value = null
    await Promise.all([loadCountryClubs(), loadNews()])
  } catch (e) {
    renameError.value = e instanceof Error ? e.message : String(e)
  } finally {
    renameBusy.value = false
  }
}

async function loadLeaguePyramid() {
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

async function loadNews() {
  try {
    status.news = 'loading'
    const res = await authedFetch(`/api/admin/worlds/${worldId.value}/news`)
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err?.error ?? 'Failed to load news')
    }
    const body = await res.json()
    newsStories.value = body.stories ?? []
    status.news = 'loaded'
  } catch (error) {
    console.error(error)
    status.news = 'error'
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
  await Promise.all([loadLeaguePyramid(), loadCountryClubs(), loadAllPlayersFromCountry(), loadFreeAgents(), loadCountryEconomics(), loadNews()])
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

    <div class="mt-6 space-y-10">
      <section>
        <h3 class="text-lg font-semibold mb-3">Clubs ({{ leagueClubs?.clubs?.length ?? 0 }})</h3>
        <div v-if="status.clubs === 'loading'" class="text-slate-400">Loading clubs…</div>
        <div v-else-if="!leagueClubs?.clubs?.length" class="text-slate-400">No clubs in this country yet.</div>
        <div v-else class="overflow-x-auto border border-slate-700 rounded-lg">
          <table class="w-full text-sm">
            <thead class="text-slate-500 text-left bg-slate-800">
              <tr>
                <th class="py-2 px-3">Club</th>
                <th class="py-2 px-3">Code</th>
                <th class="py-2 px-3">Type</th>
                <th class="py-2 px-3">League</th>
                <th class="py-2 px-3">Squad</th>
                <th class="py-2 px-3">Wage bill</th>
                <th class="py-2 px-3"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="club in leagueClubs.clubs" :key="club.id" class="border-t border-slate-700">
                <td class="py-2 px-3 font-medium text-white">{{ club.name }}</td>
                <td class="py-2 px-3 uppercase">{{ club.short_name }}</td>
                <td class="py-2 px-3">{{ club.is_ai_controlled ? 'AI' : 'Human' }}</td>
                <td class="py-2 px-3">{{ club.league_name || '—' }}</td>
                <td class="py-2 px-3">{{ club.squad_size }}</td>
                <td class="py-2 px-3">{{ club.wage_bill.toLocaleString() }}</td>
                <td class="py-2 px-3 text-right">
                  <button @click="openRename(club)" class="text-indigo-400 hover:text-indigo-300">Rename</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h3 class="text-lg font-semibold mb-3">World news</h3>
        <div v-if="status.news === 'loading'" class="text-slate-400">Loading news…</div>
        <div v-else-if="!newsStories.length" class="text-slate-400">No news stories yet — renaming a club publishes one.</div>
        <ul v-else class="space-y-3">
          <li v-for="story in newsStories" :key="story.id" class="border border-slate-700 rounded-lg p-4">
            <div class="flex items-baseline justify-between gap-3">
              <h4 class="font-medium text-white">{{ story.headline }}</h4>
              <span class="text-xs text-slate-500">{{ new Date(story.published_at).toLocaleString() }}</span>
            </div>
            <p class="text-slate-400 text-sm mt-1">{{ story.body }}</p>
          </li>
        </ul>
      </section>
    </div>

    {{ status }}
  </section>

  <div v-if="renameTarget" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50" @click.self="renameTarget = null">
    <div class="bg-slate-800 border border-slate-600 rounded-lg p-6 w-full max-w-lg">
      <h3 class="text-xl font-semibold text-white mb-1">Rename club</h3>
      <p class="text-slate-400 text-sm mb-4">{{ renameTarget.name }} — the rename publishes a news story announcing it.</p>
      <div class="space-y-3">
        <div>
          <label class="block text-sm text-slate-400 mb-1">New name</label>
          <input v-model="renameForm.name" type="text" class="w-full bg-slate-900 border border-slate-600 rounded px-3 py-2" />
        </div>
        <div>
          <label class="block text-sm text-slate-400 mb-1">Short code <span class="text-slate-500">(optional — derived if blank)</span></label>
          <input v-model="renameForm.short_name" type="text" maxlength="3" class="w-28 bg-slate-900 border border-slate-600 rounded px-3 py-2 uppercase" />
        </div>
        <div class="border-t border-slate-700 pt-3">
          <p class="text-sm text-slate-400 mb-2">News story (announces the change in-world)</p>
          <div class="space-y-3">
            <div>
              <label class="block text-sm text-slate-400 mb-1">Headline</label>
              <input v-model="renameForm.headline" type="text" placeholder="e.g. Phoenix rise from the ashes" class="w-full bg-slate-900 border border-slate-600 rounded px-3 py-2" />
            </div>
            <div>
              <label class="block text-sm text-slate-400 mb-1">Body</label>
              <textarea v-model="renameForm.body" rows="3" placeholder="The club rebrands ahead of the new season…" class="w-full bg-slate-900 border border-slate-600 rounded px-3 py-2"></textarea>
            </div>
          </div>
        </div>
        <p v-if="renameError" class="text-red-400 text-sm">{{ renameError }}</p>
        <div class="flex justify-end gap-3 pt-2">
          <button @click="renameTarget = null" class="bg-slate-700 hover:bg-slate-600 text-white rounded px-4 py-2">Cancel</button>
          <button :disabled="renameBusy" @click="submitRename" class="bg-indigo-600 hover:bg-indigo-500 text-white rounded px-4 py-2">
            {{ renameBusy ? 'Renaming…' : 'Rename & publish' }}
          </button>
        </div>
      </div>
    </div>
  </div>

  <NuxtPage />
</template>