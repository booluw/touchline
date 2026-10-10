<script setup lang="ts">
// Squad (IM63): sortable roster table + player panel on desktop; card list on
// mobile, where a tap opens /play/players/[id]. Filters and sort are client-side.
import { useManagerSquad, type RequestAction } from '~/composables/manager/squad'
import type { PricePreset } from '~/types/manager'
import type { LoadingStatus } from '~/types'
import type { ManagerSquadPlayer, PlayerMoraleDetail, SquadDynamics } from '~/types/manager'
import type { SegmentOption, TableColumn, TableSort } from '~/types/ui/design'

const viewport = useViewport()
const route = useRoute()
const router = useRouter()
const clubstore = useClubStore()
const { getSquad, getPlayer, getDynamics, respond } = useManagerSquad()

const clubId = computed(() => clubstore.club?.id)
const status = ref<LoadingStatus>('loading')
const players = ref<ManagerSquadPlayer[]>([])
const dynamics = ref<SquadDynamics | null>(null)
const detail = ref<PlayerMoraleDetail | null>(null)
const busy = ref(false)
const mobile = computed(() => viewport.isLessThan('tablet'))

const mode = ref<'simple' | 'standard'>('standard')
const modes: SegmentOption<'simple' | 'standard'>[] = [{ value: 'simple', label: 'Simple' }, { value: 'standard', label: 'Standard' }]
const q = ref('')
type Group = 'All' | 'GK' | 'DEF' | 'MID' | 'FWD'
const group = ref<Group>('All')
const groups: Group[] = ['All', 'GK', 'DEF', 'MID', 'FWD']
const DEF = ['CB', 'LB', 'RB', 'LWB', 'RWB']
const FWD = ['ST', 'CF', 'LW', 'RW']
const groupOf = (pos = ''): Group => pos === 'GK' ? 'GK' : DEF.includes(pos) ? 'DEF' : FWD.includes(pos) ? 'FWD' : 'MID'

type Row = {
  id: string, name: string, pos: string, age: number, ovr: number, form: number | null, ratings: number[]
  morale: number, exp: string, expMonths: number, wage: number, value: number, flag: string, flagTone: 'urgent' | 'important' | 'neutral'
  src: ManagerSquadPlayer
}
const monthsUntil = (iso?: string) => iso ? (new Date(iso).getTime() - Date.now()) / (30.44 * 864e5) : Infinity
const rows = computed<Row[]>(() => players.value.map((p) => {
  const ratings = p.recent_ratings ?? []
  const expMonths = monthsUntil(p.contract?.end_date)
  const morale = Math.round((p.morale ?? 0) * 100)
  let flag = ''
  let flagTone: Row['flagTone'] = 'neutral'
  if (p.transfer_request_status === 'pending') { flag = 'Wants out'; flagTone = 'urgent' }
  else if (p.unavailable_reason === 'injured') { flag = 'Injured'; flagTone = 'urgent' }
  else if (expMonths <= 6) { flag = 'Expiring'; flagTone = 'important' }
  return {
    id: p.player.id, name: p.player.name, pos: p.position ?? '', age: p.age ?? 0, ovr: p.overall ?? 0,
    form: ratings.length ? ratings.reduce((a, b) => a + b, 0) / ratings.length : null, ratings,
    morale, exp: p.contract ? new Date(p.contract.end_date).toLocaleDateString(undefined, { month: 'short', year: 'numeric' }) : '—',
    expMonths, wage: p.contract?.weekly_wage ?? 0, value: p.dossier?.bio?.market_value ?? 0, flag, flagTone, src: p,
  }
}))

const statusFilters: Record<string, (r: Row) => boolean> = {
  'All': () => true,
  'Wants out': r => r.src.transfer_request_status === 'pending',
  'Unhappy': r => r.morale < 55,
  'Expiring ≤12m': r => r.expMonths <= 12,
  'Injured': r => r.src.unavailable_reason === 'injured',
}
const statusFilter = ref('All')
const statusCount = (k: string) => rows.value.filter(statusFilters[k]!).length

type Key = 'name' | 'pos' | 'age' | 'ovr' | 'form' | 'morale' | 'exp' | 'wage' | 'value'
const sort = ref<TableSort<Key> | undefined>({ key: 'ovr', direction: 'desc' })
const columns: TableColumn<Key>[] = [
  { key: 'name', label: 'Player', sortable: true, width: 'minmax(180px,2.4fr)' },
  { key: 'pos', label: 'Pos', sortable: true, width: '48px' },
  { key: 'age', label: 'Age', align: 'right', sortable: true, width: '44px' },
  { key: 'ovr', label: 'OVR', align: 'right', sortable: true, width: '48px' },
  { key: 'form', label: 'Form', align: 'right', sortable: true, width: 'minmax(80px,.9fr)', advanced: true },
  { key: 'morale', label: 'Morale', sortable: true, width: 'minmax(110px,1.2fr)' },
  { key: 'exp', label: 'Expires', align: 'right', sortable: true, width: '80px', advanced: true },
  { key: 'wage', label: 'Wage/wk', align: 'right', sortable: true, width: '80px', advanced: true },
  { key: 'value', label: 'Value', align: 'right', sortable: true, width: '72px', advanced: true },
]
const sortValue = (r: Row, k: Key) => k === 'exp' ? r.expMonths : k === 'form' ? (r.form ?? -1) : r[k]

const visible = computed(() => {
  const needle = q.value.trim().toLowerCase()
  const out = rows.value.filter(r => (group.value === 'All' || groupOf(r.pos) === group.value)
    && statusFilters[statusFilter.value]!(r) && r.name.toLowerCase().includes(needle))
  const s = sort.value
  if (!s) return out
  const dir = s.direction === 'asc' ? 1 : -1
  return out.sort((a, b) => {
    const x = sortValue(a, s.key), y = sortValue(b, s.key)
    return (typeof x === 'string' ? x.localeCompare(y as string) : x - (y as number)) * dir
  })
})

const summary = computed(() => {
  const n = rows.value.length
  if (!n) return ''
  const avgAge = rows.value.reduce((a, r) => a + r.age, 0) / n
  const wages = rows.value.reduce((a, r) => a + r.wage, 0)
  return `${n} players · avg age ${avgAge.toFixed(1)} · wages ${money(wages)}/wk`
})
const money = (units: number) => formatMoneyCompact(units * 100)
const moraleTone = (m: number) => m < 35 ? 'bg-neg' : m < 55 ? 'bg-important' : m >= 75 ? 'bg-pos' : 'bg-t2'
const ratingTone = (v: number) => v >= 7 ? 'bg-pos' : v < 6 ? 'bg-neg' : 'bg-t3'
const selectedId = computed(() => typeof route.query.player === 'string' ? route.query.player : visible.value[0]?.id)
const selectedRow = computed(() => players.value.find(p => p.player.id === selectedId.value))

function clearFilters() {
  q.value = ''
  group.value = 'All'
  statusFilter.value = 'All'
}

function open(row: Row) {
  if (mobile.value) return navigateTo(`/play/players/${row.id}`)
  router.replace({ query: { ...route.query, player: row.id } })
}

async function loadDetail() {
  if (mobile.value || !clubId.value || !selectedId.value) return
  const id = selectedId.value
  const d = await getPlayer(clubId.value, id)
  if (selectedId.value === id) detail.value = d ?? null
}

async function load(id: string) {
  status.value = 'loading'
  const [sq, dyn] = await Promise.all([getSquad(id), getDynamics(id)])
  if (!sq) { status.value = 'error'; return }
  players.value = sq
  dynamics.value = dyn
  status.value = 'loaded'
}

async function onRespond(action: RequestAction, preset?: PricePreset) {
  if (!clubId.value || !selectedId.value) return
  busy.value = true
  const ok = await respond(clubId.value, selectedId.value, action, preset)
  busy.value = false
  if (ok) await Promise.all([load(clubId.value), loadDetail()])
}

watch(clubId, id => id && load(id), { immediate: true })
watch([selectedId, clubId, mobile], loadDetail, { immediate: true })
</script>

<template>
  <!-- Desktop: the page fits the shell; table and panel scroll on their own. -->
  <main :class="mobile ? 'space-y-5' : 'flex h-full flex-col gap-5'">
    <div class="flex flex-wrap items-end justify-between gap-3 pt-5">
      <div class="flex flex-col gap-1">
        <h2 class="text-page">Squad</h2>
        <span class="num text-meta text-t2">{{ summary }}</span>
      </div>
      <UiSegmentedControl v-if="!mobile" v-model="mode" label="Table density" :options="modes" />
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <UiTextField v-if="!mobile" v-model="q" label="Search players" type="search" placeholder="Search players" class="w-56" />
      <UiFilterChip v-for="g in groups" :key="g" :label="g" :selected="group === g" @update:selected="group = g" />
      <span class="mx-1 h-5 w-px bg-line" aria-hidden="true" />
      <UiFilterChip v-for="(_, k) in statusFilters" :key="k" :label="k" :count="k === 'All' ? undefined : statusCount(k)"
        :selected="statusFilter === k" @update:selected="statusFilter = k" />
    </div>

    <UiEmptyState v-if="status === 'error'" title="Squad unavailable" />
    <div v-else-if="status === 'loading'" class="grid gap-5 md:grid-cols-12">
      <UiCard class="space-y-3 md:col-span-8"><UiLoader v-for="i in 8" :key="i" class="h-6 w-full" /></UiCard>
      <UiCard class="space-y-3 md:col-span-4"><UiLoader class="h-16 w-full" /><UiLoader class="h-40 w-full" /></UiCard>
    </div>

    <!-- Mobile: card list; a tap opens the full-screen player view. -->
    <section v-else-if="mobile" class="flex flex-col gap-2">
      <button v-for="r in visible" :key="r.id" type="button"
        class="flex items-center gap-3 rounded-card border border-line bg-s1 p-3 text-left" @click="open(r)">
        <span class="num w-8 text-[18px] font-semibold">{{ r.ovr }}</span>
        <div class="flex min-w-0 flex-1 flex-col">
          <span class="flex items-center gap-1.5 truncate font-medium">{{ r.name }}
            <UiStatusBadge v-if="r.flag" :tone="r.flagTone">{{ r.flag }}</UiStatusBadge></span>
          <span class="num text-meta text-t3">{{ r.pos }} · {{ r.age }} · {{ money(r.wage) }}/wk · {{ r.exp }}</span>
        </div>
        <div class="flex w-20 flex-col items-end gap-1">
          <span class="text-meta text-t2">{{ moodLabel(r.morale) }}</span>
          <div class="h-1 w-full rounded bg-s3"><div :class="['h-1 rounded', moraleTone(r.morale)]" :style="{ width: `${r.morale}%` }" /></div>
        </div>
      </button>
      <UiEmptyState v-if="!visible.length" title="No players match these filters">
        <UiButton size="sm" variant="secondary" @click="clearFilters">Clear filters</UiButton>
      </UiEmptyState>
    </section>

    <section v-else class="grid min-h-0 flex-1 gap-5 pb-5 md:grid-cols-12">
      <div class="flex min-h-0 flex-col gap-2 md:col-span-7 xl:col-span-8">
        <UiDataTable class="min-h-0 flex-1" v-model:sort="sort" :columns="columns" :rows="visible" :row-key="r => r.id" :density="mode"
          :selected-key="selectedId" caption="Squad" @row-click="open">
          <template #cell-name="{ row }">
            <span class="flex min-w-0 items-center gap-2">
              <span class="num text-meta text-t3">{{ row.src.nationality?.code ?? '' }}</span>
              <span class="truncate font-medium">{{ row.name }}</span>
              <UiStatusBadge v-if="row.flag" :tone="row.flagTone">{{ row.flag }}</UiStatusBadge>
            </span>
          </template>
          <template #cell-pos="{ row }"><span class="text-t2">{{ row.pos }}</span></template>
          <template #cell-ovr="{ row }"><span class="num font-semibold">{{ row.ovr }}</span></template>
          <template #cell-form="{ row }">
            <span v-if="row.form !== null" class="inline-flex items-end gap-1.5">
              <span class="inline-flex h-4 items-end gap-px" aria-hidden="true">
                <span v-for="(v, i) in row.ratings" :key="i" :class="['w-1 rounded-sm', ratingTone(v)]" :style="{ height: `${v * 10}%` }" />
              </span>
              <span class="num">{{ row.form.toFixed(1) }}</span>
            </span>
            <span v-else class="text-t3">—</span>
          </template>
          <template #cell-morale="{ row }">
            <span class="flex items-center gap-2">
              <span class="h-1 w-12 rounded bg-s3"><span :class="['block h-1 rounded', moraleTone(row.morale)]" :style="{ width: `${row.morale}%` }" /></span>
              <span class="text-meta text-t2">{{ moodLabel(row.morale) }}</span>
            </span>
          </template>
          <template #cell-exp="{ row }"><span :class="row.expMonths <= 6 ? 'text-important' : 'text-t2'">{{ row.exp }}</span></template>
          <template #cell-wage="{ row }"><span class="num text-t2">{{ money(row.wage) }}</span></template>
          <template #cell-value="{ row }"><span class="num text-t2">{{ money(row.value) }}</span></template>
          <template #empty>
            <div class="flex items-center justify-between gap-2 p-4 text-meta text-t2">
              No players match these filters
              <UiButton size="sm" variant="secondary" @click="clearFilters">Clear filters</UiButton>
            </div>
          </template>
        </UiDataTable>
        <span class="text-meta text-t3">Click a column to sort · click a player to open details</span>
      </div>

      <aside class="min-h-0 md:col-span-5 xl:col-span-4">
        <SquadPlayerPanel v-if="detail && detail.player.id === selectedId" :detail="detail" :row="selectedRow"
          :dynamics="dynamics" :busy="busy" @respond="onRespond" />
        <UiCard v-else-if="selectedId" class="space-y-3"><UiLoader class="h-16 w-full" /><UiLoader class="h-40 w-full" /></UiCard>
      </aside>
    </section>
  </main>
</template>
