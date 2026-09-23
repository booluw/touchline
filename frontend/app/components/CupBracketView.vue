<script setup lang="ts">
// IM04: renders a cup campaign's materialized bracket rounds — decided ties
// plus byes — and the champion once decided.
import type { CupCampaign } from '~/composables/useCompetition'

const props = defineProps<{ campaign: CupCampaign }>()

function clubName(c: { id: string; name?: string }): string {
  return c.name ?? c.id.slice(0, 8)
}

function tieScore(tie: { home_score: number | null; away_score: number | null; winner?: { id: string } }): string {
  if (tie.home_score == null || tie.away_score == null || !tie.winner) return '—'
  return `${tie.home_score}–${tie.away_score}`
}
</script>

<template>
  <div class="mt-4 space-y-4">
    <div class="flex flex-wrap items-center gap-3 text-sm">
      <span class="text-slate-400">
        {{ props.campaign.total_rounds }} round(s) · top-N join in round
        {{ props.campaign.late_entry_round || 1 }}
      </span>
      <span v-if="props.campaign.season" class="text-slate-400">
        Season <span class="text-white">{{ props.campaign.season.label }}</span>
      </span>
      <span v-if="props.campaign.champion" class="text-emerald-400 font-semibold">
        Champion: {{ clubName(props.campaign.champion) }}
      </span>
    </div>

    <div v-for="r in props.campaign.rounds" :key="r.round" class="border border-slate-700 rounded-lg overflow-hidden">
      <h4 class="bg-slate-700/40 px-3 py-1.5 text-sm font-medium text-slate-300">
        Round {{ r.round }}{{ r.scheduled_at ? ` · ${new Date(r.scheduled_at).toLocaleDateString([], { day: 'numeric', month: 'short' })}` : '' }}
      </h4>
      <ul class="divide-y divide-slate-700/60">
        <li v-for="tie in r.ties" :key="tie.id" class="px-3 py-2 flex items-center gap-3 text-sm">
          <span class="flex-1 text-right">{{ clubName(tie.home_club) }}</span>
          <span class="bg-slate-900 border border-slate-600 rounded px-3 py-0.5 font-bold text-center w-16">
            {{ tieScore(tie) }}
          </span>
          <span class="flex-1">{{ clubName(tie.away_club) }}</span>
          <span class="w-24 text-right" :class="tie.status === 'completed' ? 'text-emerald-400' : 'text-slate-500'">
            {{ tie.status }}
          </span>
        </li>
        <li v-if="r.byes.length" class="px-3 py-1.5 text-sm text-slate-400">
          Byes: <span v-for="b in r.byes" :key="b.id" class="mr-2">{{ clubName(b) }}</span>
        </li>
        <li v-if="!r.ties.length && !r.byes.length" class="px-3 py-2 text-sm text-slate-500">
          Round not materialized yet.
        </li>
      </ul>
    </div>

    <p v-if="!props.campaign.rounds.length" class="text-slate-500 text-sm">
      No rounds materialized yet — start the campaign to draw the first-round bracket.
    </p>
  </div>
</template>