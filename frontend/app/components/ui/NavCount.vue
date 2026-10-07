<script setup lang="ts">
import type { ShellNavItem } from '~/types/ui/design'

defineProps<{ count: number, tone?: ShellNavItem['countTone'] }>()
const toneClass = { urgent: 'text-urgent', important: 'text-important', interesting: 'text-info', neutral: 'text-t3' } as const
</script>

<template>
  <span :class="['num text-pill font-medium', toneClass[tone ?? 'neutral']]">{{ count }}<span class="sr-only"> pending</span></span>
</template>
