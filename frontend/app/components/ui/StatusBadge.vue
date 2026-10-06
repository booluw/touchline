<script setup lang="ts">
import type { SemanticTone } from '~/types/ui/design'

/** Mono status pill: semantic text on its tinted background. `outline` for neutral tags such as U21. */
const props = withDefaults(defineProps<{
  tone?: SemanticTone | 'muted' | 'outline'
  /** Rounder, tighter variant for numeric counts. */
  count?: boolean
}>(), { tone: 'muted' })

const toneClass: Record<NonNullable<typeof props.tone>, string> = {
  urgent: 'text-urgent bg-urgent-bg',
  important: 'text-important bg-important-bg',
  info: 'text-info bg-info-bg',
  pos: 'text-pos bg-pos-bg',
  neg: 'text-neg bg-urgent-bg',
  neutral: 'text-t2 bg-s3',
  muted: 'text-t2 bg-s3',
  outline: 'text-t2 border border-line2',
}
</script>

<template>
  <span
    :class="cn(
      'num inline-flex items-center whitespace-nowrap text-pill font-medium',
      props.count ? 'rounded-nested px-[5px]' : 'rounded-pill px-1.5 py-0.5',
      toneClass[props.tone],
    )"
  >
    <slot />
  </span>
</template>
