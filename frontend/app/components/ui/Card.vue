<script setup lang="ts">
/** Base surface: --s1, 1px --line, radius 10, padding 14. `flush` drops padding for lists/tables. */
withDefaults(defineProps<{ as?: string, flush?: boolean, dashed?: boolean }>(), { as: 'section' })
</script>

<template>
  <component
    :is="as"
    :class="cn('font-geist rounded-card border border-line bg-s1 text-body text-t1', !flush && 'p-3.5', dashed && 'border-dashed border-line2 bg-transparent')"
  >
    <slot />
  </component>
</template>
