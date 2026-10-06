<script setup lang="ts">
// S04-01 manager view: the competition folder for the caller's world. Pick a
// league to see its season fixture calendar (IM03, grouped by game-week with
// kickoff times) and its live standings table.
import { computed, onMounted, ref } from 'vue'
import { useToast } from '~/components/ui/Toast'

import type { Cup, CupCampaign, Fixture, League, SeasonCalendar, Standings } from '~/composables/useCompetition'
import { useCompetition } from '~/composables/useCompetition'

const store = useClubStore()
const comp = useCompetition()
const { notify } = useToast()

const leagues = ref<League[]>([])
const league = ref<League>()

const cups = ref<Cup[]>([])
const cupCampaigns = ref<Record<string, CupCampaign | null>>({})
const selected = ref<string | null>(null)
const calendar = ref<SeasonCalendar | null>(null)
const standings = ref<Standings | null>(null)
const error = ref('')

const loading = reactive({ leagues: "loading", league: "loading" })

const club = computed(() => store.club)

async function load() {
  loading.leagues = "loading"

  try {
    leagues.value = await comp.listMyCompetitions()
    cups.value = await comp.listMyCups()
    for (const cup of cups.value) {
      cupCampaigns.value[cup.id] = await comp.getCup(cup.id)
    }
    const best = leagues.value[0]
    if (best) await selectLeague(best.id)

    loading.leagues = "loaded"
  } catch (error) {
    console.error(error)
    loading.leagues = "error"
    notify({
      title: "Error",
      description: "",
      type: "danger"
    })
  }
}

async function selectLeague(id: string) {
  league.value = leagues.value.find((l) => l.id === id)

  selected.value = id
  calendar.value = null
  standings.value = null

  try {
    loading.league = "loading"
    standings.value = await comp.getStandings(id)
    calendar.value = await comp.getSeasonCalendar(id)
    loading.league = "loaded" 
  } catch (error) {
    console.error(error)
    notify({
      title: "Error",
      description: "",
      type: "danger"
    })

    loading.league = "error"
  }
}

async function refresh() {
  if (selected.value) await selectLeague(selected.value)
  else await load()
}

onMounted(load)
</script>

<template>
  <main class="space-y-5 font-mono">
    <h1 class="page__header">Competitons</h1>
    <div class="max-sm:space-y-5 md:grid gap-5 md:grid-cols-4 grid-rows-2">
      <div class="border-brutal border-void-600 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-3 border-void-800">
          <h3 class="heading heading--small">All Competitions</h3>
          <button @click="refresh()" class="heading heading--small text-cyan-500 cursor-pointer"
            :disabled="loading.leagues === 'loading'">
            Refresh
          </button>
        </div>

        <div v-if="loading.leagues === 'error'" class=""></div>
        <div v-else class="">
          <div v-if="leagues.length === 0" class=""></div>
          <div v-else class="mt-5 flex flex-col gap-3">
            <div class="p-3 border-hairline cursor-pointer"
              :class="[{ 'border-cyan-500 bg-cyan-500 text-void-800 font-semibold': league.id === selected }]"
              @click="selectLeague(league.id)" v-for="(league, key) in leagues" :key>
              {{ league.name }}
            </div>
          </div>
        </div>
        <UiLoader v-if="loading.leagues === 'loading'" />
      </div>
      
      <div class="row-span-2 col-span-2 border-brutal border-cyan-500 p-5">
        <template v-if="selected &&league">
          <div class="flex items-center justify-between border-b-brutal pb-3 border-void-800">
            <h3 class="heading heading--small">
              <span class="text-cyan-500">{{ league.name }}</span> / {{ standings?.season.label }}
            </h3>
          </div>
          <div class="mt-5">
            <div class="grid grid-cols-12">
              <h4 class="col-span-4 heading heading--small">club</h4>
              <h4 class="heading heading--small text-center">mp</h4>
              <h4 class="heading heading--small text-center">w</h4>
              <h4 class="heading heading--small text-center">d</h4>
              <h4 class="heading heading--small text-center">l</h4>
              <h4 class="heading heading--small text-center">gf</h4>
              <h4 class="heading heading--small text-center">ga</h4>
              <h4 class="heading heading--small text-center">gd</h4>
              <h4 class="heading heading--small text-center">pts</h4>
            </div>
            <div class="overflow-auto">
              <div class="grid grid-cols-12 py-1 border-b border-void-500" :class="{
                'bg-void-600 font-bold text-cyan-500!': row.club.short === club!.short,
                'bg-loss-500/10 text-loss-500 border-loss-500!': [17, 18, 19].includes(index),
                'bg-win-500/10 text-win-500 border-win-500!': [0, 1, 2].includes(index)
              }" v-for="(row, index) in standings?.rows" :key="index">
                <div class="col-span-4 grid grid-cols-7">
                  {{ index + 1 }}
                  <nuxt-link :to="`/play/clubs/${row.club.id}`" class="col-span-6 line-clamp-1 hover:underline max-sm:text-sm" :title="row.club.name" target="_blank">
                    <span class="heading heading--small border px-1">{{ row.club.short }}</span>
                    {{ row.club.name }}
                  </nuxt-link>
                </div>
                <div class="text-center">{{ row.played }}</div>
                <div class="text-center">{{ row.won }}</div>
                <div class="text-center">{{ row.drawn }}</div>
                <div class="text-center">{{ row.lost }}</div>
                <div class="text-center">{{ row.goals_for }}</div>
                <div class="text-center">{{ row.goals_against }}</div>
                <div class="text-center">{{ row.goals_for - row.goals_against }}</div>
                <div class="text-center font-bold">{{ row.points }}</div>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div class="border-brutal border-void-600 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-3 border-void-800">
          <h3 class="heading heading--small">fixtures</h3>
        </div>
        <UiLoader v-if="loading.league === 'loading'" />
        <div v-else-if="loading.league === 'loaded'" class="mt-5 h-70 overflow-auto">
          <div class="" v-for="(wk, key) in calendar?.weeks" :key>
            <div v-for="(md, index) in wk.matchdays" :key="index" class="mb-8">
              <div class="text-center">
                <h3 class="heading text-lg">Matchweek {{ md.gameweek }}</h3>
                <div class=""></div>
              </div>

              <template v-for="fx in md.fixtures" :key="fx.id">
                <nuxt-link
                  v-if="fx.status === 'completed'"
                  :to="`/play/matches/${fx.id}`"
                  class="grid grid-cols-5 items-center justify-between py-3 border-b border-void-500 hover:bg-void-800"
                  :class="[{'text-cyan-500' : fx.home_club.id === club!.id }]"
                  target="_blank"
                >
                  <span class="text-sm col-span-2">{{ fx.home_club.name }}</span>
                  <div class="font-bold text-center">
                    {{ fx.home_score }} - {{ fx.away_score }}
                  </div>
                  <span class="text-sm col-span-2 text-right">{{ fx.away_club.name }}</span>
                </nuxt-link>
                <div
                  v-else
                  class="grid grid-cols-5 items-center justify-between py-1 border-b border-void-500 hover:bg-void-800"
                  :class="[{ 'text-cyan-500': fx.home_club.id === club!.id }]" target="_blank">
                  <span class="text-sm col-span-2">{{ fx.home_club.name }}</span>
                  <div class="font-bold text-sm text-center">
                    {{ formatFixtureDateTimeCompact(fx.scheduled_at) }}
                  </div>
                  <span class="text-sm col-span-2 text-right">{{ fx.away_club.name }}</span>
                </div>
              </template>
            </div>
          </div>
        </div>
        <div v-else class="">Error</div>
      </div>

      <div class="" />
      <div class="border-brutal border-void-600 p-5">
        <div class="flex items-center justify-between border-b-brutal pb-3 border-void-800">
          <h3 class="heading heading--small">league stats</h3>
        </div>
        <div class="mt-5">
          To Goal Scorers and assiss goes here
        </div>
      </div>
    </div>
  </main>
</template>