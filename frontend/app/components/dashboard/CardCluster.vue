<script lang="ts" setup>
import type { ManagerDashboardSummary } from '~/types/manager';

defineProps<{ summary?: ManagerDashboardSummary[] }>()

const pct = (x: number) => Math.round(x * 100)
</script>

<template>
  <div class="grid gap-5 grid-cols-3 md:grid-cols-2">
    <template v-if="summary && summary.length !== 0">
      <UiStatCard label="board confidence" :value="summary[0]!.board.confidence" :delta="summary[0]!.board.change"
        :progress="summary[0]!.board.confidence" progress-tone="important" />

      <UiStatCard label="squad morale" :value="pct(summary[0]!.morale.average)" :delta="summary[0]!.board.change"
        :progress="pct(summary[0]!.morale.average)" progress-tone="pos"
        :caption="`${summary[0]?.morale.unhappy} unhappy`" />

      <UiStatCard class="md:hidden" label="league" :value="ordinal(summary[0]!.league.position)"
        :caption="`${summary[0]?.league.points} pts · ${summary[0]?.league.played} played`" />
    </template>
    <template v-else>
      <template v-for="i in 3" :key="i">
        <UiCard class="flex flex-col gap-2" :class="{ 'flex md:hidden' : i === 2 }">
          <UiLoader class="w-20 h-3" />
          <div class="flex gap-2">
            <UiLoader class="w-5 h-10" />
            <UiLoader class="w-15 h-10" />
          </div>
          <UiLoader class="w-3/4 h-2" />
        </UiCard>
      </template>
    </template>
  </div>
</template>