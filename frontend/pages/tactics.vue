<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useTacticsStore } from '~/stores/tactics'

const store = useTacticsStore()
const { value, saving } = storeToRefs(store)
const clubId = ref('')
const style = ref('balanced')
const formation = ref('4-3-3')
const error = ref('')
const saved = ref(false)

// Mirrors backend internal/squad/lineup.go AllowedFormations; canonical/default
// first. The server (internal/tactics.SetTactics) stays authoritative at save.
const allowedByStyle: Record<string, string[]> = {
  balanced: ['4-3-3', '4-4-2', '4-2-3-1', '5-3-2'],
  possession: ['4-3-3', '3-2-4-1'],
  gegenpress: ['4-3-3', '4-2-3-1'],
  low_block: ['5-4-1', '4-5-1'],
  direct: ['4-4-2', '3-5-2'],
}

const styles = [
  { key: 'balanced', label: 'Balanced' },
  { key: 'possession', label: 'Possession Control' },
  { key: 'gegenpress', label: 'Gegenpress' },
  { key: 'low_block', label: 'Low-Block / Counter' },
  { key: 'direct', label: 'Direct / Long-Ball' },
]

const allowedFormations = computed(() => allowedByStyle[style.value] ?? allowedByStyle.balanced)

async function init() {
  const { authedFetch } = useAuth()
  const r = await authedFetch('/api/clubs')
  const clubs = await r.json()
  clubId.value = clubs[0]?.id ?? ''
  if (!clubId.value) return
  await store.load(clubId.value)
  style.value = value.value?.style ?? style.value
  formation.value = value.value?.formation ?? allowedFormations.value[0]
}

async function save() {
  if (!clubId.value) return
  error.value = ''
  saved.value = false
  try {
    await store.save(clubId.value, style.value, formation.value)
    saved.value = true
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not save tactics.'
  }
}

// Local-only validation against the *selected* style (the server was reloaded
// unnecessarily before, and only reflected the last-saved style).
watch(style, (next) => {
  const allowed = allowedByStyle[next] ?? allowedByStyle.balanced
  if (!allowed.includes(formation.value)) formation.value = allowed[0]
})

onMounted(() => init().catch(() => (error.value = 'Could not load your club.')))
</script>

<template>
  <main class="min-h-screen bg-slate-900 px-6 py-10 text-slate-200">
    <div class="mx-auto max-w-3xl space-y-6">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="text-3xl font-bold text-white">Tactics</h1>
          <p class="text-slate-400">Choose one Simple-Mode style. The server freezes it at kick-off.</p>
        </div>
        <button
          :disabled="saving"
          class="rounded bg-indigo-600 px-4 py-2 font-medium text-white disabled:bg-slate-700"
          @click="save"
        >
          {{ saving ? 'Saving…' : 'Save tactics' }}
        </button>
      </div>

      <p v-if="error" class="text-red-400">{{ error }}</p>
      <p v-if="saved" class="text-emerald-400">Tactics saved.</p>

      <div class="grid gap-3 sm:grid-cols-2">
        <button
          v-for="s in styles"
          :key="s.key"
          class="rounded border p-4 text-left"
          :class="style === s.key ? 'border-indigo-400 bg-indigo-950' : 'border-slate-700 bg-slate-800'"
          @click="style = s.key"
        >
          <strong>{{ s.label }}</strong>
          <span class="block text-sm text-slate-400">{{ s.key }}</span>
        </button>
      </div>

      <label class="block">
        Formation
        <select v-model="formation" class="ml-3 rounded bg-slate-800 p-2">
          <option v-for="f in allowedFormations" :key="f">{{ f }}</option>
        </select>
      </label>
    </div>
  </main>
</template>