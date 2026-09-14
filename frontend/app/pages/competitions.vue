<script setup lang="ts">
// S04-01 manager view: the competition folder for the caller's world. Pick a
// league to see its fixture list (per matchday) and its live standings table.
import { ref } from 'vue'

import type { Fixture, League, Standings } from '~/composables/useCompetition'
import { useCompetition } from '~/composables/useCompetition'

const comp = useCompetition()

const leagues = ref<League[]>([])
const selected = ref<string | null>(null)
const matchday = ref<number | null>(null)
const fixtures = ref<Fixture[]>([])
const standings = ref<Standings | null>(null)
const error = ref('')

const matchdayOptions = computed(() => {
  const teams = standings.value?.rows.length || 0
  const n = teams > 0 ? 2 * (teams - 1) : 0
  return Array.from({ length: n }, (_, i) => i + 1)
})

async function load() {
  leagues.value = await comp.listMyCompetitions()
  const best = leagues.value[0]
  if (best) await selectLeague(best.id)
}

async function selectLeague(id: string) {
  selected.value = id
  matchday.value = null
  fixtures.value = []
  standings.value = await comp.getStandings(id)
  await loadMatchday(1)
}

async function loadMatchday(md: number | null) {
  if (!selected.value) return
  matchday.value = md
  fixtures.value = md ? await comp.getFixtures(selected.value, md) : []
}

async function refresh() {
  if (selected.value) await selectLeague(selected.value)
  else await load()
}

function score(f: Fixture): string {
  return f.status === 'completed' && f.home_score != null && f.away_score != null
    ? `${f.home_score}–${f.away_score}`
    : '-'
}

onMounted(() => { load().catch(() => { error.value = 'Could not load your competitions.' }) })
</script>

<template>
  <main class="min-h-screen bg-slate-900 text-slate-200 px-6 py-10">
    <div class="max-w-5xl mx-auto space-y-6">
      <header class="flex items-center justify-between">
        <div>
          <h1 class="text-3xl font-bold text-white">Competitions</h1>
          <p class="text-slate-400 mt-1">Your world's leagues and where you sit in them.</p>
        </div>
        <button @click="refresh" class="bg-indigo-600 hover:bg-indigo-500 text-white rounded px-4 py-2">Refresh</button>
      </header>

      <p v-if="error" class="text-red-400 text-sm">{{ error }}</p>
      <p v-if="!leagues.length && !error" class="text-slate-500">No competitions in this world yet.</p>

      <div v-else class="flex flex-wrap gap-2">
        <button v-for="l in leagues" :key="l.id" @click="selectLeague(l.id)"
                class="rounded px-4 py-2"
                :class="selected === l.id ? 'bg-indigo-600 text-white' : 'bg-slate-800 text-slate-300 hover:bg-slate-700'">
          {{ l.name }}
        </button>
      </div>

      <template v-if="selected">
        <section v-if="standings" class="bg-slate-800 border border-slate-700 rounded-lg p-4">
          <h2 class="text-lg font-semibold text-white mb-3">Table — {{ standings.season_label }}</h2>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead class="text-slate-500 text-left">
                <tr>
                  <th class="py-1 pr-3">#</th><th class="py-1 pr-3">Club</th>
                  <th class="py-1 pr-3 text-center">P</th><th class="py-1 pr-3 text-center">W</th>
                  <th class="py-1 pr-3 text-center">D</th><th class="py-1 pr-3 text-center">L</th>
                  <th class="py-1 pr-3 text-center">GF</th><th class="py-1 pr-3 text-center">GA</th>
                  <th class="py-1 text-center">Pts</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(r, i) in standings.rows" :key="r.club_name" class="border-t border-slate-700">
                  <td class="py-2 pr-3">{{ i + 1 }}</td>
                  <td class="py-2 pr-3 font-medium text-white">{{ r.club_name }}</td>
                  <td class="py-2 pr-3 text-center">{{ r.played }}</td>
                  <td class="py-2 pr-3 text-center">{{ r.won }}</td>
                  <td class="py-2 pr-3 text-center">{{ r.drawn }}</td>
                  <td class="py-2 pr-3 text-center">{{ r.lost }}</td>
                  <td class="py-2 pr-3 text-center">{{ r.goals_for }}</td>
                  <td class="py-2 pr-3 text-center">{{ r.goals_against }}</td>
                  <td class="py-2 text-center font-bold">{{ r.points }}</td>
                </tr>
                <tr v-if="!standings.rows.length">
                  <td colspan="9" class="py-3 text-slate-500 text-center">No results yet — the first matchday will fill this table.</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section class="bg-slate-800 border border-slate-700 rounded-lg p-4">
          <div class="flex flex-wrap items-center gap-3 mb-3">
            <h2 class="text-lg font-semibold text-white">Fixtures</h2>
            <select v-if="matchdayOptions.length" :value="matchday ?? ''"
                    @change="loadMatchday(Number(($event.target as HTMLSelectElement).value))"
                    class="bg-slate-900 border border-slate-600 rounded px-2 py-1 text-sm">
              <option v-for="m in matchdayOptions" :key="m" :value="m">Matchday {{ m }}</option>
            </select>
            <span class="text-slate-500 text-sm ml-auto">Kick-off 19:00 UTC</span>
          </div>
          <ul v-if="fixtures.length" class="divide-y divide-slate-700">
            <li v-for="f in fixtures" :key="f.id" class="py-2 flex items-center gap-3 text-sm">
              <NuxtLink :to="`/matches/${f.id}`" class="flex-1 flex items-center gap-3 group">
                <span class="text-right flex-1">{{ f.home_club_name ?? f.home_club_id.slice(0, 8) }}</span>
                <span class="bg-slate-900 border border-slate-600 rounded px-3 py-1 font-bold text-center w-20 group-hover:border-indigo-500">{{ score(f) }}</span>
                <span class="flex-1">{{ f.away_club_name ?? f.away_club_id.slice(0, 8) }}</span>
              </NuxtLink>
              <span class="w-24 text-right" :class="f.status === 'completed' ? 'text-emerald-400' : 'text-slate-500'">
                {{ f.status }}
              </span>
            </li>
          </ul>
          <p v-else class="text-slate-500 text-sm">Select a matchday to see fixtures.</p>
        </section>
      </template>
    </div>
  </main>
</template>