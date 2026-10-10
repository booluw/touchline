<script setup lang="ts">
import type { ManagerSquadPlayer } from '~/types/manager'
import type { LineupSlot, PositionFit } from '~/types/ui/design'
import { assignToSlot, bestEleven, effectiveRating, fitLabel, FORMATION_ORDERS, positionFit, slotCoordinates, swapSlots } from '~/utils/lineup'

/**
 * Pick and rearrange the XI (design: Touchline Screens → Tactics).
 * Desktop: click a pitch player, then a squad row to replace him or another
 * pitch player to swap; or click a substitute first, then a pitch slot; or
 * drag. Mobile: tap a pitch player to open a sheet of the best options.
 * `xi` is the player id per formation slot (null = empty).
 */
const props = defineProps<{ formation: string, players: ManagerSquadPlayer[] }>()
const xi = defineModel<(string | null)[]>({ required: true })

const viewport = useViewport()
const mobile = computed(() => viewport.isLessThan('tablet'))

const order = computed(() => FORMATION_ORDERS[props.formation] ?? FORMATION_ORDERS['4-3-3']!)
const coords = computed(() => slotCoordinates(order.value))
const byId = computed(() => new Map(props.players.map(p => [p.player.id, p])))

/** Selected pitch slot, or a substitute waiting for a slot (desktop). */
const selSlot = ref<number | null>(null)
const pendingPlayer = ref<string | null>(null)
const sheetOpen = computed({
  get: () => mobile.value && selSlot.value !== null,
  set: (v) => { if (!v) selSlot.value = null },
})

const surname = (name: string) => name.split(' ').slice(-1)[0] ?? name
const overall = (p: ManagerSquadPlayer) => p.overall ?? 0
const isAvailable = (p: ManagerSquadPlayer) => p.available !== false
const fitClass: Record<PositionFit, string> = { natural: 'text-pos', capable: 'text-t2', awkward: 'text-important', out: 'text-neg' }
const fitText: Record<PositionFit, string> = { natural: 'Natural', capable: 'Capable', awkward: 'Awkward', out: 'Out of position' }

const pitchSlots = computed<LineupSlot[]>(() => order.value.map((role, i) => {
  const p = xi.value[i] ? byId.value.get(xi.value[i]!) : undefined
  const { x, y } = coords.value[i]!
  if (!p) return { id: String(i), x, y, rating: 0, fit: 'out', role, empty: true }
  const fit = positionFit(p.position ?? '', role)
  return {
    id: String(i), x, y, role,
    rating: effectiveRating(overall(p), fit),
    fit: fitLabel(fit),
    name: surname(p.player.name),
    label: `${p.player.name} at ${role}`,
  }
}))

interface Row {
  id: string
  name: string
  pos: string
  fit?: PositionFit
  rating: number
  fitness: string
  fitnessClass: string
  tag?: string
  tagClass?: string
  disabled: boolean
}

function row(p: ManagerSquadPlayer, slotRole?: string): Row {
  const fit = slotRole ? positionFit(p.position ?? '', slotRole) : 1
  const f = p.fitness ?? 1
  const inXI = xi.value.includes(p.player.id)
  const tag = p.unavailable_reason === 'injured' ? 'INJ' : p.unavailable_reason ? 'N/A' : slotRole && inXI ? 'IN XI' : undefined
  return {
    id: p.player.id,
    name: p.player.name,
    pos: p.position ?? '',
    fit: slotRole ? fitLabel(fit) : undefined,
    rating: effectiveRating(overall(p), fit),
    fitness: isAvailable(p) ? `${Math.round(f * 100)}%` : '—',
    fitnessClass: f < 0.75 ? 'text-urgent' : f < 0.85 ? 'text-important' : 'text-t2',
    tag,
    tagClass: tag === 'IN XI' ? 'bg-s3 text-t2' : 'bg-urgent-bg text-urgent',
    disabled: !isAvailable(p),
  }
}

/** Best options first for the selected slot; else the bench. */
const rows = computed<Row[]>(() => {
  if (selSlot.value !== null) {
    const role = order.value[selSlot.value]!
    return props.players
      .filter(p => p.player.id !== xi.value[selSlot.value!])
      .map(p => row(p, role))
      .sort((a, b) => Number(a.disabled) - Number(b.disabled) || b.rating - a.rating)
  }
  return props.players
    .filter(p => !xi.value.includes(p.player.id))
    .map(p => row(p))
    .sort((a, b) => Number(a.disabled) - Number(b.disabled) || b.rating - a.rating)
})

const selectedPlayer = computed(() => selSlot.value === null ? undefined : byId.value.get(xi.value[selSlot.value] ?? ''))
const panelTitle = computed(() => {
  if (selSlot.value === null) return 'Substitutes'
  return selectedPlayer.value ? `Replace ${selectedPlayer.value.player.name}` : `Fill ${order.value[selSlot.value]}`
})
const panelSub = computed(() => {
  if (selSlot.value !== null) return `${order.value[selSlot.value]} · best options first · or click another pitch player to swap`
  if (pendingPlayer.value) return `Now click a pitch slot for ${byId.value.get(pendingPlayer.value)?.player.name ?? 'him'}`
  return `${rows.value.length} players · click one then a pitch slot, or drag onto the pitch`
})

function clear() {
  selSlot.value = null
  pendingPlayer.value = null
}

function onSlot(slot: LineupSlot) {
  const i = Number(slot.id)
  if (pendingPlayer.value) {
    xi.value = assignToSlot(xi.value, i, pendingPlayer.value)
    return clear()
  }
  if (selSlot.value === null || selSlot.value === i || mobile.value) {
    selSlot.value = selSlot.value === i ? null : i
    return
  }
  xi.value = swapSlots(xi.value, selSlot.value, i)
  clear()
}

function onRow(r: Row) {
  if (r.disabled) return
  if (selSlot.value !== null) {
    xi.value = assignToSlot(xi.value, selSlot.value, r.id)
    return clear()
  }
  pendingPlayer.value = pendingPlayer.value === r.id ? null : r.id
}

function onRowDrag(e: DragEvent, r: Row) {
  if (r.disabled) return e.preventDefault()
  e.dataTransfer?.setData('text/player-id', r.id)
}

function pickBest() {
  xi.value = bestEleven(order.value, props.players.map(p => ({ id: p.player.id, position: p.position ?? '', overall: overall(p), available: isAvailable(p) })))
  clear()
}

const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') clear() }
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
watch(() => props.formation, clear)
</script>

<template>
  <!-- Mobile: compact pitch + bottom sheet -->
  <div v-if="mobile" class="flex flex-col gap-2">
    <div class="flex items-center justify-between">
      <span class="text-label label-caps text-t3">Starting XI</span>
      <UiButton size="sm" @click="pickBest">Pick best XI</UiButton>
    </div>
    <slot />
    <UiPitch :slots="pitchSlots" :selected-id="selSlot === null ? undefined : String(selSlot)" compact @select="onSlot" />
    <span class="text-center text-meta text-t3">Tap a player to swap or substitute. Number = rating in that slot.</span>
    <UiBottomSheet v-model:open="sheetOpen" eyebrow="Tap a player on the pitch · then pick" :title="panelTitle">
      <ul class="flex flex-col">
        <li v-for="r in rows" :key="r.id">
          <button
            type="button" :disabled="r.disabled"
            class="flex min-h-13 w-full items-center gap-2.5 border-t border-line px-1 py-1.5 text-left disabled:opacity-45"
            @click="onRow(r)"
          >
            <span :class="['num w-7 text-[15px] font-semibold', r.fit ? fitClass[r.fit] : 'text-t1']">{{ r.disabled ? '—' : r.rating }}</span>
            <span class="flex min-w-0 flex-1 flex-col">
              <span class="truncate font-medium">{{ r.name }}</span>
              <span class="text-meta text-t3">{{ r.pos }}<template v-if="r.fit"> · <span :class="fitClass[r.fit]">{{ fitText[r.fit] }}</span></template> · fitness {{ r.fitness }}</span>
            </span>
            <span v-if="r.tag" :class="['num rounded-[3px] px-1.5 py-px text-[10px] font-medium', r.tagClass]">{{ r.tag }}</span>
          </button>
        </li>
      </ul>
    </UiBottomSheet>
  </div>

  <!-- Desktop: pitch + candidate panel -->
  <div v-else class="flex flex-wrap items-start gap-3">
    <UiCard class="flex min-w-0 flex-[1_1_340px] flex-col gap-2.5">
      <slot />
      <UiPitch :slots="pitchSlots" :selected-id="selSlot === null ? undefined : String(selSlot)" draggable show-legend
        @select="onSlot"
        @swap="(a, b) => { xi = swapSlots(xi, Number(a), Number(b)); clear() }"
        @drop-player="(id, s) => { xi = assignToSlot(xi, Number(s), id); clear() }" />
      <span class="text-meta text-t3">Click a player to replace him, or click two players on the pitch to swap them. You can also drag. Number = effective rating in that slot.</span>
    </UiCard>

    <UiCard flush :class="['flex min-w-0 flex-[1_1_260px] flex-col overflow-hidden', (selSlot !== null || pendingPlayer) && 'border-t3']">
      <header class="flex items-start justify-between gap-2 border-b border-line px-3.5 py-3">
        <div class="w-3/5 flex min-w-0 flex-col">
          <span class="truncate font-semibold">{{ panelTitle }}</span>
          <span class="text-meta text-t3">{{ panelSub }}</span>
        </div>
        <UiButton v-if="selSlot !== null || pendingPlayer" size="sm" variant="ghost" @click="clear">Cancel</UiButton>
        <UiButton v-else size="md" @click="pickBest">Pick best XI</UiButton>
      </header>
      <ul class="max-h-130 overflow-y-auto">
        <li v-for="r in rows" :key="r.id">
          <button
            type="button" :disabled="r.disabled" :draggable="!r.disabled"
            :aria-pressed="pendingPlayer === r.id"
            :class="['grid w-full grid-cols-[minmax(0,1fr)_60px_38px_30px] items-center gap-2 border-t border-line px-3.5 py-[7px] text-left hover:bg-s2 disabled:cursor-not-allowed disabled:opacity-45', pendingPlayer === r.id && 'bg-s3']"
            @click="onRow(r)" @dragstart="onRowDrag($event, r)"
          >
            <span class="flex min-w-0 flex-col">
              <span class="flex min-w-0 items-center gap-1.5">
                <span class="truncate font-medium">{{ r.name }}</span>
                <span v-if="r.tag" :class="['num rounded-[3px] px-1 text-[9.5px] font-medium', r.tagClass]">{{ r.tag }}</span>
              </span>
              <span class="text-meta text-t3">{{ r.pos }}</span>
            </span>
            <span :class="['truncate text-meta', r.fit ? fitClass[r.fit] : '']">{{ r.fit ? fitText[r.fit] : '' }}</span>
            <span :class="['num text-right text-meta', r.fitnessClass]">{{ r.fitness }}</span>
            <span :class="['num text-right font-semibold', r.fit ? fitClass[r.fit] : 'text-t1']">{{ r.disabled ? '—' : r.rating }}</span>
          </button>
        </li>
      </ul>
    </UiCard>
  </div>
</template>
