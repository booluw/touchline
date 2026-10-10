<script lang="ts" setup>
import { useManagerCompetition } from '~/composables/manager/competitions';
import type { CompetitionStanding, LoadingStatus } from '~/types'
import type { TableColumn } from '~/types/ui/design';

const props = withDefaults(defineProps<{
  leagueId: string | undefined
  full?: boolean
}>(), { full: true })

const { getLeagueTable } = useManagerCompetition()
const clubstore = useClubStore()

const status = ref<LoadingStatus>("loading")
const data = ref<CompetitionStanding>()

const clubId = computed(() => clubstore.club?.id)

async function load() {
  if (!props.leagueId) return
  status.value = "loading"
  try {
    data.value = await getLeagueTable(props.leagueId)
    status.value = "loaded"
  } catch (error) {
    console.error(error)
    status.value = "error"
  }
}

watch(() => props.leagueId, load)

type TableHeader = "position" | "club" | "played" | "won" | "drawn" | "lost" | "goal" | "points" | "form"
const columns: TableColumn<TableHeader>[] = [
  { key: "position", label: "#", width: "30px" },
  { key: "club", label: "Club", width: "2fr" },
  { key: "played", label: "P", width: "30px", advanced: true },
  { key: "won", label: "W", width: "30px", advanced: true },
  { key: "drawn", label: "D", width: "30px", advanced: true },
  { key: "lost", label: "L", width: "30px", advanced: true },
  { key: "goal", label: "GD", width: "30px" },
  { key: "points", label: "Pts", width: "30px" },
  { key: "form", label: "Form", width: "76px" }
]

const skeletonRows = Array.from({ length: 20 }, (_, i) => ({ id: i }))
const numericCols = ['played', 'won', 'drawn', 'lost', 'goal', 'points'] as const

const formatter = new Intl.NumberFormat('en-US', { signDisplay: 'always' });

onMounted(() => load())
</script>
<template>
  <!-- Fills a height-bounded parent: the header stays fixed, the rows scroll. -->
  <section class="flex h-full min-h-0 flex-col gap-5">
    <template v-if="status === 'loading'">
      <!-- Same table, header and widths as the loaded state; cells are placeholders. -->
      <UiCard flush class="flex max-h-full min-h-0 flex-col overflow-hidden" aria-busy="true">
        <UiDataTable class="min-h-0" :columns :rows="skeletonRows" :row-key="r => r.id" :density="full ? undefined : 'simple'" caption="Loading table">
          <template #cell-position><UiLoader class="h-3 w-4" /></template>
          <template #cell-club><UiLoader class="h-3.5 w-28" /></template>
          <template v-for="k in numericCols" :key="k" #[`cell-${k}`]><UiLoader class="h-3 w-4" /></template>
          <template #cell-form><span class="flex gap-1"><UiLoader v-for="i in 5" :key="i" class="h-3 w-3 rounded-pill" /></span></template>
        </UiDataTable>
      </UiCard>
    </template>
    <template v-else-if="status === 'loaded' && data">
      <UiCard flush class="flex max-h-full min-h-0 flex-col overflow-hidden">
        <UiDataTable class="min-h-0" :columns :rows="data.rows" :row-key="r => r.club.id" :density="full ? undefined : 'simple'"
          :selected-key="clubId">
          <template #cell-position="{ row }">
            <div :class="`absolute border h-8 -translate-x-3 -translate-y-2 hidden`" />
            <span class="num w-8 text-label text-t3">{{ row.position }}</span>
          </template>
          <template #cell-club="{ row }">
            <nuxt-link :to="`/play/clubs/${row.club.id}`" class="block truncate hover:underline" :title="row.club.name">{{ row.club.name }}</nuxt-link>
          </template>
          <template #cell-lost="{ row }">
            <span class="num w-8 text-t3">{{ row.lost }}</span>
          </template>
          <template #cell-drawn="{ row }">
            <span class="num w-8 text-t3">{{ row.drawn }}</span>
          </template>
          <template #cell-played="{ row }">
            <span class="num w-8 text-t3">{{ row.played }}</span>
          </template>
          <template #cell-won="{ row }">
            <span class="num w-8 text-t3">{{ row.won }}</span>
          </template>
          <template #cell-goal="{ row }">
            <span class="num">{{ formatter.format(row.goals_for - row.goals_against) }}</span>
          </template>
          <template #cell-points="{ row }">
            <span class="num w-8">{{ row.points }}</span>
          </template>
          <template #cell-form="{ row }">
            <div class="flex gap-1">
              <div
                v-for="(form, key) in row.form" :key
                class="h-3 w-3 shrink-0 rounded-pill"
                :class="[{ 'bg-pos' : form === 'W' }, { 'bg-neg': form === 'L' }, { 'bg-neutral': form === 'D'}]"
              />
            </div>
          </template>
        </UiDataTable>
      </UiCard>
    </template>
    <template v-else></template>
  </section>
</template>