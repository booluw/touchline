<script setup lang="ts">
import type { DisplayDensity, ShellClub, ShellFixture, ShellNavItem } from '~/types/ui/design'

/**
 * Game app shell. Desktop: 216px sidebar + top bar + content.
 * Mobile (<768px): compact top bar, content, 64px bottom nav with a More sheet.
 */
defineProps<{
  club: ShellClub
  items: ShellNavItem[]
  season: string
  date: string
  fixture?: ShellFixture
  inboxCount?: number
  loading?: boolean
}>()
const density = defineModel<DisplayDensity>('density', { default: 'standard' })
const emit = defineEmits<{ openInbox: [] }>()
</script>

<template>
  <div class="font-geist flex h-dvh bg-bg text-body text-t1">
    <UiNavSidebar :club="club" :items="items" class="hidden md:flex" />
    <div class="flex min-w-0 flex-1 flex-col">
      <UiTopBar
        v-model:density="density"
        :club :season :date :fixture :inbox-count="inboxCount"
        :loading
        @open-inbox="emit('openInbox')"
      />
      <main class="min-h-0 flex-1 overflow-y-auto p-3.5 pt-0 md:p-5 md:pt-0"><slot /></main>
      <UiBottomNav :items="items" />
    </div>
  </div>
</template>
