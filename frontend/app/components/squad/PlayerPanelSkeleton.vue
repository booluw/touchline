<script setup lang="ts">
// Loading state shaped like PlayerPanel: header card, (tabs), state, relationships, attributes.
defineProps<{ tabbed?: boolean }>()
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-4" aria-busy="true" aria-label="Loading player">
    <UiCard class="flex items-start gap-3">
      <div class="flex flex-1 flex-col gap-2">
        <UiLoader class="h-5 w-40" />
        <UiLoader class="h-3 w-32" />
        <div class="flex gap-1.5"><UiLoader class="h-4 w-16" /><UiLoader class="h-4 w-20" /></div>
      </div>
      <UiLoader class="h-7 w-9" />
    </UiCard>

    <UiLoader v-if="tabbed" class="h-8 w-full" />

    <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden">
      <UiCard class="flex flex-col gap-3">
        <div class="flex justify-between"><UiLoader class="h-3 w-28" /><UiLoader class="h-3 w-14" /></div>
        <div v-for="i in 4" :key="i" class="flex items-center gap-2">
          <UiLoader class="h-3 w-16" /><UiLoader class="h-1.5 flex-1" /><UiLoader class="h-3 w-6" />
        </div>
        <div class="flex justify-between border-t border-line pt-3"><UiLoader class="h-3 w-44" /><UiLoader class="h-4 w-16" /></div>
      </UiCard>

      <UiCard class="flex flex-col gap-2">
        <UiLoader class="h-3 w-24" />
        <div v-for="i in 3" :key="i" class="flex justify-between"><UiLoader class="h-3 w-36" /><UiLoader class="h-3 w-8" /></div>
      </UiCard>

      <UiCard class="flex flex-col gap-3">
        <UiLoader class="h-3 w-28" />
        <div class="grid gap-4 sm:grid-cols-2">
          <div v-for="g in 2" :key="g" class="flex flex-col gap-1.5">
            <UiLoader class="h-3 w-16" />
            <div v-for="i in 5" :key="i" class="flex justify-between"><UiLoader class="h-3 w-20" /><UiLoader class="h-3 w-5" /></div>
          </div>
        </div>
      </UiCard>
    </div>
  </div>
</template>
