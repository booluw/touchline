<script setup lang="ts">
import { useTrainingStore } from '~/app/stores/training'
const store = useTrainingStore(); const clubId = ref(''); const selected = ref('technical'); const error = ref('')
const plans = [['technical', 'Technical', 'Passing, first touch and composure.'], ['physical', 'Physical', 'Stamina, strength and work rate.'], ['defensive', 'Defensive', 'Positioning, tackling and marking.'], ['attacking', 'Attacking', 'Finishing, movement and pace.'], ['recovery', 'Recovery', 'Reduce fatigue and injury risk.']]
async function init() { const { authedFetch } = useAuth(); const r = await authedFetch('/api/clubs'); const clubs = await r.json(); clubId.value = clubs[0]?.id ?? ''; if (clubId.value) { await store.load(clubId.value); selected.value = store.value?.archetype ?? selected.value } }
async function save() { try { await store.save(clubId.value, selected.value); error.value = '' } catch (e) { error.value = e instanceof Error ? e.message : 'Could not save plan.' } }
onMounted(() => init().catch(() => error.value = 'Could not load your club.'))
</script>
<template>
  <main class="min-h-screen bg-slate-900 px-6 py-10 text-slate-200">
    <div class="mx-auto max-w-3xl space-y-6">
      <h1 class="text-3xl font-bold text-white">Weekly training</h1>
      <p class="text-slate-400">Your plan applies on the next weekly world tick.</p>
      <p v-if="error" class="text-red-400">{{ error }}</p>
      <div class="grid gap-3 sm:grid-cols-2"><button v-for="p in plans" :key="p[0]" @click="selected = p[0]"
          class="rounded border p-4 text-left"
          :class="selected === p[0] ? 'border-emerald-400 bg-emerald-950' : 'border-slate-700 bg-slate-800'"><strong>{{ p[1] }}</strong><span
            class="block text-sm text-slate-400">{{ p[2] }}</span></button></div><button @click="save"
        :disabled="store.saving"
        class="rounded bg-emerald-600 px-4 py-2 font-medium text-white">{{ store.saving ? 'Saving…' : 'Set training
        plan'}}</button>
    </div>
  </main>
</template>
