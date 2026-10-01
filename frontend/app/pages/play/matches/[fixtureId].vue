<script setup lang="ts">
// S04-03 match screen: renders the server-produced event feed for one fixture.
// On mount it catch-ups over REST (header + persisted events), then the shared
// realtime session streams later minutes as match_tick envelopes. Everything on
// screen — events, scoreline, clock — is produced by the server.
import { storeToRefs } from 'pinia'

// import type { EventType } from '~/storesmatch'
// import { useMatchStore } from '~/storesmatch'

const route = useRoute()
const matchStore = useMatchStore()
const { fixture, match, events, error, stuck, track, trackPlayers, trackLast } = storeToRefs(matchStore)

const fixtureId = computed(() => String(route.params.fixtureId))
const liveStyle = ref('balanced')
const tacticalError = ref('')

async function changeStyle() {
  if (!match.value) return
  const { authedFetch } = useAuth()
  const r = await authedFetch(`/api/matches/${match.value.id}/tactical`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ minute: match.value.minute + 1, style: liveStyle.value }),
  })
  tacticalError.value = r.ok ? '' : ((await r.json().catch(() => ({}))).error ?? 'Could not change style.')
}

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
  shot: 'Shot',
  save: 'Save',
  tackle: 'Tackle',
  foul: 'Foul',
  corner: 'Corner',
  offside: 'Offside',
}

// IM34 2D simulation. `playhead` is match time in milliseconds from kick-off.
// A live match plays one match minute per `pacing_millis`, one step behind the
// server (a minute's movement only exists once that minute has been played).
// While the picture is running, the feed, scoreline and clock follow the
// playhead, so nothing is announced before it is shown.
const playhead = ref(0)
const playing = ref(false)
const speed = ref(4)
const following = ref(false) // watching a live match as it is played
const started = ref(false) // a replay has been started
const speeds = [1, 4, 16]

const visual = computed(() => !!match.value?.visual && trackLast.value > 0)
const gated = computed(() => visual.value && (following.value || started.value))
const trackEnd = computed(() => trackLast.value * 60000)
const behindLive = computed(() => following.value && trackEnd.value - playhead.value > 90000)

const at = (ev: { minute: number; offset_millis?: number }) => (Math.max(ev.minute, 1) - 1) * 60000 + (ev.offset_millis ?? 0)
const shownEvents = computed(() =>
  gated.value ? events.value.filter((ev) => at(ev) <= playhead.value).reverse() : events.value)

const score = computed(() => {
  if (!match.value) return null
  if (!gated.value || !fixture.value) return { home: match.value.home_score, away: match.value.away_score }
  const goals = shownEvents.value.filter((ev) => ev.type === 'goal' || ev.type === 'penalty_scored')
  return {
    home: goals.filter((ev) => ev.club?.id === fixture.value!.home_club.id).length,
    away: goals.filter((ev) => ev.club?.id === fixture.value!.away_club.id).length,
  }
})

// Match stats up to the playhead: extras from the feed, passes from the track.
const statRows = [
  ['Passes', 'pass'], ['Shots off target', 'shot'], ['Saves', 'save'], ['Corners', 'corner'],
  ['Tackles', 'tackle'], ['Fouls', 'foul'], ['Offsides', 'offside'],
] as const
const stats = computed(() => {
  if (!visual.value || !fixture.value) return []
  const upTo = gated.value ? playhead.value : trackEnd.value
  const seen = events.value.filter((ev) => at(ev) <= upTo)
  const minutes = Object.values(track.value).filter((m) => (m.minute - 1) * 60000 <= upTo)
  return statRows.map(([label, type]) => {
    const count = (clubId: string, side: 0 | 1) => type === 'pass'
      ? minutes.reduce((n, m) => n + m.passes[side], 0)
      : seen.filter((ev) => ev.type === type && ev.club?.id === clubId).length
    return { label, home: count(fixture.value!.home_club.id, 0), away: count(fixture.value!.away_club.id, 1) }
  })
})

let raf = 0
let lastFrame = 0
function step(now: number) {
  const dt = lastFrame ? now - lastFrame : 0
  lastFrame = now
  if (playing.value) {
    const pacing = match.value?.pacing_millis || 20000
    const rate = (60000 / pacing) * (following.value ? 1 : speed.value)
    playhead.value = Math.min(playhead.value + dt * rate, trackEnd.value)
    // A replay stops at the end; a live match waits there for the next minute.
    if (playhead.value >= trackEnd.value && match.value?.status === 'completed') {
      playing.value = false
      following.value = false
      started.value = false
    }
  }
  raf = requestAnimationFrame(step)
}

function toggle() {
  if (!playing.value && playhead.value >= trackEnd.value) playhead.value = 0
  playing.value = !playing.value
  if (playing.value && !following.value) started.value = true
}
function seek(e: Event) {
  playhead.value = Number((e.target as HTMLInputElement).value)
  started.value = true
}
function jumpToLive() {
  playhead.value = Math.max(0, trackEnd.value - 60000)
}
const clock = computed(() => {
  const total = Math.floor(playhead.value / 1000)
  return `${Math.floor(total / 60) + 1}'`
})

const statusText = computed(() => {
  if (!match.value) return 'Not started'
  if (gated.value) return `${following.value ? 'Live' : 'Replay'} · ${clock.value}`
  if (match.value.status === 'in_progress') return `Live · ${match.value.minute}'`
  if (match.value.status === 'completed') return 'Full-time'
  return match.value.status
})

onMounted(async () => {
  await matchStore.watch(fixtureId.value).catch(() => { })
  if (match.value?.visual && match.value.status === 'in_progress') {
    // Join a live match at its latest minute rather than from kick-off.
    following.value = true
    playing.value = true
    jumpToLive()
  }
  raf = requestAnimationFrame(step)
})
onUnmounted(() => {
  cancelAnimationFrame(raf)
  matchStore.disconnect()
})
</script>

<template>
  <main class="min-h-screen bg-slate-900 text-slate-200 px-6 py-10">
    <div class="max-w-3xl mx-auto space-y-6">
      <NuxtLink to="/competitions" class="text-slate-400 text-sm hover:text-indigo-300">← Competitions</NuxtLink>

      <template v-if="fixture">
        <header class="bg-slate-800 border border-slate-700 rounded-lg p-6">
          <div class="flex items-center justify-between gap-4 text-center">
            <div class="flex-1 font-semibold text-white">{{ fixture.home_club.name }}</div>
            <div class="shrink-0">
              <div class="text-4xl font-bold text-white">
                {{ score ? `${score.home}–${score.away}` : '–' }}
              </div>
              <div class="mt-2 inline-block rounded px-3 py-1 text-xs font-semibold" aria-live="polite" :class="match?.status === 'in_progress' ? 'bg-emerald-800 text-emerald-200' :
                match?.status === 'completed' ? 'bg-indigo-800 text-indigo-200' :
                  'bg-slate-700 text-slate-300'">
                {{ statusText }}
              </div>
            </div>
            <div class="flex-1 font-semibold text-white">{{ fixture.away_club.name }}</div>
          </div>
        </header>

        <p v-if="error" class="text-red-400 text-sm">{{ error }}</p>
        <p v-if="!match" class="text-slate-500 text-sm">
          This fixture has not kicked off yet — check back on matchday.
        </p>

        <section v-if="match && visual" class="bg-slate-800 border border-slate-700 rounded-lg p-4 space-y-3">
          <MatchPitch :track="track" :players="trackPlayers" :playhead="playhead"
            :label="`2D view of ${fixture.home_club.name} against ${fixture.away_club.name}, ${clock}. The match events list below describes the play.`" />
          <div v-if="following" class="flex items-center gap-3 text-sm">
            <span class="text-slate-400">The picture runs one step behind the live match.</span>
            <button v-if="behindLive" class="rounded bg-indigo-600 px-3 py-1" @click="jumpToLive">Jump to live</button>
          </div>
          <div v-else class="flex flex-wrap items-center gap-3 text-sm">
            <button class="rounded bg-indigo-600 px-3 py-1" @click="toggle">{{ playing ? 'Pause' : 'Play replay' }}</button>
            <label class="flex items-center gap-2">Speed
              <select v-model.number="speed" class="rounded bg-slate-900 p-1">
                <option v-for="s in speeds" :key="s" :value="s">{{ s }}×</option>
              </select>
            </label>
            <input type="range" min="0" :max="trackEnd" step="1000" :value="playhead" class="min-w-40 flex-1"
              aria-label="Replay position" @input="seek">
            <span class="w-10 text-right font-mono tabular-nums text-slate-400">{{ clock }}</span>
          </div>
          <table v-if="stats.length" class="w-full text-sm">
            <caption class="sr-only">Match statistics</caption>
            <tbody>
              <tr v-for="row in stats" :key="row.label" class="border-t border-slate-700">
                <td class="w-16 py-1 text-right tabular-nums text-white">{{ row.home }}</td>
                <th scope="row" class="py-1 text-center font-normal text-slate-400">{{ row.label }}</th>
                <td class="w-16 py-1 tabular-nums text-white">{{ row.away }}</td>
              </tr>
            </tbody>
          </table>
        </section>

        <section v-if="match" class="bg-slate-800 border border-slate-700 rounded-lg p-4">
          <div v-if="match.status === 'in_progress'" class="mb-5 rounded border border-slate-700 bg-slate-900 p-3">
            <label class="mr-3 text-sm font-medium">Live style
              <select v-model="liveStyle" class="ml-2 rounded bg-slate-800 p-1 text-sm">
                <option value="balanced">Balanced</option>
                <option value="possession">Possession</option>
                <option value="gegenpress">Gegenpress</option>
                <option value="low_block">Low-block</option>
                <option value="direct">Direct</option>
              </select>
            </label><button @click="changeStyle" class="rounded bg-indigo-600 px-3 py-1 text-sm">Apply next
              minute</button>
            <p v-if="tacticalError" class="mt-2 text-sm text-red-400">{{ tacticalError }}</p>
          </div>
          <h2 class="text-lg font-semibold text-white mb-3">Match events</h2>
          <ul v-if="shownEvents.length" class="divide-y divide-slate-700">
            <li v-for="ev in shownEvents" :key="ev.id" class="flex items-start gap-3 py-2 text-sm">
              <span class="w-10 shrink-0 text-right font-mono text-slate-400 tabular-nums">{{ ev.minute }}'</span>
              <span class="w-28 shrink-0 font-semibold" :class="ev.type === 'goal' || ev.type === 'penalty_scored' ? 'text-emerald-400' :
                ev.type === 'red_card' || ev.type === 'injury' ? 'text-red-400' :
                  ev.type === 'yellow_card' ? 'text-yellow-400' :
                    ev.type === 'half_time' || ev.type === 'full_time' ? 'text-indigo-300' :
                      'text-slate-300'">
                {{ labels[ev.type] }}</span>
              <span class="text-slate-300/90">{{ ev.detail?.commentary ?? ev.detail?.detail ?? ev.type }}
                <NuxtLink v-if="ev.player?.id" :to="`/play/players/${ev.player.id}`"
                  class="ml-2 text-xs text-slate-500 hover:text-slate-300 underline-offset-2 hover:underline">
                  player card
                </NuxtLink>
              </span>
            </li>
          </ul>
          <p v-else-if="match.status === 'in_progress' || gated" class="text-slate-500 text-sm">
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
