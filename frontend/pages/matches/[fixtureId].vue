<script setup lang="ts">
// S04-03 match screen: renders the server-produced event feed for one fixture.
// On mount it catch-ups over REST (header + persisted events), then the shared
// realtime session streams later minutes as match_tick envelopes. Everything on
// screen — events, scoreline, clock — is produced by the server.
import { storeToRefs } from 'pinia'

import type { EventType } from '~/stores/match'
import { useMatchStore } from '~/stores/match'

const route = useRoute()
const matchStore = useMatchStore()
const { fixture, match, events, error, stuck } = storeToRefs(matchStore)

const fixtureId = computed(() => String(route.params.fixtureId))

const labels: Record<EventType, string> = {
  kickoff: 'Kick-off',
  goal: 'Goal',
  assist: 'Assist',
  chance_created: 'Chance',
  yellow_card: 'Yellow card',
  red_card: 'Red card',
  injury: 'Injury',
  substitution: 'Substitution',
  penalty_awarded: 'Penalty awarded',
  penalty_scored: 'Penalty scored',
  penalty_missed: 'Penalty missed',
  half_time: 'Half-time',
  full_time: 'Full-time',
}

const statusText = computed(() => {
  if (!match.value) return 'Not started'
  if (match.value.status === 'in_progress') return `Live · ${match.value.minute}'`
  if (match.value.status === 'completed') return 'Full-time'
  return match.value.status
})

onMounted(() => { matchStore.watch(fixtureId.value).catch(() => {}) })
onUnmounted(() => matchStore.disconnect())
</script>

<template>
  <main class="min-h-screen bg-slate-900 text-slate-200 px-6 py-10">
    <div class="max-w-3xl mx-auto space-y-6">
      <NuxtLink to="/competitions" class="text-slate-400 text-sm hover:text-indigo-300">← Competitions</NuxtLink>

      <template v-if="fixture">
        <header class="bg-slate-800 border border-slate-700 rounded-lg p-6">
          <div class="flex items-center justify-between gap-4 text-center">
            <div class="flex-1 font-semibold text-white">{{ fixture.home_club_name }}</div>
            <div class="shrink-0">
              <div class="text-4xl font-bold text-white">
                {{ match ? `${match.home_score}–${match.away_score}` : '–' }}
              </div>
              <div class="mt-2 inline-block rounded px-3 py-1 text-xs font-semibold"
                   :class="match?.status === 'in_progress' ? 'bg-emerald-800 text-emerald-200' :
                           match?.status === 'completed' ? 'bg-indigo-800 text-indigo-200' :
                           'bg-slate-700 text-slate-300'">
                {{ statusText }}
              </div>
            </div>
            <div class="flex-1 font-semibold text-white">{{ fixture.away_club_name }}</div>
          </div>
        </header>

        <p v-if="error" class="text-red-400 text-sm">{{ error }}</p>
        <p v-if="!match" class="text-slate-500 text-sm">
          This fixture has not kicked off yet — check back on matchday.
        </p>

        <section v-else class="bg-slate-800 border border-slate-700 rounded-lg p-4">
          <h2 class="text-lg font-semibold text-white mb-3">Match events</h2>
          <ul v-if="events.length" class="divide-y divide-slate-700">
            <li v-for="ev in events" :key="ev.id" class="flex items-start gap-3 py-2 text-sm">
              <span class="w-10 shrink-0 text-right font-mono text-slate-400 tabular-nums">{{ ev.minute }}'</span>
              <span class="w-28 shrink-0 font-semibold"
                    :class="ev.type === 'goal' || ev.type === 'penalty_scored' ? 'text-emerald-400' :
                            ev.type === 'red_card' || ev.type === 'injury' ? 'text-red-400' :
                            ev.type === 'yellow_card' ? 'text-yellow-400' :
                            ev.type === 'half_time' || ev.type === 'full_time' ? 'text-indigo-300' :
                            'text-slate-300'">
                {{ labels[ev.type] }}</span>
              <span class="text-slate-300/90">{{ ev.detail?.commentary ?? ev.detail?.detail ?? ev.type }}</span>
            </li>
          </ul>
          <p v-else-if="match.status === 'in_progress'" class="text-slate-500 text-sm">
            Waiting for the first event…
          </p>
          <p v-else class="text-slate-500 text-sm">No events recorded.</p>
        </section>

        <p v-if="stuck" class="text-slate-500 text-sm">The live stream has ended; this feed is final.</p>
      </template>

      <p v-else-if="error" class="text-red-400 text-sm">{{ error }}</p>
      <p v-else class="text-slate-500">Loading match…</p>
    </div>
  </main>
</template>