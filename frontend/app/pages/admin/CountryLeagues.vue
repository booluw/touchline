<script lang="ts" setup>
import { useToast } from '~/components/ui/Toast';
import { useAdminOverview } from '~/composables/admin/overview';
import { useAuth } from '~/composables/useAuth';
import type { Country, CountryClubs, CountryStats, League, World, CountryMarketData } from '~/types';
import { formatMoney, formatMoneyCompact } from '../../utils/helpers';

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
  seedWorld,
  seedWorldStatus,
  countryOverview,
  countryTransferMarket
} = useAdminOverview({ worldId: worldId.value, countryId: countryId.value })

const world = computed(() => store.worlds?.find((w: World) => w.id === worldId.value))
const country = computed(() => store.countries?.find((c: Country) => c.id === countryId.value))

const status = reactive<Record<string, 'loading' | 'error' | 'loaded'>>({
  pyramid: 'loading',
  clubs: 'loading',
  players: 'loading',
  news: 'loading',
  seed: 'loaded',
  overview: 'loading',
  market: 'loading'
})

const leagueClubs = ref<CountryClubs>()
const countryPlayers = ref()
const leaguePyramids = ref()
const overview = ref<CountryStats>()
const market = ref<CountryMarketData>()
const newsStories = ref<NewsStory[]>([])

interface ClubRow {
  id: string
  name: string
  short_name: string
  is_ai_controlled: boolean
  tier: number
  league?: { id: string; name: string } | null
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

async function loadCountryOverview() {
  try {
    status.overview = 'loading'
    overview.value = await countryOverview()
    status.overview = 'loaded'
  } catch (error) {
    console.error(error)
    status.overview = 'error'
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

async function loadCountryMarkets() {
  try {
    status.market = 'loading'
    market.value = await countryTransferMarket()
    status.market = 'loaded'
  } catch (error) {
    console.error(error)
    status.market = 'error'
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
  await loadCountryOverview()
  await Promise.all([
    loadLeaguePyramid(),
    loadCountryClubs(),
    loadAllPlayersFromCountry(),
    loadNews(),
    loadCountryMarkets()
  ])
}

onMounted(() => initCountryDashboard())
</script>
<template>
  <section>
    <div class="flex justify-between items-center">
      <div class="flex flex-col gap-3 items-start mb-5">
        <span class="pill pill--success">{{ world!.status }}</span>
        <h2 class="page__header">{{ country!.name }} <span class="uppercase">[{{ country!.code }}]</span>, {{
          world!.name }}</h2>
      </div>
    </div>

    <section class="grid gap-5 grid-cols-4 grid-rows-3">
      <div class="col-span-2 bg-void-900 border-brutal border-cyan-300 p-5">
        <h3 class="heading heading--small">economics</h3>
        <UiLoader v-if="status.overview === 'loading'" />
        <div v-else-if="status.overview === 'loaded'" class="grid grid-cols-4 gap-5 mt-5">
          <div class="row-span-2 flex flex-col justify-center">
            <h3 class="heading heading--big">{{ formatMoneyCompact(overview!.economy.cash) }}</h3>
            <h4 class="heading heading--small">cash</h4>
          </div>

          <div class="">
            <h3 class="heading">{{ formatMoney(overview!.economy.wage_allocated) }}</h3>
            <h4 class="heading heading--small">wage allocation</h4>
          </div>

          <div class="">
            <h3 class="heading">{{ formatMoney(overview!.economy.wage_bill) }}</h3>
            <h4 class="heading heading--small">total wage</h4>
          </div>

          <div class="">
            <h3 class="heading">{{ formatMoney(overview!.economy.wage_committed) }}</h3>
            <h4 class="heading heading--small">wage committed</h4>
          </div>

          <div class="">
            <h3 class="heading">{{ formatMoney(overview!.economy.transfer_allocated) }}</h3>
            <h4 class="heading heading--small">transfer allocation</h4>
          </div>

          <div class="">
            <h3 class="heading">{{ formatMoney(overview!.economy.transfer_committed) }}</h3>
            <h4 class="heading heading--small">transfer committed</h4>
          </div>

          <div :class="{ 'text-loss-500': overview.economy.crisis_clubs !== 0 }">
            <h3 class="heading text-inherit">{{ overview!.economy.crisis_clubs }}</h3>
            <h4 class="heading heading--small text-inherit">clubs in crisis</h4>
          </div>
        </div>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="loadCountryOverview()">Retry</button>
        </div>
      </div>

      <div class="row-span-1 bg-void-900 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">leagues</h3>
          <nuxt-link :to="{ name: 'admin-CountryLeagues-create', params: { id: worldId, countryId } }"
            class="font-mono text-cyan-500/50 hover:text-cyan-500 text-xs uppercase">
            Create League
          </nuxt-link>
        </div>
        <UiLoader v-if="status.pyramid === 'loading'" />
        <template v-else-if="status.pyramid === 'loaded'">
          <div v-if="leaguePyramids.leagues.length === 0" class="p-10 uppercase text-xs text-void-400">
            No league created in this country, yet.
          </div>
          <div v-else class="mt-5 h-37.5 overflow-auto">
            <div v-for="(league, key) in leaguePyramids.leagues" :key
              class="grid gap-3 justify-between grid-cols-5 text-void-400">
              <nuxt-link :to="{ name: 'admin-league-page', params: { id: worldId, countryId, leagueId: league.id } }"
                class="font-semibold col-span-3 text-ellipsis line-clamp-1 hover:text-cyan-500 ease-in-out uppercase text-sm font-mono">
                #{{ league.tier }}
                {{ league.name }}
              </nuxt-link>
              <div class="">{{ league.team_count }}</div>
              <div class="flex gap-2">
                <span class="text-win-500">{{ league.promotions }}</span>
                <span class="text-loss-500">({{ league.relegations }})</span>
              </div>
            </div>
          </div>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="loadLeaguePyramid()">Retry</button>
        </div>
      </div>

      <div class="row-span-2 bg-void-900 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">players</h3>
        </div>

        <UiLoader v-if="status.players === 'loading'" />
        <template v-else-if="status.players === 'loaded'">
          <div v-if="countryPlayers.total === 0" class="p-10 uppercase text-xs text-void-400">
            No player seeded for this country, yet.
          </div>

          <template v-else>
            <div class="my-5 grid gap-3 grid-cols-2 grid-rows-2">
              <div class="row-span-2 flex flex-col justify-center">
                <h3 class="heading heading--big">{{ countryPlayers.total }}</h3>
                <h4 class="heading heading--small">total players</h4>
              </div>

              <div class="">
                <h3 class="heading">{{ countryPlayers.by_status.active }}</h3>
                <h4 class="heading heading--small">active</h4>
              </div>

              <div class="">
                <h3 class="heading">{{ countryPlayers.by_status.free_agent }}</h3>
                <h4 class="heading heading--small">free agents</h4>
              </div>
            </div>

            <div class="max-h-77.5 overflow-auto space-y-2">
              <div v-for="(nation, key) in countryPlayers.nationalities" :key
                class="flex gap-3 justify-between text-void-400 border-b border-void-700 py-2">
                <div class="uppercase font-mono text-sm">{{ nation.name }}</div>
                <div class="">{{ nation.count }}</div>
              </div>
            </div>
          </template>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="loadAllPlayersFromCountry()">Retry</button>
        </div>
      </div>

      <div class="row-span-2 bg-void-900 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">news</h3>

          <button
            disabled
            @click="seedWorldAndCountry()"
            class="font-mono text-cyan-500/50 hover:text-cyan-500 text-xs uppercase cursor-pointer"
          >
            Press Release
          </button>
        </div>

        <UiLoader v-if="status.news === 'loading'" />
        <template v-else-if="status.news === 'loaded'">
          {{ newsStories }}
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="loadNews()">Retry</button>
        </div>
      </div>

      <div class="row-span-2 col-span-2 bg-void-900 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">clubs</h3>
        </div>

        <UiLoader v-if="status.clubs === 'loading'" />
        <template v-else-if="status.clubs === 'loaded'">
          <div class="h-105 mt-5 overflow-auto font-mono">
            <div v-for="(club, key) in leagueClubs!.clubs" :key
              class="grid gap-3 items-center justify-between grid-cols-5 text-void-400 border-b border-void-700 py-2">
              <div class="col-span-2 flex gap-2 items-center">
                <UiAvatar :alt="club!.short_name!" />
                <div class="flex gap-1 flex-col">
                  <div class="flex items-center gap-2">
                    <h3 class="font-mono uppercase font-semibold text-xs text-void-300">{{ club!.name }}</h3>
                    <svg class="w-5 h-auto fill-void-600 cursor-pointer" @click="openRename(club)"
                      viewBox="0 0 256 256">
                      <path
                        d="M248,92.68a15.86,15.86,0,0,0-4.69-11.31L174.63,12.68a16,16,0,0,0-22.63,0L123.57,41.11l-58,21.77A16.06,16.06,0,0,0,55.35,75.23L32.11,214.68A8,8,0,0,0,40,224a8.4,8.4,0,0,0,1.32-.11l139.44-23.24a16,16,0,0,0,12.35-10.17l21.77-58L243.31,104A15.87,15.87,0,0,0,248,92.68Zm-69.87,92.19L63.32,204l47.37-47.37a28,28,0,1,0-11.32-11.32L52,192.7,71.13,77.86,126,57.29,198.7,130ZM112,132a12,12,0,1,1,12,12A12,12,0,0,1,112,132Zm96-15.32L139.31,48l24-24L232,92.68Z">
                      </path>
                    </svg>
                  </div>
                  <div v-if="club!.is_ai_controlled" class="flex gap-3 items-center heading heading--small">
                    <svg xmlns="http://www.w3.org/2000/svg" class="w-4 h-auto fill-cyan-700" viewBox="0 0 256 256">
                      <path
                        d="M200,48H136V16a8,8,0,0,0-16,0V48H56A32,32,0,0,0,24,80V192a32,32,0,0,0,32,32H200a32,32,0,0,0,32-32V80A32,32,0,0,0,200,48Zm16,144a16,16,0,0,1-16,16H56a16,16,0,0,1-16-16V80A16,16,0,0,1,56,64H200a16,16,0,0,1,16,16Zm-52-56H92a28,28,0,0,0,0,56h72a28,28,0,0,0,0-56Zm-24,16v24H116V152ZM80,164a12,12,0,0,1,12-12h8v24H92A12,12,0,0,1,80,164Zm84,12h-8V152h8a12,12,0,0,1,0,24ZM72,108a12,12,0,1,1,12,12A12,12,0,0,1,72,108Zm88,0a12,12,0,1,1,12,12A12,12,0,0,1,160,108Z">
                      </path>
                    </svg>
                    AI controlled
                  </div>
                  <h4 class="heading heading--small"></h4>
                </div>
              </div>

              <div class="heading heading--small">{{ club.league?.name ?? "-" }}</div>
              <div class="heading heading--small">{{ formatMoneyCompact(club.transfer_allocated) }}/{{
                formatMoneyCompact(club.transfer_committed) }}</div>
              <div class="heading heading--small">{{ formatMoneyCompact(club.wage_allocated) }}/{{
                formatMoneyCompact(club.wage_bill) }}</div>
            </div>
          </div>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="loadCountryClubs()">Retry</button>
        </div>
      </div>

      <div class="bg-void-900 border-brutal border-void-800 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-5 border-void-800">
          <h3 class="heading heading--small">market</h3>

          <button @click="seedWorldAndCountry()"
            class="font-mono text-cyan-500/50 hover:text-cyan-500 text-xs uppercase cursor-pointer">
            Seed
          </button>
        </div>

        <UiLoader v-if="status.market === 'loading'" />
        <template v-else-if="status.market === 'loaded'">
          <div class="grid grid-cols-4 grid-rows-2 gap-y-3 mt-5">
            <div class="row-span-2 flex flex-col justify-center">
              <h3 class="heading text-3xl">{{ market?.window_days }}</h3>
              <h4 class="heading heading--small">Window days</h4>
            </div>


            <div class="">
              <h3 class="heading text-3xl">{{ market?.signings.length }}</h3>
              <h4 class="heading heading--small">signings</h4>
            </div>

            <div class="">
              <h3 class="heading">{{ market?.open_listings.length }}</h3>
              <h4 class="heading heading--small">open</h4>
            </div>

            <div class="">
              <h3 class="heading">{{ market?.bids_made.length }}</h3>
              <h4 class="heading heading--small">made</h4>
            </div>
            <div class="">
              <h3 class="heading">{{ market?.bids_received.length }}</h3>
              <h4 class="heading heading--small">recieved</h4>
            </div>
            <div class="">
              <h3 class="heading">{{ market?.transfers_in.length }}</h3>
              <h4 class="heading heading--small">in</h4>
            </div>
            <div class="">
              <h3 class="heading">{{ market?.transfers_out.length }}</h3>
              <h4 class="heading heading--small">out</h4>
            </div>
          </div>
        </template>
        <div v-else class="uppercase text-xs p-10 flex flex-col items-start gap-2">
          <div class="text-loss-500">
            An Error Occurred
          </div>
          <button class="uppercase text-cyan-300" @click="loadCountryMarkets()">Retry</button>
        </div>
      </div>
    </section>
  </section>

  <div v-if="renameTarget" class="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
    @click.self="renameTarget = null">
    <div class="bg-slate-800 border border-slate-600 rounded-lg p-6 w-full max-w-lg">
      <h3 class="text-xl font-semibold text-white mb-1">Rename club</h3>
      <p class="text-slate-400 text-sm mb-4">{{ renameTarget.name }} — the rename publishes a news story announcing it.
      </p>
      <div class="space-y-3">
        <div>
          <label class="block text-sm text-slate-400 mb-1">New name</label>
          <input v-model="renameForm.name" type="text"
            class="w-full bg-slate-900 border border-slate-600 rounded px-3 py-2" />
        </div>
        <div>
          <label class="block text-sm text-slate-400 mb-1">Short code <span class="text-slate-500">(optional — derived
              if
              blank)</span></label>
          <input v-model="renameForm.short_name" type="text" maxlength="3"
            class="w-28 bg-slate-900 border border-slate-600 rounded px-3 py-2 uppercase" />
        </div>
        <div class="border-t border-slate-700 pt-3">
          <p class="text-sm text-slate-400 mb-2">News story (announces the change in-world)</p>
          <div class="space-y-3">
            <div>
              <label class="block text-sm text-slate-400 mb-1">Headline</label>
              <input v-model="renameForm.headline" type="text" placeholder="e.g. Phoenix rise from the ashes"
                class="w-full bg-slate-900 border border-slate-600 rounded px-3 py-2" />
            </div>
            <div>
              <label class="block text-sm text-slate-400 mb-1">Body</label>
              <textarea v-model="renameForm.body" rows="3" placeholder="The club rebrands ahead of the new season…"
                class="w-full bg-slate-900 border border-slate-600 rounded px-3 py-2"></textarea>
            </div>
          </div>
        </div>
        <p v-if="renameError" class="text-red-400 text-sm">{{ renameError }}</p>
        <div class="flex justify-end gap-3 pt-2">
          <button @click="renameTarget = null"
            class="bg-slate-700 hover:bg-slate-600 text-white rounded px-4 py-2">Cancel</button>
          <button :disabled="renameBusy" @click="submitRename"
            class="bg-indigo-600 hover:bg-indigo-500 text-white rounded px-4 py-2">
            {{ renameBusy ? 'Renaming…' : 'Rename & publish' }}
          </button>
        </div>
      </div>
    </div>
  </div>

  <NuxtPage />
</template>