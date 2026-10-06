<script setup lang="ts">
/** Club identity square. The only place club colour appears. */
withDefaults(defineProps<{ short: string, color?: string, size?: 'sm' | 'md' | 'lg' }>(), { size: 'md' })
const sizes = { sm: 'size-7 rounded-[7px] text-label', md: 'size-8 rounded-nested text-meta', lg: 'size-9 rounded-[9px] text-meta' }
</script>

<template>
  <span
    :class="['font-geist grid shrink-0 place-items-center font-bold tracking-[.02em] text-white', color ? '' : 'bg-club', sizes[size]]"
    :style="color ? { background: color } : undefined"
    aria-hidden="true"
  >{{ short }}</span>
</template>
