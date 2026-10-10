<script setup lang="ts">
import type { LineupSlot, PositionFit } from '~/types/ui/design'

/**
 * Tactics pitch. Click/tap a token to select; the parent decides replace/swap
 * behaviour. With `draggable` (pointer devices), tokens can be dragged onto
 * each other (`swap`) and squad rows can be dropped on a token (`drop-player`,
 * the row sets dataTransfer "text/player-id").
 */
defineProps<{ slots: LineupSlot[], selectedId?: string, showLegend?: boolean, draggable?: boolean, compact?: boolean }>()
const emit = defineEmits<{ select: [slot: LineupSlot], swap: [from: string, to: string], dropPlayer: [playerId: string, slotId: string] }>()
const legend: { fit: PositionFit, label: string, ring: string }[] = [
  { fit: 'natural', label: 'Natural', ring: 'border-pos' },
  { fit: 'capable', label: 'Capable', ring: 'border-t2' },
  { fit: 'awkward', label: 'Awkward', ring: 'border-important' },
  { fit: 'out', label: 'Out of position', ring: 'border-neg' },
]
const over = ref<string>()

function onDragStart(e: DragEvent, slot: LineupSlot) {
  e.dataTransfer?.setData('text/slot-id', slot.id)
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
function onDrop(e: DragEvent, slot: LineupSlot) {
  over.value = undefined
  const from = e.dataTransfer?.getData('text/slot-id')
  const player = e.dataTransfer?.getData('text/player-id')
  if (from && from !== slot.id) emit('swap', from, slot.id)
  else if (player) emit('dropPlayer', player, slot.id)
}
</script>

<template>
  <div class="font-geist flex flex-col gap-2.5">
    <div :class="['relative w-full overflow-hidden rounded-nested border border-line2 bg-s2', compact ? 'aspect-[68/80]' : 'aspect-[68/90]']">
      <div class="absolute inset-x-0 top-1/2 border-t border-line2" aria-hidden="true" />
      <div class="absolute left-1/2 top-1/2 aspect-square w-[24%] -translate-1/2 rounded-full border border-line2" aria-hidden="true" />
      <div class="absolute inset-x-1/4 top-0 h-[14%] border border-t-0 border-line2" aria-hidden="true" />
      <div class="absolute inset-x-1/4 bottom-0 h-[14%] border border-b-0 border-line2" aria-hidden="true" />
      <div
        v-for="slot in slots" :key="slot.id"
        class="absolute z-[1] flex min-h-11 min-w-11 -translate-1/2 flex-col items-center gap-0.5"
        :style="{ left: `${slot.x}%`, top: `${slot.y}%` }"
        :draggable="draggable && !slot.empty"
        @dragstart="onDragStart($event, slot)"
        @dragover.prevent="draggable && (over = slot.id)"
        @dragleave="over === slot.id && (over = undefined)"
        @drop.prevent="draggable && onDrop($event, slot)"
      >
        <button
          v-if="slot.empty" type="button"
          :aria-pressed="slot.id === selectedId"
          :aria-label="`Empty ${slot.role ?? 'slot'}, pick a player`"
          :class="['grid size-8 place-items-center rounded-full border-2 border-dashed border-line2 text-t3', slot.id === selectedId && 'outline-2 outline-offset-[3px] outline-t1']"
          @click="emit('select', slot)"
        >+</button>
        <UiLineupToken
          v-else
          :rating="slot.rating" :fit="slot.fit" :label="slot.label" :selected="slot.id === selectedId || slot.id === over"
          @click="emit('select', slot)"
        />
        <span v-if="slot.name" :class="['max-w-24 truncate rounded-[3px] px-1 text-pill font-medium', slot.id === selectedId ? 'bg-s3 text-t1' : 'bg-s1 text-t1', compact && 'text-[10px]']">{{ slot.name }}</span>
        <span v-if="slot.role && !compact" :class="['num text-[9.5px]', slot.id === selectedId ? 'text-t1' : 'text-t3']">{{ slot.role }}</span>
      </div>
    </div>
    <div v-if="showLegend" class="flex flex-wrap items-center gap-3 text-meta">
      <span v-for="item in legend" :key="item.fit" class="flex items-center gap-1.5">
        <span :class="['size-3 rounded-full border-2', item.ring]" aria-hidden="true" />{{ item.label }}
      </span>
    </div>
  </div>
</template>
