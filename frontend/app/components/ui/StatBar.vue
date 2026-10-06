<script setup lang="ts">
import type { SemanticTone } from '~/types/ui/design'

/** 0–100 bar (fitness, fatigue, happiness). Tone is the caller's call: what's good depends on the stat. */
const props = withDefaults(defineProps<{ label: string, value: number, tone?: SemanticTone }>(), { tone: 'neutral' })
const fill: Record<SemanticTone, string> = {
  urgent: 'bg-urgent', important: 'bg-important', intresting: 'bg-info', pos: 'bg-pos', neg: 'bg-neg', neutral: 'bg-t2',
}
const clamped = computed(() => Math.min(100, Math.max(0, props.value)))
</script>

<template>
  <div class="font-geist grid grid-cols-[70px_1fr_26px] items-center gap-2 text-meta">
    <span class="text-t2">{{ label }}</span>
    <div class="h-1.5 rounded-[3px] bg-s3" role="meter" :aria-label="label" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="clamped">
      <div :class="['h-1.5 rounded-[3px]', fill[props.tone]]" :style="{ width: `${clamped}%` }" />
    </div>
    <span class="num text-right text-t1">{{ value }}</span>
  </div>
</template>
