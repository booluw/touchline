<script setup lang="ts">
import { FINANCIAL_HEALTH_STAGES, type FinancialHealthStage } from '~/types/ui/design'

/** Escalation ladder: fill up to the current stage. */
const props = defineProps<{ stage: FinancialHealthStage }>()
const index = computed(() => FINANCIAL_HEALTH_STAGES.indexOf(props.stage))
const TONES = ['pos', 'important', 'restriction', 'urgent', 'urgent'] as const
const tone = computed(() => TONES[index.value] ?? 'urgent')
const fill = { pos: 'bg-pos', important: 'bg-important', restriction: 'bg-restriction', urgent: 'bg-urgent' } as const
const pill = { pos: 'pos', important: 'important', restriction: 'important', urgent: 'urgent' } as const
</script>

<template>
  <div class="font-geist flex flex-col gap-2">
    <UiStatusBadge :tone="pill[tone]" class="w-fit">{{ stage }}</UiStatusBadge>
    <div class="grid grid-cols-5 gap-[3px]" role="meter" aria-label="Financial health" aria-valuemin="1" aria-valuemax="5" :aria-valuenow="index + 1" :aria-valuetext="stage">
      <span v-for="(s, i) in FINANCIAL_HEALTH_STAGES" :key="s" :class="['h-1.5 rounded-[2px]', i <= index ? fill[tone] : 'bg-s3']" />
    </div>
    <span class="text-label text-t2">{{ FINANCIAL_HEALTH_STAGES.join(' → ') }}</span>
  </div>
</template>
