<script setup lang="ts">
const clubstore = useClubStore()
const league = computed(() => clubstore.competitions.find((c) => c.competition_type === "league")?.league?.competition ?? undefined)
</script>

<template>
  <main class="space-y-5">
    <div class="flex flex-col gap-1">
      <h2 class="text-page">Competitions</h2>
      <p class="text-t2 capitalize">
        {{ league?.name }} · {{ league?.team_count }} Clubs · {{ league?.promotions ? `top ${league.promotions} promoted, ` : '' }} {{ league?.relegations ? `bottom ${league.relegations} relegated` : '' }}
      </p>
    </div>
    <section class="grid gap-10 md:grid-cols-12">
      <div class="col-span-5">
        <CompetitionLeagueTable :leagueId="league?.id" />
      </div>
      <div class="col-span-3">Smallie</div>
      <div class="col-span-4">Hiii</div>
    </section>
  </main>
</template>