<script setup lang="ts">
import type { PositionFit } from '~/types/ui/design'

/** 32px club-colour circle; number = effective rating in the slot; ring = position fit. */
const props = defineProps<{ rating: number, fit: PositionFit, selected?: boolean, label?: string }>()
const ring: Record<PositionFit, string> = {
  natural: 'border-pos', capable: 'border-t2', awkward: 'border-important', out: 'border-neg',
}
</script>

<template>
  <button
    type="button"
    :aria-pressed="!!props.selected"
    :aria-label="label ? `${label}, rating ${rating}, ${fit}` : `Rating ${rating}, ${fit}`"
    :class="['num grid size-8 place-items-center rounded-full border-2 bg-club text-label font-semibold text-white',
             ring[props.fit], props.selected && 'outline-2 outline-offset-[3px] outline-t1']"
  >
    {{ rating }}
  </button>
</template>
