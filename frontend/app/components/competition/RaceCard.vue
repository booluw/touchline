<script lang="ts" setup>
import { useManagerCompetition } from '~/composables/manager/competitions';
import type { LoadingStatus } from '~/types';
import type { ManagerClubOutlook, OutlookRace } from '~/types/manager';
import { ordinal, toWhy } from '~/utils/helpers';

const props = defineProps<{ leagueId: string | undefined }>()

const { getCompetitionOutlook } = useManagerCompetition()

const status = ref<LoadingStatus>("loading")
const data = ref<ManagerClubOutlook>()

async function load() {
  if (!props.leagueId) return
  status.value = "loading"
  // getCompetitionOutlook toasts and returns undefined on failure.
  data.value = await getCompetitionOutlook(props.leagueId)
  status.value = data.value ? "loaded" : "error"
}

watch(() => props.leagueId, load, { immediate: true })

const headline = computed(() => data.value?.stakes ?? data.value?.races[0])
const rows = computed(() => [...(data.value?.races ?? []), ...(data.value?.attachments ?? [])])

const bandName = (r: OutlookRace) => r.cup?.name ?? r.kind
const band = (r: OutlookRace) => r.from_position === r.to_position ? ordinal(r.from_position) : `${ordinal(r.from_position)}–${ordinal(r.to_position)}`

// "Clinched"/"Relegated" only when mathematically certain; otherwise state the points gap.
function raceLine(r: OutlookRace) {
  const pts = `${r.gap} pt${r.gap === 1 ? '' : 's'}`
  if (r.status === 'clinched') return r.kind === 'relegation' ? 'Relegated' : `Clinched ${bandName(r)}`
  if (r.status === 'eliminated') return r.kind === 'relegation' ? 'Safe from relegation' : `Out of the ${bandName(r)} race`
  if (r.kind === 'relegation') return r.inside ? `${pts} from safety` : `${pts} above the drop`
  return r.inside ? `In the ${bandName(r)} places · ${pts} cushion` : `${pts} off ${bandName(r)}`
}

function tone(r: OutlookRace): 'pos' | 'neg' | 'muted' {
  if (r.status === 'eliminated') return r.kind === 'relegation' ? 'pos' : 'muted'
  const good = r.kind !== 'relegation'
  return r.inside === good ? 'pos' : 'neg'
}
</script>

<template>
  <UiCard v-if="status === 'loading'" class="flex flex-col gap-4 border-info">
    <UiLoader class="h-5 w-20" />
    <UiLoader class="h-5 w-50" />
    <div class="mt-5 flex gap-3">
      <UiLoader class="h-5 w-20" />
      <UiLoader class="h-5 w-40" />
      <UiLoader class="h-5 w-20" />
    </div>
    <UiLoader class="h-5 w-50" />
  </UiCard>
  <UiEmptyState v-else-if="status === 'error'" title="Outlook unavailable" />
  <div v-else-if="data" class="space-y-4">
    <UiCard class="space-y-4 border-info">
      <div class="text-label label-caps text-info">race</div>

      <div v-if="headline">
        <p class="text-title capitalize">{{ raceLine(headline) }}</p>
        <p class="text-t2">{{ ordinal(data.position) }} · {{ data.points }} pts · {{ data.games_left }} left</p>
      </div>

      <ul class="space-y-2">
        <li v-for="(r, i) in rows" :key="i" class="flex items-center justify-between gap-3">
          <div>
            <p class="capitalize">{{ bandName(r) }} <span class="text-t3">{{ band(r) }}</span></p>
            <p class="text-t2">{{ raceLine(r) }}</p>
          </div>
          <!-- <UiStatusBadge :tone="tone(r)">{{ r.status }}</UiStatusBadge> -->
        </li>
      </ul>

      <p v-if="data.guaranteed_at_least" class="text-t2">
        Guaranteed at least: <span class="text-t1">{{ data.guaranteed_at_least.name }}</span>
      </p>

      <div v-if="data.next_match" class="flex gap-4 num">
        <span>W → {{ ordinal(data.next_match.if_win) }}</span>
        <span>D → {{ ordinal(data.next_match.if_draw) }}</span>
        <span>L → {{ ordinal(data.next_match.if_loss) }}</span>
      </div>

      <p class="text-t3">Can finish {{ ordinal(data.finish.best) }}-{{ ordinal(data.finish.worst) }}</p>
    </UiCard>
    <UiWhyBreakdown v-if="data.projection.runs" subject="Projected league finish" :value="data.projection.position" :factors="toWhy(data.projection)" />
  </div>
</template>
