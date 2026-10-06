<script setup lang="ts">
import type { SemanticTone } from '~/types/ui/design'

const props = defineProps<{
  label: string
  value: string | number
  /** Signed change; colour follows sign unless `deltaTone` is given. */
  delta?: number
  /** Formatted delta text when the raw number isn't the right display (e.g. −£210k). */
  deltaText?: string
  deltaTone?: SemanticTone
  /** 0–100 fill for the progress bar; omit to hide it. */
  progress?: number
  progressTone?: SemanticTone
  caption?: string
}>()

const toneText: Record<SemanticTone, string> = {
  urgent: 'text-urgent', important: 'text-important', info: 'text-info', pos: 'text-pos', neg: 'text-neg', neutral: 'text-t3',
}
const toneBg: Record<SemanticTone, string> = {
  urgent: 'bg-urgent', important: 'bg-important', info: 'bg-info', pos: 'bg-pos', neg: 'bg-neg', neutral: 'bg-t2',
}
const resolvedDeltaTone = computed<SemanticTone>(() =>
  props.deltaTone ?? (props.delta === undefined || props.delta === 0 ? 'neutral' : props.delta > 0 ? 'pos' : 'neg'))
</script>

<template>
  <UiCard class="flex flex-col gap-1.5">
    <span class="label-caps">{{ label }}</span>
    <div class="flex items-baseline gap-1.5">
      <span class="text-[24px] font-semibold">{{ value }}</span>
      <span v-if="delta !== undefined || deltaText" :class="['num text-meta', toneText[resolvedDeltaTone]]">
        {{ deltaText ?? formatSigned(delta!) }}
      </span>
    </div>
    <div
      v-if="progress !== undefined"
      class="h-1 rounded-sm bg-s3"
      role="meter" :aria-label="label" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="progress"
    >
      <div :class="['h-1 rounded-sm', toneBg[progressTone ?? 'neutral']]" :style="{ width: `${Math.min(100, Math.max(0, progress))}%` }" />
    </div>
    <span v-if="caption" class="text-label text-t2">{{ caption }}</span>
    <slot />
  </UiCard>
</template>
