<script setup lang="ts">
import type { WhyFactor } from '~/types/ui/design'

/**
 * The core explainability component. Signed factors, centre-zero bars.
 * Rules (design spec): net = exact sum of all factors · sorted by |value| ·
 * max `maxRows` rows, remainder folded into "Other" · bars relative to the largest row.
 */
const props = withDefaults(defineProps<{
  /** e.g. "Kintu’s happiness" */
  subject: string
  /** Current value of the subject, e.g. 22. */
  value?: string | number
  factors: WhyFactor[]
  maxRows?: number
  /** Standalone card (--s1) instead of the nested inline panel (--s2). */
  standalone?: boolean
}>(), { maxRows: 6 })

const rows = computed<WhyFactor[]>(() => {
  const sorted = [...props.factors].sort((a, b) => Math.abs(b.value) - Math.abs(a.value))
  if (sorted.length <= props.maxRows) return sorted
  const kept = sorted.slice(0, props.maxRows - 1)
  const other = sorted.slice(props.maxRows - 1).reduce((sum, f) => sum + f.value, 0)
  return [...kept, { label: 'Other', value: other }]
})
const net = computed(() => props.factors.reduce((sum, f) => sum + f.value, 0))
const scale = computed(() => Math.max(1, ...rows.value.map(r => Math.abs(r.value))))
const width = (v: number) => `${(Math.abs(v) / scale.value) * 100}%`
const toneOf = (v: number) => (v > 0 ? 'text-pos' : v < 0 ? 'text-neg' : 'text-t3')
</script>

<template>
  <section
    :class="['font-geist flex flex-col gap-2 border border-line p-3 text-meta', standalone ? 'rounded-card bg-s1' : 'rounded-nested bg-s2']"
    :aria-label="`Why? ${subject}`"
  >
    <!-- The summary wraps on narrow screens; net never shrinks out of the card. -->
    <header class="flex items-start justify-between gap-2">
      <span class="min-w-0 break-words font-semibold text-t1">
        Why? <span v-if="value !== undefined" class="font-normal text-t3">{{ subject }}: {{ value }}</span>
        <span v-else class="font-normal text-t3">{{ subject }}</span>
      </span>
      <span :class="['num shrink-0 whitespace-nowrap', toneOf(net)]">net {{ formatSigned(net) }}</span>
    </header>
    <ul class="flex flex-col gap-2">
      <li v-for="row in rows" :key="row.label" class="grid grid-cols-[minmax(0,1fr)_120px_36px] items-center gap-2.5">
        <span class="truncate text-t2">{{ row.label }}</span>
        <div class="flex h-1.5 items-center" aria-hidden="true">
          <div class="flex h-1.5 flex-1 justify-end rounded-l-[3px] bg-s3">
            <div class="h-1.5 rounded-l-[3px] bg-neg" :style="{ width: row.value < 0 ? width(row.value) : '0%' }" />
          </div>
          <div class="h-2.5 w-px bg-t3" />
          <div class="h-1.5 flex-1 rounded-r-[3px] bg-s3">
            <div class="h-1.5 rounded-r-[3px] bg-pos" :style="{ width: row.value > 0 ? width(row.value) : '0%' }" />
          </div>
        </div>
        <span :class="['num text-right', toneOf(row.value)]">{{ formatSigned(row.value) }}</span>
      </li>
    </ul>
  </section>
</template>
