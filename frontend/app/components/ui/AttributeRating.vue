<script setup lang="ts">
/** 1–20 attribute. Colour by threshold: ≥17 pos · ≥13 t1 · ≥8 t2 · <8 neg. */
const props = defineProps<{ label: string, value: number }>()
const tone = computed(() =>
  props.value >= 17 ? 'text-pos' : props.value >= 13 ? 'text-t1' : props.value >= 8 ? 'text-t2' : 'text-neg')
</script>

<template>
  <div class="font-geist flex justify-between border-b border-line py-0.5 text-meta">
    <span class="text-t2">{{ label }}</span>
    <span :class="['num font-semibold', tone]">{{ value }}</span>
  </div>
</template>
