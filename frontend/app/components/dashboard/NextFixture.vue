<script lang="ts" setup>
import { Format } from '@ark-ui/vue';
import type { ManagerFixtureDossier } from '~/types/manager'

const clubstore = useClubStore()

const side = (d?: ManagerFixtureDossier) => ({
  id: d?.club.id ?? '',
  short: d?.club.short,
  name: d?.club.name,
  league_position: d?.league_position,
  form_string: d?.form.form_string.replaceAll("-", ""),
})

const next_fixture = computed(() => ({
  club: side(clubstore.next_fixture?.club),
  opponent: side(clubstore.next_fixture?.opponent),
}))
</script>

<template>
  <UiCard class="flex flex-col gap-5">
    <template v-if="next_fixture && clubstore.next_fixture">
      <div class="flex items-center justify-between">
        <span class="text-label label-caps">next match · league</span>
        <span class="text-important font-semibold">{{
          formatFixtureDateTimeSmart(clubstore.next_fixture!.fixture.scheduled_at, false) }}</span>
      </div>
      <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-2 text-center">
        <div v-for="(side, i) in [next_fixture.club, next_fixture.opponent]" :key="side.id"
          :class="['flex flex-col items-center gap-1.5', i ? 'order-3' : '']">
          <UiClubCrest :short="side.short!" size="lg" :color="i ? 'var(--color-opp)' : undefined" />
          <span class="font-medium">{{ side.name }}</span>
          <span class="num text-label text-t3">{{ ordinal(side.league_position!) }} · {{ side.form_string }}</span>
        </div>
        <span class="num order-2 text-meta text-t3" v-if="clubstore.next_fixture!.fixture.status === 'scheduled'">
          <Format.Time :value="new Date(clubstore.next_fixture!.fixture.scheduled_at)" />
        </span>
        <nuxt-link v-else :to="`/play/matches/${clubstore.next_fixture!.fixture.id}`"
          class="animate-pulse text-important">
          Live
        </nuxt-link>
      </div>
      <div class="flex gap-3">
        <UiButton
          variant="primary"
          size="md"
          class="w-full"
          @click="$router.push('/play/squad')"
        >
          Set lineup
        </UiButton>
        <UiButton variant="secondary" size="md" class="w-full">Opponent report</UiButton>
      </div>
    </template>
    <template v-else>
      <div class="flex items-center justify-between">
        <UiLoader class="w-8 h-3" />
        <UiLoader class="w-8 h-3" />
      </div>
      <div class="grid grid-cols-[1fr_auto_1fr] items-center gap-2 text-center">
        <div v-for="i in 2" :key="i" :class="['flex flex-col items-center gap-1.5', i == 2 ? 'order-3' : '']">
          <UiLoader class="h-14 w-14 rounded-2xl" />
          <UiLoader class="w-20 h-3" />
          <UiLoader class="w-12 h-2" />
        </div>
        <div class="order-2!">
        <UiLoader class="w-10 h-3" />
        </div>
      </div>
      <div class="flex gap-3">
        <UiLoader class="w-full h-8" />
        <UiLoader class="w-full h-8" />
      </div>
    </template>
  </UiCard>
</template>