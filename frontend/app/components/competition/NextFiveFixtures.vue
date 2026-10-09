<script lang="ts" setup>
import { useClubFixtures } from '~/composables/manager/fixtures';
import type { LoadingStatus } from '~/types';
import type { ManagerFixture } from '~/types/manager';

const clubstore = useClubStore()
const { getNUpcomingFixtures } = useClubFixtures()

const status = ref<LoadingStatus>("loading")
const data = ref<ManagerFixture[]>([])

const clubId = computed(() => clubstore.club?.id)

async function load() {
  status.value = "loading"

  try {
    const { fixtures } = await getNUpcomingFixtures(10)
    data.value = fixtures
    status.value = "loaded"
  } catch {
    status.value = "error"
  }
}

onMounted(load)
</script>

<template>
  <UiCard class="flex flex-col gap-1.5">
    <div class="text-label label-caps">
      next fixtures
    </div>
    <template v-if="status === 'loading'">
      <div v-for="key in 10" :key class="[&+&]:border-t [&+&]:border-line grid gap-5 grid-cols-3 py-1.5">
        <UiLoader class="w-10" />
        <UiLoader class="" />
        <div class="flex justify-end">
          <UiLoader class="w-15 h-3" />
        </div>
      </div>
    </template>
    <template v-else-if="status === 'loaded' && data">
      <div v-for="(fixture, key) in data" :key class="[&+&]:border-t [&+&]:border-line grid grid-cols-3 py-1.5 text-sm">
        <div class="">
          {{ formatFixtureDateTimeSmart(fixture.scheduled_at, false) }}
        </div>
        <nuxt-link :to="`/play/clubs/${clubId !== fixture.away_club.id ? fixture.away_club.id : fixture.home_club.id}`" class="text-t2 hover:underline">
          {{ clubId !== fixture.away_club.id ? fixture.away_club.name : fixture.home_club.name }}
        </nuxt-link>
        <div class="text-right text-xs" :class="[
          { 'text-pos': ['Easy', 'Very easy'].includes(fixture.difficulty.label) },
          { 'text-neutral': fixture.difficulty.label === 'Even' },
          { 'text-neg': ['Hard', 'Very hard'].includes(fixture.difficulty.label) }
        ]">
          {{ fixture.difficulty.label }}
        </div>
      </div>
    </template>
  </UiCard>
</template>