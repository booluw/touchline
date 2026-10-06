<script setup lang="ts">
import { Checkbox } from '@ark-ui/vue/checkbox'

/** Full-width toggle row, e.g. an optional clause in an offer: "After 25 appearances · £25k". */
defineProps<{ label: string, meta?: string, disabled?: boolean }>()
const checked = defineModel<boolean>('checked', { default: false })
</script>

<template>
  <Checkbox.Root
    :checked="checked"
    :disabled="disabled"
    class="font-geist flex cursor-pointer items-center gap-2.5 rounded-[7px] border border-line2 bg-s2 px-2.5 py-2 text-body data-[disabled]:opacity-40"
    @checked-change="(d: { checked: boolean | 'indeterminate' }) => (checked = d.checked === true)"
  >
    <Checkbox.Control class="grid size-3.5 place-items-center rounded-pill border border-line2 data-[state=checked]:border-t1 data-[state=checked]:bg-t1 data-[focus-visible]:outline-2 data-[focus-visible]:outline-offset-2 data-[focus-visible]:outline-t2">
      <Checkbox.Indicator class="size-1.5 rounded-[1px] bg-bg" />
    </Checkbox.Control>
    <Checkbox.Label class="flex-1 text-t1">{{ label }}</Checkbox.Label>
    <span v-if="meta" class="num text-t2">{{ meta }}</span>
    <Checkbox.HiddenInput />
  </Checkbox.Root>
</template>
