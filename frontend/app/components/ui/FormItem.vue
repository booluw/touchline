<script lang="ts" setup>
const props = defineProps<{ label: string, prop?: string }>()
const error = inject("f_errors") as Record<string, string>
provide('f_prop', props.prop)
</script>
<template>
  <label class="flex flex-col font-mono uppercase text-sm text-void-500 font-semibold mb-5" :for="label">
    <span>{{ label }}</span>
    <slot />
    <span class="text-xs text-loss-500 mt-1" v-show="prop">{{ error[prop] || " " }}</span>
  </label>
</template>