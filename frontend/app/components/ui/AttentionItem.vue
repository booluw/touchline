<script setup lang="ts">
import type { AttentionTier } from '~/types/ui/design'

/** Home-inbox row: tier pill → fact → consequence → one action. Stack inside <UiCard flush>. */
defineProps<{
  tier: AttentionTier
  /** Pill text, e.g. "Transfer request". */
  tag: string
  title: string
  consequence?: string
  actionLabel?: string
  /** Show the "Why?" button; only when the state is a sum of factors. */
  explainable?: boolean
}>()
const emit = defineEmits<{ action: [], why: [] }>()
</script>

<template>
  <div class="font-geist flex items-start gap-3 px-3.5 py-3 text-body [&+&]:border-t [&+&]:border-line">
    <UiStatusBadge :tone="tier">{{ tag }}</UiStatusBadge>
    <div class="flex min-w-0 flex-1 flex-col gap-0.5">
      <span class="font-medium text-t1">{{ title }}</span>
      <span v-if="consequence" class="text-meta text-t2">{{ consequence }}</span>
      <slot />
    </div>
    <UiButton v-if="explainable" variant="secondary" @click="emit('why')">Why?</UiButton>
    <UiButton v-if="actionLabel" variant="primary" @click="emit('action')">{{ actionLabel }}</UiButton>
  </div>
</template>
