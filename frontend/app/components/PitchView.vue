<!--
  PitchView.vue — renders one of the two static pitch SVGs (pitch-full.svg /
  pitch-half.svg) as a background, then overlays player tokens as ordinary
  HTML positioned by percentage — NOT as SVG elements.

  Why percentage-positioned HTML instead of drawing tokens inside the SVG:
  - Both pitch SVGs use `preserveAspectRatio` defaults, so a percentage-based
    x/y always lands in the same spot on the pitch regardless of the
    rendered container size — no coordinate math tied to viewBox units.
  - Tokens can be ordinary interactive HTML (buttons, drag handles, avatar
    images, tooltips) instead of fighting with SVG's more limited event/
    layout model.
  - Updating a formation is just replacing the `slots` array — Vue's
    reactivity re-renders token positions, the pitch itself never re-renders.
    This is what makes it "dynamic": the SVG is inert background art, all
    the live state (lineup, live subs, drag-and-drop) lives in this layer.
-->
<template>
  <div class="pitch-view" :class="variant">
    <img src="assets/svgs/pitch-half.svg" class="pitch-bg" alt="" aria-hidden="true" />

    <button v-for="slot in positionedSlots" :key="slot.slot" class="token"
      :class="{ empty: !slot.player, selected: slot.slot === props.selectedSlot, 'cursor-pointer': clickable }"
      :style="{ left: slot.x + '%', top: slot.y + '%' }" @click="clickable ? $emit('select', slot.slot) : undefined">
      <span class="token-number">{{ slot.player?.squad_number ?? '' }}</span>
      <span class="token-name">{{ slot.player?.display_name ?? slot.player.name ?? 'Empty' }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type SquadPlayer = { squad_number?: number; display_name?: string; name?: string }
type LineupSlot = { slot: number; player?: SquadPlayer | null }

const props = defineProps<{
  variant: 'full' | 'half'
  formation: string // e.g. "4-3-3" — key into FORMATION_COORDS
  slots: LineupSlot[]
  selectedSlot?: number
  clickable?: boolean
}>()

defineEmits<{ select: [slot: number] }>()

const pitchSrc = computed(() =>
  props.variant === 'half' ? '~/assets/svgs/pitch-half.svg' : '~/assets/svgs/pitch-full.svg'
)

const positionedSlots = computed(() =>
  props.slots.map((slot, i) => {
    const coords = FORMATION_COORDS[props.formation]?.[i] ?? { x: 50, y: 50 }
    return { ...slot, x: coords.x, y: coords.y }
  })
)

</script>

<style scoped>
.pitch-view {
  position: relative;
  width: 100%;
  aspect-ratio: 680 / 1098;
}

.pitch-view.half {
  aspect-ratio: 680 / 573;
}

.pitch-bg {
  width: 100%;
  height: 100%;
  display: block;
  transform: rotate(180deg);
}

.token {
  position: absolute;
  transform: translate(-50%, -50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 0;
  background: var(--color-void-850, #0e1218);
  border: 2px solid var(--color-void-600, #2c3542);
  color: var(--color-void-100, #e4e7eb);
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  min-width: 3.5rem;
}

.token.selected {
  border-color: var(--color-cyan-500, #00e8ff);
  color: var(--color-void-50, #f5f6f8);
}

.token.empty {
  border-style: dashed;
  color: var(--color-void-500, #4a5566);
}

.token-number {
  font-weight: 700;
}
</style>