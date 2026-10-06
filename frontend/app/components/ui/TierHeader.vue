<script setup lang="ts">
import type { AttentionTier } from '~/types/ui/design'

const props = defineProps<{ tier: AttentionTier, count?: number }>()
const meta: Record<AttentionTier, { label: string, dot: string }> = {
  urgent: { label: 'Urgent', dot: 'bg-urgent' },
  important: { label: 'Important', dot: 'bg-important' },
  interesting: { label: 'Interesting', dot: 'bg-info' },
}
</script>

<template>
  <div class="font-geist flex items-center gap-2">
    <span :class="['size-2 rounded-[2px]', meta[props.tier].dot]" aria-hidden="true" />
    <h3 class="text-meta font-semibold uppercase tracking-wide-brutal text-t1">{{ meta[props.tier].label }}</h3>
    <span v-if="count !== undefined" class="num text-label text-t3">{{ count }}</span>
    <span class="h-px flex-1 bg-line" aria-hidden="true" />
  </div>
</template>
