<script setup lang="ts">
const viewport = useViewport()
const clubstore = useClubStore()

const mode = ref<'fixtures'|'table'>("table")
const league = computed(() => clubstore.competitions.find((c) => c.competition_type === "league")?.league?.competition ?? undefined)
</script>

<template>
  <main class="space-y-5 pt-3.5">
    <div class="flex flex-col gap-1">
      <h2 class="text-page">Competitions</h2>
      <p class="text-t2 capitalize">
        {{ league?.name }} · {{ league?.team_count }} Clubs · {{ league?.promotions ? `top ${league.promotions} promoted, ` : '' }} {{ league?.relegations ? `bottom ${league.relegations} relegated` : '' }}
      </p>
    </div>
    <section v-if="viewport.isLessThan('tablet')" class="space-y-5">
      <UiSegmentedControl v-model="mode" label="Tabs" class="w-full self-start"
        :options="[{ value: 'table', label: 'Table' }, { value: 'fixtures', label: 'Fixtures' }]" />
      <div class="h-[68dvh] overflow-auto">
        <CompetitionLeagueTable v-if="mode === 'table'" :leagueId="league?.id" :full="false" />
        <CompetitionNextFiveFixtures v-else-if="mode === 'fixtures'" />
      </div>
    </section>
    <section v-else class="md:grid gap-5 md:grid-cols-12">
      <div class="md:col-span-5 space-y-3">
        <CompetitionLeagueTable :leagueId="league?.id" />
      </div>
      <div class="md:col-span-3 flex flex-col gap-4">
        <CompetitionNextFiveFixtures />
      </div>
      <div class="col-span-4">
        <CompetitionRaceCard :leagueId="league?.id" />
      </div>
    </section>
  </main>
</template>