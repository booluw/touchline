<script setup lang="ts">
import { useClubFixtures } from '~/composables/manager/fixtures'

// Opt in per page with definePageMeta({ layout: 'game' }).
// ponytail: season/date/fixture/inbox are placeholders until a dashboard endpoint feeds the shell.
const clubstore = useClubStore()
const store = useAppStore()

const { getNextFixture } = useClubFixtures()

const loading = ref(false)
const density = computed({
  get() { return store.density },
  set (val) { store.setDensity(val) }
})
const club = computed(() => {
  const { name, short } = clubstore.club!

  return {
    name,
    short,
    league: clubstore.competitions[0]?.league?.competition.name
  }
})

const fixture = computed(() => {
  if (!clubstore.next_fixture) return

  return {
    context: "Next · " + clubstore.next_fixture.home_or_away,
    opponent: clubstore.next_fixture!.opponent.club.name,
    opponentShort: clubstore.next_fixture!.opponent.club.short,
    countdown: formatFixtureDateTimeSmart(clubstore.next_fixture.fixture.scheduled_at)
  }
})

onMounted(async () => {
  if (!fixture.value) {
    loading.value = true
    await getNextFixture()
    loading.value = false
  }
})
</script>

<template>
  <UiAppShell
    v-model:density="density"
    :club
    :fixture
    :loading
    :items="GAME_NAV"
    :season="`Season: 2026 · Matchday ${clubstore.next_fixture?.fixture.matchday ?? ''}`"
    :date="formatFixtureDateTime(Date())"
  >
    <slot />
  </UiAppShell>
</template>
