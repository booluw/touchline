<script setup lang="ts">
// Squad player detail (IM63). Desktop renders it as the squad page's side
// panel; mobile renders it full screen on /play/players/[id] with `tabbed`.
// Everything shown is stored or computed by the engine: no potential (OPD-60).
// Morale "Why?" is the target's exact terms (IM64); the request preview is the
// constants each answer applies (IM65).
import { useManagerSquad, type RequestAction } from '~/composables/manager/squad'
import type { ManagerSquadPlayer, PlayerMoraleDetail, PricePreset, RequestPreview, SquadDynamics } from '~/types/manager'
import type { SegmentOption, SemanticTone, WhyFactor } from '~/types/ui/design'

const props = defineProps<{
  detail: PlayerMoraleDetail
  row?: ManagerSquadPlayer
  dynamics?: SquadDynamics | null
  tabbed?: boolean
  busy?: boolean
}>()
const emit = defineEmits<{ respond: [action: RequestAction, preset?: PricePreset] }>()
const clubstore = useClubStore()
const { getPreview } = useManagerSquad()

type Tab = 'overview' | 'attributes' | 'personality' | 'contract'
const tabs: SegmentOption<Tab>[] = [
  { value: 'overview', label: 'Overview' },
  { value: 'attributes', label: 'Attributes' },
  { value: 'personality', label: 'Personality' },
  { value: 'contract', label: 'Contract' },
]
const tab = ref<Tab>('overview')
const show = (t: Tab) => !props.tabbed || tab.value === t

const humanize = (s: string) => s.replace(/_/g, ' ').replace(/^\w/, c => c.toUpperCase())
const money = (units: number) => formatMoneyCompact(units * 100)
const date = (iso: string) => new Date(iso).toLocaleDateString(undefined, { month: 'short', year: 'numeric' })

const morale = computed(() => Math.round(props.detail.morale * 100))
const moodWord = computed(() => moodLabel(morale.value))
const ovr = computed(() => props.row?.overall)

const request = computed(() => props.detail.transfer_request?.status === 'pending' ? props.detail.transfer_request : undefined)
const options: { action: RequestAction, label: string, sub: string, cta: string }[] = [
  { action: 'approve', label: 'Approve and transfer-list', sub: 'He is listed open to offers.', cta: 'List player' },
  { action: 'reassure', label: 'Deny, but promise playing time', sub: 'The request pauses while the promise is judged.', cta: 'Make promise' },
  { action: 'deny', label: 'Deny outright', sub: 'Morale drops and he cannot ask again for a while.', cta: 'Deny request' },
]
const choice = ref<(typeof options)[number] | null>(null)
const preview = ref<RequestPreview | null>(null)
const preset = ref<PricePreset>('valuation')
watch(() => props.detail.player.id, () => { choice.value = null; tab.value = 'overview' })
watch(() => [request.value?.id, props.detail.player.id] as const, async ([reqId, playerId]) => {
  preview.value = null
  preset.value = 'valuation'
  const clubId = clubstore.club?.id
  if (!reqId || !clubId) return
  const p = await getPreview(clubId, playerId)
  if (props.detail.player.id === playerId) preview.value = p ?? null
}, { immediate: true })
const chosen = computed(() => preview.value?.options.find(o => o.action === choice.value?.action))
const effects = computed<WhyFactor[]>(() => toWhy(chosen.value && { factors: chosen.value.effects }))
const priceOptions = computed<SegmentOption<PricePreset>[]>(() => (preview.value?.prices ?? [])
  .map(p => ({ value: p.preset, label: `${money(p.asking_price)} · ${p.label}` })))
const moraleWhy = computed<WhyFactor[]>(() => toWhy(props.detail.explanation?.why))

const emotion = computed(() => {
  const now = Date.now()
  return props.detail.dossier.private?.emotional_states
    .find(e => !e.expires_at || new Date(e.expires_at).getTime() > now)
})

const condition = computed(() => {
  const c = props.detail.dossier.condition
  if (!c) return []
  const pct = (v: number) => Math.round(v * 100)
  return [
    { label: 'Morale', value: morale.value, tone: (morale.value < 35 ? 'neg' : morale.value < 55 ? 'important' : 'neutral') as SemanticTone },
    { label: 'Fitness', value: pct(c.fitness), tone: (c.fitness < 0.7 ? 'neg' : 'neutral') as SemanticTone },
    { label: 'Sharpness', value: pct(c.sharpness), tone: 'neutral' as SemanticTone },
    { label: 'Fatigue', value: pct(c.fatigue), tone: (c.fatigue > 0.6 ? 'neg' : 'neutral') as SemanticTone },
  ]
})

const expectation = computed(() => props.detail.expectations[0])
const expectationTone: Record<string, SemanticTone> = { satisfied: 'pos', neutral: 'neutral', unhappy: 'neg', free: 'neutral' }

// Stored 1–100; shown 1–20 to match the design and AttributeRating thresholds.
const attrGroups = computed(() => Object.entries(props.detail.dossier.attribute_values)
  .filter(([cat, items]) => cat !== 'positional' && Object.keys(items).length)
  .map(([cat, items]) => ({
    name: humanize(cat),
    items: Object.entries(items).map(([k, v]) => ({ label: humanize(k), value: Math.ceil(v / 5) })),
  })))

const traits = computed(() => Object.entries(props.detail.dossier.personality ?? {})
  .map(([k, v]) => ({ label: humanize(k), value: Math.ceil(v / 5) })))

const faction = computed(() => props.dynamics?.factions.find(f =>
  f.members.some(m => m.id === props.detail.player.id) || f.leader?.id === props.detail.player.id))

const contract = computed(() => {
  const c = props.detail.dossier.private?.contracts.find(x => x.status === 'active')
  const rows: { k: string, v: string }[] = []
  if (c) {
    rows.push({ k: 'Wage', v: `${money(c.weekly_wage)}/wk` }, { k: 'Expires', v: date(c.end_date) })
    rows.push({ k: 'Release clause', v: c.release_clause ? money(c.release_clause) : 'None' })
    if (c.playing_time_promise) rows.push({ k: 'Promise', v: humanize(c.playing_time_promise) })
  }
  rows.push({ k: 'Squad role', v: humanize(props.detail.squad_role) }, { k: 'Value', v: money(props.detail.dossier.bio.market_value) })
  return rows
})
</script>

<template>
  <!-- Header card (and tabs) stay put; the cards below scroll. -->
  <div class="flex h-full min-h-0 flex-col gap-4">
    <UiCard class="flex items-start gap-3">
      <div class="flex min-w-0 flex-1 flex-col gap-1">
        <span class="text-title font-semibold">{{ detail.player.name }}</span>
        <span class="text-meta text-t2">
          {{ detail.position }}<template v-if="row?.age"> · {{ row.age }}</template><template v-if="row?.nationality"> · {{ row.nationality.name }}</template>
          · <span :class="morale < 45 ? 'text-neg' : 'text-t2'">{{ moodWord }}</span>
        </span>
        <div class="flex flex-wrap gap-1.5">
          <UiStatusBadge v-if="request" tone="urgent">Wants out</UiStatusBadge>
          <UiStatusBadge v-if="row?.unavailable_reason === 'injured'" tone="urgent">Injured</UiStatusBadge>
          <UiStatusBadge tone="outline">{{ humanize(detail.squad_role) }}</UiStatusBadge>
        </div>
      </div>
      <span v-if="ovr" class="num text-[28px] font-semibold leading-none">{{ ovr }}</span>
    </UiCard>

    <UiSegmentedControl v-if="tabbed" v-model="tab" label="Player sections" :options="tabs" fill />

    <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto">

    <template v-if="show('overview')">
      <UiDecisionCard v-if="request" tier="urgent" eyebrow="Transfer request"
        :title="choice ? choice.label : `${detail.last_name || detail.player.name} has handed in a transfer request`">
        <template v-if="!choice">Reason: {{ humanize(request.reason) }}. Raised {{ new Date(request.created_at).toLocaleDateString() }}.</template>
        <div v-else class="flex flex-col gap-2">
          <span>{{ choice.sub }}</span>
          <div v-if="choice.action === 'approve' && priceOptions.length" class="flex flex-col gap-1">
            <span class="text-label label-caps text-t3">Asking price · valuation {{ money(preview!.market_value) }}</span>
            <UiSegmentedControl v-model="preset" label="Asking price" :options="priceOptions" numeric class="flex-wrap" />
          </div>
          <UiWhyBreakdown v-if="effects.length" subject="Predicted consequences" :factors="effects" />
          <span v-for="n in chosen?.notes ?? []" :key="n" class="text-t3">{{ n }}</span>
        </div>
        <template #actions>
          <template v-if="!choice">
            <UiButton v-for="o in options" :key="o.action" size="sm" variant="secondary" @click="choice = o">{{ o.label }}</UiButton>
          </template>
          <template v-else>
            <UiButton size="sm" variant="ghost" :disabled="busy" @click="choice = null">Back</UiButton>
            <UiButton size="sm" :variant="choice.action === 'deny' ? 'destructive' : 'primary'" :loading="busy" @click="emit('respond', choice.action, choice.action === 'approve' ? preset : undefined)">{{ choice.cta }}</UiButton>
          </template>
        </template>
      </UiDecisionCard>

      <UiCard class="flex flex-col gap-3">
        <div class="flex items-center justify-between">
          <span class="text-label label-caps text-t3">Emotional state</span>
          <span class="text-meta">{{ emotion ? humanize(emotion.emotional_state) : moodWord }}</span>
        </div>
        <span v-if="emotion" class="text-meta text-t2">{{ humanize(emotion.cause) }}</span>
        <UiStatBar v-for="c in condition" :key="c.label" :label="c.label" :value="c.value" :tone="c.tone" />
        <div v-if="expectation" class="flex items-center justify-between gap-2 border-t border-line pt-3 text-meta">
          <span class="text-t2">{{ expectation.label }} expects {{ expectation.expected }}, has {{ Math.round(expectation.current * 100) }}%</span>
          <UiStatusBadge :tone="expectationTone[expectation.status]">{{ humanize(expectation.status) }}</UiStatusBadge>
        </div>
        <UiWhyBreakdown v-if="moraleWhy.length" :subject="`Morale drifting toward ${50 + (detail.explanation.why?.score ?? 0)}`"
          :value="`now ${morale}`" :factors="moraleWhy" />
      </UiCard>

      <UiCard class="flex flex-col gap-2">
        <span class="text-label label-caps text-t3">Relationships</span>
        <div v-if="faction" class="flex items-center justify-between text-meta">
          <span>{{ faction.label }}<span class="text-t3"> · {{ faction.leader?.id === detail.player.id ? 'leader' : 'member' }}</span></span>
          <span class="num text-t2">cohesion {{ faction.cohesion }}</span>
        </div>
        <template v-if="detail.relationship_history && detail.relationship_history.length !== 0">
          <div v-for="(e, i) in detail.relationship_history.slice(0, 5)" :key="i" class="flex items-center justify-between text-meta">
            <span class="text-t2">You · {{ humanize(e.event_type) }}</span>
            <span :class="['num', e.sentiment_delta < 0 ? 'text-neg' : 'text-pos']">{{ e.sentiment_delta > 0 ? '+' : '' }}{{ e.sentiment_delta }}</span>
          </div>
        </template>
        <span v-if="!faction && !detail.relationship_history.length" class="text-meta text-t3">No history with you yet.</span>
      </UiCard>
    </template>

    <UiCard v-if="show('attributes')" class="flex flex-col gap-3">
      <span class="text-label label-caps text-t3">Attributes · 1–20</span>
      <div class="grid gap-4 sm:grid-cols-2">
        <div v-for="g in attrGroups" :key="g.name" class="flex flex-col gap-1">
          <span class="text-meta text-t3">{{ g.name }}</span>
          <UiAttributeRating v-for="a in g.items" :key="a.label" :label="a.label" :value="a.value" />
        </div>
      </div>
    </UiCard>

    <UiCard v-if="show('personality') && traits.length" class="flex flex-col gap-2">
      <span class="text-label label-caps text-t3">Personality</span>
      <UiTraitMeter v-for="t in traits" :key="t.label" :label="t.label" :value="t.value" />
    </UiCard>

    <UiCard v-if="show('contract')" class="flex flex-col gap-2">
      <span class="text-label label-caps text-t3">Contract</span>
      <div v-for="c in contract" :key="c.k" class="flex items-center justify-between text-meta">
        <span class="text-t3">{{ c.k }}</span><span class="num">{{ c.v }}</span>
      </div>
    </UiCard>
    </div>
  </div>
</template>
