<script setup lang="ts">
/** Personality trait, 0–20 as 20 cells. Neutral grey on purpose: traits are not good or bad. */
const props = withDefaults(defineProps<{ label: string, value: number, cells?: number }>(), { cells: 20 })
const filled = computed(() => Math.min(props.cells, Math.max(0, Math.round(props.value))))
const ink = computed(() => (filled.value > props.cells / 2 ? 'bg-t1' : 'bg-t2'))
</script>

<template>
  <div class="font-geist grid grid-cols-[64px_1fr_20px] items-center gap-2 text-meta">
    <span class="text-t2">{{ label }}</span>
    <div
      class="grid h-2 gap-px" :style="{ gridTemplateColumns: `repeat(${cells}, 1fr)` }"
      role="meter" :aria-label="label" aria-valuemin="0" :aria-valuemax="cells" :aria-valuenow="filled"
    >
      <span v-for="i in cells" :key="i" :class="i <= filled ? ink : 'bg-s3'" />
    </div>
    <span class="num text-right text-t1">{{ value }}</span>
  </div>
</template>
