<script setup lang="ts">
import { Toggle } from '@ark-ui/vue/toggle'

defineProps<{ label: string, count?: number, disabled?: boolean }>()
const selected = defineModel<boolean>('selected', { default: false })
</script>

<template>
  <Toggle.Root
    v-model:pressed="selected"
    :disabled="disabled"
    class="font-geist inline-flex items-center gap-1.5 rounded-full border border-line px-2.5 py-[5px] text-meta font-medium text-t2 transition-colors duration-150 hover:bg-s2 data-[state=on]:border-line2 data-[state=on]:bg-s3 data-[state=on]:text-t1 focus-visible:outline-2 focus-visible:outline-t2 disabled:opacity-40"
  >
    {{ label }}
    <span v-if="count !== undefined" class="num text-pill text-t3">{{ count }}</span>
  </Toggle.Root>
</template>
