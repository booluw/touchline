<script lang="ts" setup>
import { useClubFixtures } from '~/composables/manager/fixtures';
import type { LoadingStatus } from '~/types';
import type { ManagerFixture } from '~/types/manager';

const { getNUpcomingFixtures } = useClubFixtures()

const status = ref<LoadingStatus>("loading")
const data = ref<ManagerFixture[]>([])

async function load() {
  status.value = "loading"

  try {
    const { fixtures } = await getNUpcomingFixtures(5)
    data.value = fixtures
    status.value = "loaded"
  } catch {
    status.value = "error"
  }
}
</script>

<template>
  <UiCard class="flex flex-col gap-1.5">
    <template v-if="status === 'loading'">
      <div class="flex">

      </div>
    </template>
    <template v-else-if="data"></template>
  </UiCard>
</template>