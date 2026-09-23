<script setup lang="ts">
// S04-01 manager view: the competition folder for the caller's world. Pick a
// league to see its season fixture calendar (IM03, grouped by game-week with
// kickoff times) and its live standings table.
import { computed, onMounted, ref } from 'vue'

import type { Cup, CupCampaign, Fixture, League, SeasonCalendar, Standings } from '~/composables/useCompetition'
import { useCompetition } from '~/composables/useCompetition'

const comp = useCompetition()

const leagues = ref<League[]>([])
const cups = ref<Cup[]>([])
const cupCampaigns = ref<Record<string, CupCampaign | null>>({})
const selected = ref<string | null>(null)
const calendar = ref<SeasonCalendar | null>(null)
const standings = ref<Standings | null>(null)
const error = ref('')

const totalMatchdays = computed(() => {
  const teams = standings.value?.rows.length || 0
  return teams > 0 ? 2 * (teams - 1) : 0
})

async function load() {
  leagues.value = await comp.listMyCompetitions()
  cups.value = await comp.listMyCups()
  for (const cup of cups.value) {
    cupCampaigns.value[cup.id] = await comp.getCup(cup.id)
  }
  const best = leagues.value[0]
  if (best) await selectLeague(best.id)
}

async function selectLeague(id: string) {
  selected.value = id
  calendar.value = null
  standings.value = null
  standings.value = await comp.getStandings(id)
  calendar.value = await comp.getSeasonCalendar(id)
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

function kickoffTime(scheduledAt: string): string {
  return new Date(scheduledAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function weekLabel(w: { week: number; first_day: string }): string {
  const start = new Date(w.first_day).toLocaleDateString([], { day: 'numeric', month: 'short' })
  const end = new Date(new Date(w.first_day).getTime() + 6 * 24 * 3600 * 1000)
    .toLocaleDateString([], { day: 'numeric', month: 'short' })
  return start === end ? start : `${start} – ${end}`
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

      <section v-if="cups.length" class="bg-slate-800 border border-slate-700 rounded-lg p-4">
        <h2 class="text-lg font-semibold text-white mb-3">Cups</h2>
        <div class="space-y-4">
          <div v-for="cup in cups" :key="cup.id" class="bg-slate-900 border border-slate-700 rounded-lg p-3">
            <h3 class="font-medium text-white mb-1">
              {{ cup.name }}
              <span class="text-slate-500 text-sm">{{ cup.country.name ?? '' }}</span>
            </h3>
            <p class="text-sm text-slate-400 mb-2">
              {{ cup.format }} · top {{ cup.first_tier_bye }} join at {{ cup.survivor_threshold }} survivors
            </p>
            <CupBracketView v-if="cupCampaigns[cup.id]" :campaign="cupCampaigns[cup.id]" />
            <p v-else class="text-slate-500 text-sm">No campaign started yet.</p>
          </div>
        </div>
      </section>

      <template v-if="selected">
        <section v-if="standings" class="bg-slate-800 border border-slate-700 rounded-lg p-4">
          <h2 class="text-lg font-semibold text-white mb-3">Table — {{ standings.season?.label }}</h2>
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
                <tr v-for="(r, i) in standings.rows" :key="r.club.id" class="border-t border-slate-700">
                  <td class="py-2 pr-3">{{ i + 1 }}</td>
                  <td class="py-2 pr-3 font-medium text-white">{{ r.club.name }}</td>
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
          <h2 class="text-lg font-semibold text-white mb-1">Fixtures — {{ calendar?.season?.label }}</h2>
          <p class="text-slate-500 text-sm mb-3">
            {{ totalMatchdays }} matchdays paced across the game-week, {{ calendar?.weeks.length ?? 0 }} week(s).
          </p>

          <div v-if="calendar?.weeks.length" class="space-y-5">
            <div v-for="w in calendar.weeks" :key="w.week" class="space-y-3">
              <h3 class="text-sm font-medium text-indigo-300 border-b border-slate-700 pb-1">
                Week {{ w.week + 1 }} — {{ weekLabel(w) }}
              </h3>
              <div v-for="md in w.matchdays" :key="md.matchday" class="space-y-1">
                <p class="text-xs text-slate-400">
                  Matchday {{ md.matchday }} · kick-off
                  {{ new Date(md.scheduled_at).toLocaleDateString([], { day: 'numeric', month: 'short' }) }}
                  {{ kickoffTime(md.scheduled_at) }}
                </p>
                <ul class="divide-y divide-slate-700/60">
                  <li v-for="f in md.fixtures" :key="f.id" class="py-2 flex items-center gap-3 text-sm">
                    <NuxtLink :to="`/matches/${f.id}`" class="flex-1 flex items-center gap-3 group">
                      <span class="text-right flex-1">{{ f.home_club.name ?? f.home_club.id.slice(0, 8) }}</span>
                      <span class="bg-slate-900 border border-slate-600 rounded px-3 py-1 font-bold text-center w-20 group-hover:border-indigo-500">{{ score(f) }}</span>
                      <span class="flex-1">{{ f.away_club.name ?? f.away_club.id.slice(0, 8) }}</span>
                    </NuxtLink>
                    <span class="w-24 text-right" :class="f.status === 'completed' ? 'text-emerald-400' : 'text-slate-500'">
                      {{ f.status }}
                    </span>
                  </li>
                </ul>
              </div>
            </div>
          </div>
          <p v-else class="text-slate-500 text-sm">No fixtures scheduled yet.</p>
        </section>
      </template>
    </div>
  </main>
</template>