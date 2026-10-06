<script setup lang="ts">
import type { LineupSlot, PositionFit } from '~/types/ui/design'

/** Tactics pitch. Click a token to select; the parent decides replace/swap behaviour. */
defineProps<{ slots: LineupSlot[], selectedId?: string, showLegend?: boolean }>()
const emit = defineEmits<{ select: [slot: LineupSlot] }>()
const legend: { fit: PositionFit, label: string, ring: string }[] = [
  { fit: 'natural', label: 'Natural', ring: 'border-pos' },
  { fit: 'capable', label: 'Capable', ring: 'border-t2' },
  { fit: 'awkward', label: 'Awkward', ring: 'border-important' },
  { fit: 'out', label: 'Out of position', ring: 'border-neg' },
]
</script>

<template>
  <div class="font-geist flex flex-col gap-2.5">
    <div class="relative aspect-[68/90] w-full overflow-hidden rounded-nested border border-line2 bg-s2">
      <div class="absolute inset-x-0 top-1/2 border-t border-line2" aria-hidden="true" />
      <div class="absolute left-1/2 top-1/2 aspect-square w-[24%] -translate-1/2 rounded-full border border-line2" aria-hidden="true" />
      <UiLineupToken
        v-for="slot in slots" :key="slot.id"
        class="absolute -translate-1/2"
        :style="{ left: `${slot.x}%`, top: `${slot.y}%` }"
        :rating="slot.rating" :fit="slot.fit" :label="slot.label" :selected="slot.id === selectedId"
        @click="emit('select', slot)"
      />
    </div>
    <div v-if="showLegend" class="flex flex-wrap items-center gap-3 text-meta">
      <span v-for="item in legend" :key="item.fit" class="flex items-center gap-1.5">
        <span :class="['size-3 rounded-full border-2', item.ring]" aria-hidden="true" />{{ item.label }}
      </span>
    </div>
  </div>
</template>
