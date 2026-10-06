<script setup lang="ts">
defineProps<{ title: string, description?: string }>()
</script>

<template>
  <UiCard dashed class="flex flex-col items-center gap-1.5 px-3.5 py-[18px] text-center">
    <span class="font-semibold text-t1">{{ title }}</span>
    <span v-if="description" class="text-meta text-t2">{{ description }}</span>
    <div v-if="$slots.default" class="mt-1"><slot /></div>
  </UiCard>
</template>
