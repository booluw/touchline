<script lang="ts" setup>
import { useAdminLeague } from '~/composables/admin/league';
import type { Competition } from '~/types';

definePageMeta({
  name: "admin-league-page",
  path: "/admin/world/:id/countries/:countryId/leagues/:leagueId"
})

const route = useRoute()
const leagueId = computed<string>(() => route.params.leagueId)
const { getLeagueDetails } = useAdminLeague({ leagueId: leagueId.value })

const status = ref("loading")
const modal = ref(false)
const competition = ref<Competition>()

async function loadLeague() {
  status.value = "loading"
  try {
    competition.value = await getLeagueDetails()
    status.value = "loaded"
  } catch (error) {
    status.value = "error"
  }
}

onMounted(() => loadLeague())
</script>
<template>
  <!-- {{ competition }} -->
  <UiLoader v-if="status === 'loading'" />
  <section v-else-if="status === 'loaded' && competition" class="space-y-10">
    <div class="flex items-center justify-between">
      <div class="">
        <h1 class="page__header uppercase">{{ competition.name }}</h1>
      </div>
      <div class="">
        <button class="button button--outline" @click="modal = true">Configure League</button>
      </div>
    </div>

    <section class="grid grid-cols-4 row-span-2">
      <div class="col-span-2 row-span-2 border-brutal border-void-600 p-5 space-y-5">
        <div class="pb-3 border-b-brutal border-void-700">
          <h3 class="heading heading--small">League table</h3>
        </div>

        <div class="">
          {{ competition }}
        </div>
      </div>
    </section>

    <AdminLeagueConfig
      v-if="modal"
      :world_id="competition.world_id"
      :id="competition.id"
      :name="competition.name"
      :started="Boolean(competition.league?.standings.length)"
      @close="modal = false"
      @done="loadLeague()"
    />
  </section>
  <section v-else class=""></section>
</template>