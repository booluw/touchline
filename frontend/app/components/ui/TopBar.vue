<script setup lang="ts">
import { PhTray } from '@phosphor-icons/vue'
import type { DisplayDensity, ShellClub, ShellFixture } from '~/types/ui/design'

/** Persistent context: season/date, next-fixture countdown, density mode, inbox. Collapses to one row on mobile. */
defineProps<{
  club: ShellClub
  /** "Season 2026/27 · Matchday 19" */
  season: string
  /** "Sat 14 Dec 2026" */
  date: string
  fixture?: ShellFixture
  inboxCount?: number
}>()
const density = defineModel<DisplayDensity>('density', { default: 'standard' })
const emit = defineEmits<{ openInbox: [] }>()
const densityOptions = [
  { value: 'simple', label: 'Simple' },
  { value: 'standard', label: 'Standard' },
  { value: 'advanced', label: 'Advanced' },
] satisfies { value: DisplayDensity, label: string }[]
</script>

<template>
  <header class="font-geist border-b border-line bg-s1 text-body text-t1">
    <!-- Desktop -->
    <div class="hidden flex-wrap items-center justify-between gap-x-5 gap-y-3 px-5 py-3 md:flex">
      <div class="flex flex-wrap items-center gap-5">
        <div class="flex flex-col">
          <span class="text-label text-t3">{{ season }}</span>
          <span class="font-semibold tabular-nums">{{ date }}</span>
        </div>
        <template v-if="fixture">
          <div class="h-7 w-px bg-line" aria-hidden="true" />
          <div class="flex flex-col">
            <span class="text-label text-t3">{{ fixture.context }}</span>
            <span class="font-medium">vs {{ fixture.opponent }} <span class="num ml-1.5 text-important">{{ fixture.countdown }}</span></span>
          </div>
        </template>
      </div>
      <div class="flex items-center gap-2.5">
        <UiSegmentedControl v-model="density" label="Display density" :options="densityOptions" />
        <button
          type="button"
          class="flex items-center gap-1.5 rounded-[7px] border border-line bg-s2 px-2.5 py-1.5 text-meta font-medium hover:bg-s3 focus-visible:outline-2 focus-visible:outline-t2"
          @click="emit('openInbox')"
        >
          <PhTray :size="16" aria-hidden="true" />Inbox
          <span v-if="inboxCount" class="num rounded-[9px] bg-urgent px-1.5 py-px text-pill text-white">{{ inboxCount }}</span>
        </button>
      </div>
    </div>
    <!-- Mobile -->
    <div class="flex items-center gap-2.5 px-4 pb-2.5 pt-3.5 md:hidden">
      <UiClubCrest :short="club.short" :color="club.color" size="sm" />
      <div class="flex min-w-0 flex-1 flex-col leading-tight">
        <span class="truncate font-semibold">{{ club.name }}</span>
        <span class="truncate text-label text-t3">
          {{ date }}<template v-if="fixture"> · <span class="text-important">vs {{ fixture.opponentShort ?? fixture.opponent }} {{ fixture.countdown }}</span></template>
        </span>
      </div>
      <button
        type="button" :aria-label="`Inbox, ${inboxCount ?? 0} items`"
        class="num relative grid size-11 place-items-center rounded-card border border-line text-label font-medium"
        @click="emit('openInbox')"
      >
        {{ inboxCount ?? 0 }}
        <span v-if="inboxCount" class="absolute right-2 top-2 size-1.5 rounded-full bg-urgent" aria-hidden="true" />
      </button>
    </div>
  </header>
</template>
