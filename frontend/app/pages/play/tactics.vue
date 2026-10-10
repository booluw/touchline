<script setup lang="ts">
import { useManagerTactics } from '~/composables/manager/tactics'
import type { LoadingStatus } from '~/types'
import type { ManagerSquadPlayer, TacticalAdvice, TacticStyle, TeamInstructions } from '~/types/manager'
import type { SegmentOption, WhyFactor } from '~/types/ui/design'

const viewport = useViewport()
const clubstore = useClubStore()
const { getTactics, getAdvice, getLineup, getSquad, savePlan } = useManagerTactics()

const clubId = computed(() => clubstore.club?.id)
const status = ref<LoadingStatus>('loading')
const saving = ref(false)
const style = ref<TacticStyle>('balanced')
const formation = ref('4-3-3')
const instructions = ref<TeamInstructions>({ mentality: 'balanced', pressing: 'mid', width: 'normal', tempo: 'normal' })
const xi = ref<(string | null)[]>(Array(11).fill(null))
const squad = ref<ManagerSquadPlayer[]>([])
const saved = ref('')
const advice = ref<TacticalAdvice | null>()

// Mirrors backend internal/squad/lineup.go AllowedFormations; the server
// stays authoritative at save.
const allowedByStyle: Record<TacticStyle, string[]> = {
  balanced: ['4-3-3', '4-4-2', '4-2-3-1', '5-3-2'],
  possession: ['4-3-3', '3-2-4-1'],
  gegenpress: ['4-3-3', '4-2-3-1'],
  low_block: ['5-4-1', '4-5-1'],
  direct: ['4-4-2', '3-5-2'],
}
const styles: SegmentOption<TacticStyle>[] = [
  { value: 'balanced', label: 'Balanced' },
  { value: 'possession', label: 'Possession' },
  { value: 'gegenpress', label: 'Gegenpress' },
  { value: 'low_block', label: 'Low block' },
  { value: 'direct', label: 'Direct' },
]
const dials: { key: keyof TeamInstructions, label: string, options: SegmentOption[] }[] = [
  { key: 'mentality', label: 'Mentality', options: [{ value: 'cautious', label: 'Cautious' }, { value: 'balanced', label: 'Balanced' }, { value: 'positive', label: 'Positive' }] },
  { key: 'pressing', label: 'Pressing', options: [{ value: 'low', label: 'Low' }, { value: 'mid', label: 'Mid' }, { value: 'high', label: 'High' }] },
  { key: 'width', label: 'Width', options: [{ value: 'narrow', label: 'Narrow' }, { value: 'normal', label: 'Normal' }, { value: 'wide', label: 'Wide' }] },
  { key: 'tempo', label: 'Tempo', options: [{ value: 'patient', label: 'Patient' }, { value: 'normal', label: 'Normal' }, { value: 'direct', label: 'Direct' }] },
]
const formations = computed(() => allowedByStyle[style.value].map(f => ({ value: f, label: f })))
const snapshot = () => JSON.stringify([style.value, formation.value, instructions.value, xi.value])
const xiComplete = computed(() => xi.value.every(Boolean))
const dirty = computed(() => saved.value !== snapshot())
const fitFactors = computed<WhyFactor[]>(() => (advice.value?.fit.factors ?? []).map(f => ({ label: f.label, value: f.delta })))
const followsAdvice = computed(() => !!advice.value && JSON.stringify(advice.value.suggested) === JSON.stringify(instructions.value))
const kickoff = computed(() => advice.value
  ? new Date(advice.value.scheduled_at).toLocaleString(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit' })
  : '')

watch(style, (next) => {
  if (!allowedByStyle[next].includes(formation.value)) formation.value = allowedByStyle[next][0]!
})

async function load(id: string) {
  status.value = 'loading'
  const [t, a, l, sq] = await Promise.all([getTactics(id), getAdvice(id), getLineup(id), getSquad(id)])
  if (!t || !l || !sq) { status.value = 'error'; return }
  squad.value = sq
  xi.value = Array.from({ length: 11 }, (_, i) => l.slots.find(s => s.slot === i)?.player?.id ?? null)
  style.value = t.style
  await nextTick() // let the style watcher settle before restoring the saved formation
  formation.value = t.formation
  instructions.value = { ...t.instructions }
  saved.value = snapshot()
  advice.value = a
  status.value = 'loaded'
}

async function save() {
  if (!clubId.value || !xiComplete.value) return
  saving.value = true
  const ok = await savePlan(clubId.value, { style: style.value, formation: formation.value, instructions: instructions.value, xi: xi.value as string[] })
  saving.value = false
  if (!ok) return
  saved.value = snapshot()
  // Fit is scored server-side against the saved dials.
  advice.value = await getAdvice(clubId.value)
}

async function applySuggested() {
  if (!advice.value) return
  instructions.value = { ...advice.value.suggested }
  await save()
}

watch(clubId, id => id && load(id), { immediate: true })
</script>

<template>
  <main class="space-y-5">
    <div class="sticky top-0 bg-bg z-9 flex items-center justify-between gap-3 py-5">
      <div class="w-3/5 flex flex-col gap-1">
        <h2 class="text-page">Tactics</h2>
        <p class="text-t2">
          <template v-if="advice">Plan for {{ advice.opponent.name }} ({{ advice.home_or_away === 'home' ? 'H' : 'A' }}). </template>
          Changes apply from the next match.
        </p>
      </div>
      <div class="flex flex-col items-end gap-1">
        <UiButton variant="primary" :loading="saving" :disabled="!dirty || !xiComplete || status !== 'loaded'" @click="save">
          {{ dirty ? 'Save tactics' : 'Saved' }}
        </UiButton>
        <span v-if="status === 'loaded' && !xiComplete" class="text-meta text-urgent">Fill all 11 slots to save.</span>
      </div>
    </div>

    <UiEmptyState v-if="status === 'error'" title="Tactics unavailable" />
    <div v-else-if="status === 'loading'" class="grid gap-5 md:grid-cols-12">
      <UiCard class="space-y-3 md:col-span-7"><UiLoader class="h-5 w-40" /><UiLoader class="h-20 w-full" /></UiCard>
      <UiCard class="space-y-3 md:col-span-5"><UiLoader class="h-5 w-32" /><UiLoader class="h-16 w-full" /></UiCard>
    </div>

    <section v-else class="grid gap-5 md:grid-cols-12">
      <aside v-if="viewport.isLessThan('tablet')" class="flex flex-col gap-4 md:col-span-5">
        <UiCard v-if="advice === null" dashed>
          <span class="text-meta text-t2">No upcoming fixture. The assistant briefs you once the next match is
            scheduled.</span>
        </UiCard>
        <template v-else-if="advice">
          <UiDecisionCard tier="interesting" :eyebrow="`Opponent · ${kickoff}`" :title="advice.headline">
            {{ advice.summary }}
            <span v-if="advice.profile.matches" class="mt-1 block text-t3">
              Last {{ advice.profile.matches }}:
              <template v-if="advice.profile.xg_for !== undefined">{{ advice.profile.xg_for }} xG for · {{
                advice.profile.xg_against }} xG against</template>
              <template v-else>{{ advice.profile.goals_for }} scored · {{ advice.profile.goals_against }} conceded per
                game</template>
            </span>
            <template v-if="advice.fit.factors.length" #actions>
              <UiButton variant="primary" size="sm" :disabled="followsAdvice" :loading="saving" @click="applySuggested">
                {{ followsAdvice ? 'Plan applied' : 'Apply suggested plan' }}
              </UiButton>
            </template>
          </UiDecisionCard>
          <span v-if="dirty && fitFactors.length" class="text-meta text-t3">Fit reflects your saved plan. Save to
            rescore it.</span>
        </template>
      </aside>
      <div class="flex flex-col gap-4 md:col-span-7">
        <TacticsLineupEditor v-model="xi" :formation="formation" :players="squad">
          <div class="flex flex-col gap-2">
            <span class="text-label label-caps text-t3">Formation</span>
            <UiSegmentedControl v-model="formation" label="Formation" :options="formations" numeric
              class="self-start" />
          </div>
        </TacticsLineupEditor>

        <UiCard v-if="viewport.isLessThan('tablet')" class="flex flex-col gap-3">
          <div class="flex flex-col gap-3 mb-5">
            <span class="text-label label-caps text-t3">Style</span>
            <UiSegmentedControl v-model="style" label="Playing style" :options="styles" class="flex-wrap self-start" />
          </div>
          <span class="text-label label-caps text-t3">Team instructions</span>
          <div v-for="d in dials" :key="d.key" class="flex flex-col gap-1.5">
            <span class="text-meta text-t2">{{ d.label }}</span>
            <UiSegmentedControl v-model="instructions[d.key]" :label="d.label" :options="d.options" class="grid grid-cols-3" />
          </div>
        </UiCard>
      </div>

      <aside v-if="!viewport.isLessThan('tablet')" class="flex flex-col gap-4 md:col-span-5">
        <UiCard v-if="advice === null" dashed>
          <span class="text-meta text-t2">No upcoming fixture. The assistant briefs you once the next match is scheduled.</span>
        </UiCard>
        <template v-else-if="advice">
          <UiDecisionCard tier="interesting" :eyebrow="`Opponent · ${kickoff}`" :title="advice.headline">
            {{ advice.summary }}
            <span v-if="advice.profile.matches" class="mt-1 block text-t3">
              Last {{ advice.profile.matches }}:
              <template v-if="advice.profile.xg_for !== undefined">{{ advice.profile.xg_for }} xG for · {{ advice.profile.xg_against }} xG against</template>
              <template v-else>{{ advice.profile.goals_for }} scored · {{ advice.profile.goals_against }} conceded per game</template>
            </span>
            <template v-if="advice.fit.factors.length" #actions>
              <UiButton variant="primary" size="sm" :disabled="followsAdvice" :loading="saving" @click="applySuggested">
                {{ followsAdvice ? 'Plan applied' : 'Apply suggested plan' }}
              </UiButton>
            </template>
          </UiDecisionCard>
          <UiWhyBreakdown
            v-if="fitFactors.length"
            standalone
            :subject="`Tactical fit vs ${advice.opponent.name}`"
            :value="`${advice.fit.score} / 100`"
            :factors="fitFactors"
          />
          <span v-if="dirty && fitFactors.length" class="text-meta text-t3">Fit reflects your saved plan. Save to rescore it.</span>
        </template>

        <UiCard class="flex flex-col gap-3">
          <div class="flex flex-col gap-3 mb-5">
            <span class="text-label label-caps text-t3">Style</span>
            <UiSegmentedControl v-model="style" label="Playing style" :options="styles" class="flex-wrap self-start" />
          </div>
          <span class="text-label label-caps text-t3">Team instructions</span>
          <div v-for="d in dials" :key="d.key" class="flex flex-col gap-1.5">
            <span class="text-meta text-t2">{{ d.label }}</span>
            <UiSegmentedControl v-model="instructions[d.key]" :label="d.label" :options="d.options"
              class="grid grid-cols-3" />
          </div>
        </UiCard>
      </aside>
    </section>
  </main>
</template>
