<script setup lang="ts">
import type { AttentionTier } from '~/types/ui/design'

/** A decision the manager must make now: border + tint in the tier colour. */
const props = withDefaults(defineProps<{ tier?: AttentionTier, eyebrow?: string, title: string }>(), { tier: 'important' })

const tierClass: Record<AttentionTier, { box: string, text: string }> = {
  urgent: { box: 'border-urgent bg-urgent-bg', text: 'text-urgent' },
  important: { box: 'border-important bg-important-bg', text: 'text-important' },
  info: { box: 'border-info bg-info-bg', text: 'text-info' },
}
</script>

<template>
  <section :class="['font-geist flex flex-col gap-1.5 rounded-card border p-3.5 text-body text-t1', tierClass[props.tier].box]">
    <span v-if="eyebrow" :class="['num text-label uppercase', tierClass[props.tier].text]">{{ eyebrow }}</span>
    <span class="font-semibold">{{ title }}</span>
    <div class="text-meta text-t2"><slot /></div>
    <div v-if="$slots.actions" class="mt-1 flex flex-wrap gap-2"><slot name="actions" /></div>
  </section>
</template>
