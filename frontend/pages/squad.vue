<script setup lang="ts">
import { useSquadStore } from '~/stores/squad'

const squad = useSquadStore()
const clubId = ref('')
const clubName = ref('')
const error = ref('')
const saved = ref(false)
const slots = ref<Record<number, string>>({})

async function init() {
  const { authedFetch } = useAuth()
  const r = await authedFetch('/api/clubs')
  const clubs = await r.json()
  clubId.value = clubs[0]?.id ?? ''
  clubName.value = clubs[0]?.name ?? ''
  if (!clubId.value) return
  await squad.fetchSquad(clubId.value)
  await squad.fetchLineup(clubId.value)
  const bySlot: Record<number, string> = {}
  for (const s of squad.lineup?.slots ?? []) {
    bySlot[s.slot] = s.player_id
  }
  slots.value = bySlot
}

const rosterOptions = computed(() =>
  (squad.players ?? []).map((p) => ({
    id: String(p.id),
    label: `${p.display_name ?? p.first_name ?? p.id}${p.primary_position ? ` (${p.primary_position})` : ''}`,
  })),
)

const orderedSlots = computed(() => {
  const source = squad.lineup?.slots ?? []
  return [...source].sort((a, b) => a.slot - b.slot)
})

const unfilled = computed(() =>
  orderedSlots.value.filter((s) => !slots.value[s.slot] || slots.value[s.slot].trim() === '').length,
)

const duplicatePlayers = computed(() => {
  const seen = new Set<string>()
  const dups = new Set<string>()
  for (const s of orderedSlots.value) {
    const pid = slots.value[s.slot] ?? ''
    if (!pid) continue
    if (seen.has(pid)) dups.add(pid)
    seen.add(pid)
  }
  return [...dups]
})

const canSave = computed(() => unfilled.value === 0 && duplicatePlayers.value.length === 0 && !squad.saving)

async function save() {
  if (!clubId.value || !canSave.value) return
  error.value = ''
  saved.value = false
  try {
    const payload = orderedSlots.value.map((s) => ({ slot: s.slot, player_id: slots.value[s.slot] }))
    await squad.saveLineup(clubId.value, payload)
    saved.value = true
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Could not save lineup.'
  }
}

onMounted(() => init().catch((e) => (error.value = e instanceof Error ? e.message : 'Could not load your squad.')))
</script>

<template>
  <main class="min-h-screen bg-slate-900 px-6 py-10 text-slate-200">
    <div class="mx-auto max-w-4xl space-y-6">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="text-3xl font-bold text-white">Squad</h1>
          <p v-if="clubName" class="text-slate-400">{{ clubName }} — pick your starting XI</p>
          <p v-else class="text-slate-400">{{ squad.players.length }} players available</p>
        </div>
        <button
          :disabled="!canSave"
          class="rounded bg-indigo-600 px-4 py-2 font-medium text-white disabled:cursor-not-allowed disabled:bg-slate-700"
          @click="save"
        >
          {{ squad.saving ? 'Saving…' : 'Save lineup' }}
        </button>
      </div>

      <p v-if="error" class="text-red-400">{{ error }}</p>
      <p v-if="saved" class="text-emerald-400">Lineup saved.</p>

      <div v-if="unfilled" class="text-sm text-amber-400">{{ unfilled }} slot(s) still empty — fill all 11 to field an XI.</div>
      <div v-if="duplicatePlayers.length" class="text-sm text-amber-400">Duplicate players selected — each player can only start once.</div>

      <div class="grid gap-2 sm:grid-cols-2">
        <div
          v-for="s in orderedSlots"
          :key="s.slot"
          class="flex items-center gap-3 rounded border border-slate-700 bg-slate-800 p-2"
        >
          <span class="w-24 text-xs uppercase tracking-wide text-slate-400">{{ s.position }}</span>
          <select
            v-model="slots[s.slot]"
            class="w-full rounded bg-slate-900 p-2 text-sm"
            :class="duplicatePlayers.includes(slots[s.slot]) ? 'text-amber-300' : ''"
          >
            <option value="">— select —</option>
            <option v-for="p in rosterOptions" :key="p.id" :value="p.id">{{ p.label }}</option>
          </select>
        </div>
      </div>

      <p v-if="!squad.players.length" class="p-4 text-slate-500">No squad is available yet.</p>
    </div>
  </main>
</template>