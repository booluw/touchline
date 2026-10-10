<script setup lang="ts" generic="V extends string">
import { SegmentGroup } from '@ark-ui/vue/segment-group'
import type { SegmentOption } from '~/types/ui/design'

defineProps<{
  options: SegmentOption<V>[]
  /** Accessible name for the group. */
  label: string
  /** Use mono digits, for numeric options like 10% / 15%. */
  numeric?: boolean
  disabled?: boolean
  /** Full width, one equal column per option (tabs, dials). */
  fill?: boolean
}>()
const model = defineModel<V>({ required: true })

function onChange(value: string | null) {
  if (value !== null) model.value = value as V
}
</script>

<template>
  <SegmentGroup.Root
    :model-value="model"
    :disabled="disabled"
    :aria-label="label"
    :class="['relative rounded-[7px] border border-line bg-s2 p-0.5', fill ? 'grid w-full' : 'inline-flex']"
    :style="fill ? { gridTemplateColumns: `repeat(${options.length}, minmax(0, 1fr))` } : undefined"
    @update:model-value="onChange"
  >
    <SegmentGroup.Indicator class="rounded-[5px] bg-s3 transition-all duration-150 ease-out [height:var(--height)] [left:var(--left)] [top:var(--top)] [width:var(--width)]" />
    <SegmentGroup.Item
      v-for="option in options"
      :key="option.value"
      :value="option.value"
      :disabled="option.disabled"
      class="relative z-10 flex cursor-pointer items-center justify-center rounded-[5px] px-2.5 py-[5px] text-meta font-medium text-t3 transition-colors data-[state=checked]:text-t1 data-[disabled]:opacity-40 data-[focus-visible]:outline-2 data-[focus-visible]:outline-t2"
    >
      <SegmentGroup.ItemText :class="numeric ? 'num' : 'font-geist'">{{ option.label }}</SegmentGroup.ItemText>
      <SegmentGroup.ItemControl />
      <SegmentGroup.ItemHiddenInput />
    </SegmentGroup.Item>
  </SegmentGroup.Root>
</template>
