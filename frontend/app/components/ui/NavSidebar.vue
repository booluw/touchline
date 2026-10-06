<script setup lang="ts">
import type { ShellClub, ShellNavItem } from '~/types/ui/design'

/** Desktop sidebar, 216px: club identity then every section. */
defineProps<{ club: ShellClub, items: ShellNavItem[] }>()
const isActive = useShellActive()
</script>

<template>
  <nav aria-label="Main" class="font-geist flex w-[216px] shrink-0 flex-col gap-0.5 border-r border-line bg-s1 px-2.5 py-3.5 text-body">
    <div class="flex items-center gap-2.5 px-1.5 pb-3.5 pt-1">
      <UiClubCrest :short="club.short" :color="club.color" />
      <div class="flex min-w-0 flex-col">
        <span class="truncate font-semibold text-t1">{{ club.name }}</span>
        <span v-if="club.league" class="truncate text-label text-t3">{{ club.league }}</span>
      </div>
    </div>
    <NuxtLink
      v-for="item in items" :key="item.to" :to="item.to"
      :aria-current="isActive(item) ? 'page' : undefined"
      :class="['flex items-center gap-2 rounded-control px-2 py-1.5 transition-colors duration-150 hover:bg-s2 hover:no-underline focus-visible:outline-2 focus-visible:outline-t2',
               isActive(item) ? 'bg-s3 text-t1' : 'text-t2']"
    >
      <component :is="item.icon" :size="16" aria-hidden="true" />
      <span class="flex-1">{{ item.label }}</span>
      <UiNavCount v-if="item.count" :count="item.count" :tone="item.countTone" />
    </NuxtLink>
  </nav>
</template>
