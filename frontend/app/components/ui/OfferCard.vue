<script setup lang="ts">
import type { OfferTerm, SemanticTone } from '~/types/ui/design'

/** Negotiation thread message. Mine: right-aligned on --s2; theirs: left on --s1. Changed terms show old struck through. */
const props = withDefaults(defineProps<{
  side: 'mine' | 'theirs'
  /** "@mira_dunn · Harlow Athletic · 13 Dec" */
  meta?: string
  status: string
  statusTone?: SemanticTone
  /** Right side of the header, e.g. "£305k max". */
  headline?: string
  terms: OfferTerm[]
  message?: string
  /** Highlight border, e.g. a final counter. */
  emphasis?: boolean
}>(), { statusTone: 'neutral' })
const statusText: Record<SemanticTone, string> = {
  urgent: 'text-urgent', important: 'text-important', info: 'text-info', pos: 'text-pos', neg: 'text-neg', neutral: 'text-t3',
}
</script>

<template>
  <article :class="['font-geist flex max-w-[420px] flex-col gap-1.5 text-t1', props.side === 'mine' && 'ml-auto items-end']">
    <span v-if="meta" class="text-[11.5px] text-t3">{{ meta }}</span>
    <div :class="['w-full overflow-hidden rounded-card border', props.side === 'mine' ? 'bg-s2' : 'bg-s1', emphasis ? 'border-important' : 'border-line']">
      <header class="flex justify-between border-b border-line px-3 py-2.5">
        <span :class="['num text-label uppercase', statusText[props.statusTone]]">{{ status }}</span>
        <span v-if="headline" class="num font-semibold">{{ headline }}</span>
      </header>
      <dl class="px-3 py-1.5">
        <div v-for="term in terms" :key="term.label" class="flex justify-between py-1 text-[12.5px]">
          <dt class="text-t2">{{ term.label }}</dt>
          <dd v-if="term.previous !== undefined && term.previous !== term.value">
            <s class="mr-2 text-[11.5px] text-t3">{{ term.previous }}</s><b class="font-semibold">{{ term.value }}</b>
          </dd>
          <dd v-else class="text-t2">{{ term.value }}</dd>
        </div>
      </dl>
      <p v-if="message" class="border-t border-line px-3 py-2.5 text-body">“{{ message }}”</p>
    </div>
  </article>
</template>
