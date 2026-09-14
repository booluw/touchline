<template>
  <section class="w-full max-w-2xl mx-auto text-left space-y-4">
    <div v-if="status === 'loading'" class="rounded-lg bg-slate-900 border border-slate-700 p-6">
      <p class="text-slate-400">Checking your world…</p>
    </div>

    <div v-else-if="status === 'signed-out'" class="rounded-lg bg-slate-900 border border-slate-700 p-6 text-center">
      <p class="text-slate-300 mb-4">Sign in to open your world. Ticks, offers and transfers arrive live on the socket once you're in.</p>
      <NuxtLink
        to="/auth/login"
        class="inline-block px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-500"
      >
        Sign in
      </NuxtLink>
    </div>

    <div v-else-if="status === 'error'" class="rounded-lg bg-slate-900 border border-red-800 p-6">
      <p class="text-red-400 mb-4">{{ errorMessage }}</p>
      <button
        type="button"
        class="px-4 py-2 bg-slate-700 text-white rounded-lg hover:bg-slate-600"
        @click="load"
      >
        Retry
      </button>
    </div>

    <div v-else class="space-y-4">
      <div class="rounded-lg bg-slate-900 border border-slate-700 p-6 space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="text-lg font-semibold text-white">Live world tick</h2>
          <span
            class="inline-flex items-center gap-1.5 text-xs font-medium"
            :class="connectionClass"
          >
            <span class="w-2 h-2 rounded-full" :class="connectionDot" />
            {{ connectionLabel }}
          </span>
        </div>
        <p v-if="realtime.lastTick" class="text-slate-300">
          <span class="font-semibold text-white">{{ granularityLabel }} tick #{{ realtime.lastTick.tick }}</span>
          <span class="text-slate-500"> · fired {{ tickTimeLabel }}</span>
        </p>
        <p v-else class="text-slate-500">
          No tick yet this session — the first one will appear here live from the server, no simulation.
        </p>
      </div>

      <div class="rounded-lg bg-slate-900 border border-slate-700 p-6">
        <h2 class="text-lg font-semibold text-white mb-3">Your club</h2>
        <div v-if="clubDetail">
          <p class="text-slate-300">
            <span class="font-semibold text-white">{{ clubDetail.name }}</span>
            <span v-if="clubDetail.manager?.is_policy_bot" class="text-slate-500"> · run by the AI policy-bot</span>
          </p>
          <p v-if="clubDetail.squad" class="text-slate-500 text-sm mt-1">
            {{ clubDetail.squad.length }} players in the squad.
          </p>
        </div>
        <p v-else-if="clubList.length > 0" class="text-slate-300">
          {{ clubList.map((c) => c.name).join(', ') }}
        </p>
        <p v-else class="text-slate-500">
          You're in a world but don't manage a club yet. When a world starts, an AI club offers you the job.
        </p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
const { authedFetch } = useAuth()
const realtime = useRealtimeStore()
const socket = useSocket()

type PageStatus = 'loading' | 'signed-out' | 'connected' | 'error'

const status = ref<PageStatus>('loading')
const errorMessage = ref('')
const clubList = ref<{ id: string; name: string }[]>([])
const clubDetail = ref<{ name: string; manager?: { is_policy_bot?: boolean }; squad?: unknown[] } | null>(null)

const connectionLabel = computed(() => {
  switch (socket.status.value) {
    case 'open':
      return 'Connected'
    case 'reconnecting':
      return 'Reconnecting…'
    default:
      return 'Offline'
  }
})

const connectionClass = computed(() => {
  switch (socket.status.value) {
    case 'open':
      return 'text-emerald-400'
    case 'reconnecting':
      return 'text-amber-400'
    default:
      return 'text-slate-500'
  }
})

const connectionDot = computed(() => {
  switch (socket.status.value) {
    case 'open':
      return 'bg-emerald-400'
    case 'reconnecting':
      return 'bg-amber-400 animate-pulse'
    default:
      return 'bg-slate-500'
  }
})

const granularityLabel = computed(() => {
  const gran = realtime.lastTick?.granularity
  if (!gran) return ''
  return gran.charAt(0).toUpperCase() + gran.slice(1)
})

const tickTimeLabel = computed(() => {
  const ts = realtime.lastEvent?.ts
  if (!ts) return ''
  return new Date(ts).toLocaleTimeString()
})

async function load() {
  status.value = 'loading'
  errorMessage.value = ''
  clubList.value = []
  clubDetail.value = null

  try {
    const res = await authedFetch('/api/clubs')
    if (res.status === 401) {
      status.value = 'signed-out'
      return
    }
    if (res.status === 403) {
      const body = (await res.json().catch(() => ({}))) as { error?: string }
      status.value = 'error'
      errorMessage.value = body.error ?? 'Your session has no world context — contact support.'
      return
    }
    if (!res.ok) {
      const body = (await res.json().catch(() => ({}))) as { error?: string }
      status.value = 'error'
      errorMessage.value = body.error ?? 'Could not load your world. Try again.'
      return
    }

    const { clubs } = (await res.json()) as { clubs: { id: string; name: string }[] }
    clubList.value = clubs ?? []
    if (clubs.length > 0) {
      const detail = await authedFetch(`/api/clubs/${clubs[0].id}`)
      if (detail.ok) {
        clubDetail.value = (await detail.json()) as typeof clubDetail.value
      }
    }
    status.value = 'connected'
    realtime.connect()
  } catch {
    status.value = 'error'
    errorMessage.value = 'Could not reach the server. Check that the API is running, then retry.'
  }
}

onMounted(load)
</script>