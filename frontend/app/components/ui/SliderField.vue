<script setup lang="ts">
import { Slider } from '@ark-ui/vue/slider'

const props = withDefaults(defineProps<{
  label: string
  min?: number
  max?: number
  step?: number
  /** Formats the value shown on the right, e.g. v => `£${v}k`. */
  format?: (value: number) => string
  disabled?: boolean
}>(), { min: 0, max: 100, step: 1, format: (v: number) => String(v) })
const model = defineModel<number>({ required: true })
</script>

<template>
  <Slider.Root
    :model-value="[model]"
    :min="props.min"
    :max="props.max"
    :step="props.step"
    :disabled="props.disabled"
    class="font-geist grid grid-cols-[100px_1fr_60px] items-center gap-2.5 text-body data-[disabled]:opacity-40"
    @update:model-value="(v: number[]) => (model = v[0] ?? props.min)"
  >
    <Slider.Label class="text-t2">{{ label }}</Slider.Label>
    <Slider.Control class="relative flex h-5 items-center">
      <Slider.Track class="h-1 flex-1 rounded-full bg-s3">
        <Slider.Range class="h-1 rounded-full bg-t1" />
      </Slider.Track>
      <Slider.Thumb :index="0" class="size-3.5 rounded-full border-2 border-bg bg-t1 outline-none data-[focus]:ring-2 data-[focus]:ring-t2">
        <Slider.HiddenInput />
      </Slider.Thumb>
    </Slider.Control>
    <span class="num text-right font-semibold text-t1">{{ props.format(model) }}</span>
  </Slider.Root>
</template>
